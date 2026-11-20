package agentui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

func checkOrigin(r *http.Request) bool {
	if r == nil {
		return false
	}
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if origin == "" {
		return true
	}
	u, err := url.Parse(origin)
	if err != nil {
		return false
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" && scheme != "ws" && scheme != "wss" {
		return false
	}
	if u.User != nil {
		return false
	}
	host := strings.ToLower(u.Hostname())
	switch host {
	case "localhost", "127.0.0.1", "::1":
		return true
	default:
		return false
	}
}

var upgrader = websocket.Upgrader{
	CheckOrigin: checkOrigin,
}

type wsClient struct {
	conn      *websocket.Conn
	send      chan []byte
	done      chan struct{}
	closeOnce sync.Once
}

func newWSClient(conn *websocket.Conn) *wsClient {
	return &wsClient{
		conn: conn,
		send: make(chan []byte, 256),
		done: make(chan struct{}),
	}
}

func (c *wsClient) close() {
	c.closeOnce.Do(func() {
		close(c.done)
		_ = c.conn.Close()
	})
}

func (c *wsClient) enqueue(msg []byte) bool {
	select {
	case <-c.done:
		return false
	default:
	}
	select {
	case c.send <- msg:
		return true
	case <-c.done:
		return false
	default:
		return false
	}
}

type TelemetryServer struct {
	mu         sync.RWMutex
	port       int
	listener   net.Listener
	httpServer *http.Server
	started    bool
	stopped    bool
	startErr   error

	clients   map[*wsClient]bool
	clientsMu sync.Mutex

	broadcast chan []byte
	stopChan  chan struct{}
	stopOnce  sync.Once
	stopErr   error

	readyChan chan struct{}
	readyOnce sync.Once

	wg sync.WaitGroup
}

func NewTelemetryServer(port int) *TelemetryServer {
	if port < 0 {
		port = 8085
	}
	return &TelemetryServer{
		port:      port,
		clients:   make(map[*wsClient]bool),
		broadcast: make(chan []byte, 256),
		stopChan:  make(chan struct{}),
		readyChan: make(chan struct{}),
	}
}

func (s *TelemetryServer) signalReady() {
	s.readyOnce.Do(func() {
		close(s.readyChan)
	})
}

// Ready returns a channel closed once the server listener has bound (or failed/stopped).
func (s *TelemetryServer) Ready() <-chan struct{} {
	return s.readyChan
}

// StartErr returns any early startup/listen error recorded before Ready() closed.
func (s *TelemetryServer) StartErr() error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.startErr
}

// Port returns the bound loopback TCP port (or configured port before Start).
func (s *TelemetryServer) Port() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.port
}

func (s *TelemetryServer) Start() error {
	s.mu.Lock()
	if s.stopped {
		s.startErr = http.ErrServerClosed
		s.mu.Unlock()
		s.signalReady()
		return http.ErrServerClosed
	}
	if s.started {
		s.mu.Unlock()
		return fmt.Errorf("telemetry server already started")
	}
	s.started = true
	listenPort := s.port
	s.mu.Unlock()

	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", listenPort))
	if err != nil {
		s.mu.Lock()
		s.startErr = err
		s.mu.Unlock()
		s.signalReady()
		return err
	}

	boundPort := listenPort
	if tcpAddr, ok := ln.Addr().(*net.TCPAddr); ok {
		boundPort = tcpAddr.Port
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/ws", s.handleWS)
	mux.HandleFunc("/api/telemetry", s.handleTelemetryAPI)
	mux.HandleFunc("/api/rag/search", s.handleRAGSearchAPI)
	mux.HandleFunc("/", s.handleDashboardHTML)

	srv := &http.Server{
		Addr:              ln.Addr().String(),
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}

	s.mu.Lock()
	if s.stopped {
		s.mu.Unlock()
		_ = ln.Close()
		s.signalReady()
		return http.ErrServerClosed
	}
	s.port = boundPort
	s.listener = ln
	s.httpServer = srv
	s.wg.Add(2)
	go s.listenBroadcast()
	go s.autoBroadcastLoop()
	s.mu.Unlock()

	s.signalReady()

	err = srv.Serve(ln)
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		_ = s.Stop()
	}
	return err
}

func (s *TelemetryServer) Stop() error {
	s.stopOnce.Do(func() {
		s.mu.Lock()
		s.stopped = true
		srv := s.httpServer
		ln := s.listener
		s.mu.Unlock()

		close(s.stopChan)
		s.signalReady()

		if srv != nil {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			if err := srv.Shutdown(ctx); err != nil && !errors.Is(err, http.ErrServerClosed) {
				s.stopErr = srv.Close()
			}
		} else if ln != nil {
			s.stopErr = ln.Close()
		}

		s.clientsMu.Lock()
		for c := range s.clients {
			c.close()
			delete(s.clients, c)
		}
		s.clientsMu.Unlock()

		s.wg.Wait()
	})
	return s.stopErr
}

// Broadcast queues a serialized telemetry message for all connected WebSocket clients.
func (s *TelemetryServer) Broadcast(msg []byte) {
	if len(msg) == 0 {
		return
	}
	select {
	case <-s.stopChan:
		return
	default:
	}
	select {
	case s.broadcast <- msg:
	case <-s.stopChan:
	default:
	}
}

func (s *TelemetryServer) removeClient(c *wsClient) {
	s.clientsMu.Lock()
	delete(s.clients, c)
	s.clientsMu.Unlock()
}

func (s *TelemetryServer) handleTelemetryAPI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	query := r.URL.Query().Get("q")
	data := GetDashboardTelemetry(query)
	_ = json.NewEncoder(w).Encode(data)
}

func (s *TelemetryServer) handleRAGSearchAPI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	query := r.URL.Query().Get("q")
	if query == "" {
		query = "agent workflow"
	}
	ragData := GetRAGTelemetry(query)
	_ = json.NewEncoder(w).Encode(ragData)
}

func (s *TelemetryServer) handleWS(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	c := newWSClient(conn)

	s.mu.RLock()
	if s.stopped {
		s.mu.RUnlock()
		c.close()
		return
	}
	s.clientsMu.Lock()
	s.clients[c] = true
	s.clientsMu.Unlock()
	s.wg.Add(2)
	go s.readPump(c)
	go s.writePump(c)
	s.mu.RUnlock()
}

func (s *TelemetryServer) readPump(c *wsClient) {
	defer s.wg.Done()
	defer func() {
		s.removeClient(c)
		c.close()
	}()
	for {
		if _, _, err := c.conn.ReadMessage(); err != nil {
			return
		}
	}
}

func (s *TelemetryServer) writePump(c *wsClient) {
	defer s.wg.Done()
	defer func() {
		s.removeClient(c)
		c.close()
	}()
	for {
		select {
		case msg := <-c.send:
			_ = c.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
			if err := c.conn.WriteMessage(websocket.TextMessage, msg); err != nil {
				return
			}
		case <-c.done:
			return
		case <-s.stopChan:
			return
		}
	}
}

func (s *TelemetryServer) listenBroadcast() {
	defer s.wg.Done()
	for {
		select {
		case msg, ok := <-s.broadcast:
			if !ok {
				return
			}
			s.clientsMu.Lock()
			targets := make([]*wsClient, 0, len(s.clients))
			for c := range s.clients {
				targets = append(targets, c)
			}
			s.clientsMu.Unlock()

			for _, c := range targets {
				if !c.enqueue(msg) {
					s.removeClient(c)
					c.close()
				}
			}
		case <-s.stopChan:
			return
		}
	}
}

func (s *TelemetryServer) autoBroadcastLoop() {
	defer s.wg.Done()
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			data := GetDashboardTelemetry("")
			b, err := json.Marshal(data)
			if err == nil {
				s.Broadcast(b)
			}
		case <-s.stopChan:
			return
		}
	}
}

func (s *TelemetryServer) handleDashboardHTML(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(dashboardHTMLTemplate))
}

const dashboardHTMLTemplate = `<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>Microgravity Telemetry Control Center</title>
    <style>
        :root {
            --bg: #0f172a;
            --card-bg: #1e293b;
            --border: #334155;
            --cyan: #38bdf8;
            --mint: #34d399;
            --purple: #c084fc;
            --gold: #fbbf24;
            --pink: #f472b6;
            --crimson: #f87171;
            --muted: #94a3b8;
            --text: #f8fafc;
        }
        * { box-sizing: border-box; margin: 0; padding: 0; font-family: 'Segoe UI', system-ui, -apple-system, sans-serif; }
        body { background: var(--bg); color: var(--text); padding: 24px; }
        header { display: flex; justify-content: space-between; align-items: center; padding-bottom: 20px; border-bottom: 1px solid var(--border); margin-bottom: 24px; }
        .logo { font-size: 1.5rem; font-weight: 800; color: var(--pink); display: flex; align-items: center; gap: 10px; }
        .badge-live { background: rgba(52, 211, 153, 0.15); color: var(--mint); padding: 6px 14px; border-radius: 999px; font-weight: 600; font-size: 0.85rem; border: 1px solid var(--mint); display: flex; align-items: center; gap: 8px; }
        .dot { width: 8px; height: 8px; background: var(--mint); border-radius: 50%; display: inline-block; animation: pulse 1.5s infinite; }
        @keyframes pulse { 0% { opacity: 0.4; } 50% { opacity: 1; } 100% { opacity: 0.4; } }
        
        .grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(500px, 1fr)); gap: 24px; }
        .card { background: var(--card-bg); border: 1px solid var(--border); border-radius: 12px; padding: 20px; box-shadow: 0 4px 20px rgba(0,0,0,0.3); }
        .card-header { display: flex; justify-content: space-between; align-items: center; margin-bottom: 16px; border-bottom: 1px solid rgba(255,255,255,0.05); padding-bottom: 10px; }
        .card-title { font-size: 1.1rem; font-weight: 700; display: flex; align-items: center; gap: 8px; }
        .title-swarm { color: var(--mint); }
        .title-sys { color: var(--pink); }
        .title-tokens { color: var(--gold); }
        .title-rag { color: var(--purple); }
        .title-sessions { color: var(--cyan); }

        .spark-row { display: grid; grid-template-columns: repeat(2, 1fr); gap: 14px; margin-top: 8px; }
        .spark-box { background: rgba(15, 23, 42, 0.7); border: 1px solid rgba(255,255,255,0.06); border-radius: 8px; padding: 12px; }
        .spark-head { display: flex; justify-content: space-between; align-items: baseline; margin-bottom: 8px; }
        .spark-title { font-size: 0.8rem; color: var(--muted); text-transform: uppercase; font-weight: 700; }
        .spark-val { font-size: 1.1rem; font-weight: 800; color: var(--cyan); }
        .spark-svg { width: 100%; height: 48px; display: block; }
        .spark-meta { font-size: 0.75rem; color: var(--muted); margin-top: 6px; font-family: monospace; }
        
        .meter-container { background: #0f172a; border-radius: 8px; height: 24px; width: 100%; overflow: hidden; margin: 12px 0; border: 1px solid var(--border); position: relative; }
        .meter-bar { height: 100%; background: linear-gradient(90deg, var(--mint), var(--gold)); width: 0%; transition: width 0.5s ease; }
        .meter-label { position: absolute; right: 10px; top: 3px; font-size: 0.8rem; font-weight: 700; color: #fff; text-shadow: 0 1px 2px #000; }
        
        .stats-grid { display: grid; grid-template-columns: repeat(3, 1fr); gap: 12px; margin-top: 14px; }
        .stat-item { background: rgba(15, 23, 42, 0.6); padding: 10px; border-radius: 8px; text-align: center; border: 1px solid rgba(255,255,255,0.05); }
        .stat-val { font-size: 1.2rem; font-weight: 700; color: var(--cyan); }
        .stat-lbl { font-size: 0.75rem; color: var(--muted); text-transform: uppercase; margin-top: 4px; }

        table { width: 100%; border-collapse: collapse; margin-top: 10px; font-size: 0.9rem; }
        th, td { text-align: left; padding: 10px; border-bottom: 1px solid rgba(255,255,255,0.05); }
        th { color: var(--muted); font-weight: 600; font-size: 0.8rem; text-transform: uppercase; }
        .status-pill { padding: 3px 8px; border-radius: 4px; font-weight: 700; font-size: 0.75rem; display: inline-block; }
        .status-running { background: rgba(52, 211, 153, 0.2); color: var(--mint); border: 1px solid var(--mint); }
        .status-completed { background: rgba(56, 189, 248, 0.2); color: var(--cyan); border: 1px solid var(--cyan); }
        .status-failed { background: rgba(248, 113, 113, 0.2); color: var(--crimson); border: 1px solid var(--crimson); }

        .search-box { display: flex; gap: 8px; margin-bottom: 14px; }
        .search-box input { flex: 1; background: #0f172a; border: 1px solid var(--border); padding: 8px 12px; border-radius: 6px; color: #fff; font-size: 0.9rem; }
        .search-box button { background: var(--purple); color: #fff; border: none; padding: 8px 16px; border-radius: 6px; font-weight: 700; cursor: pointer; transition: opacity 0.2s; }
        .search-box button:hover { opacity: 0.9; }

        .rag-item { background: rgba(15, 23, 42, 0.5); padding: 10px; border-radius: 6px; margin-bottom: 8px; border-left: 3px solid var(--purple); }
        .rag-id { font-weight: 700; color: var(--pink); font-size: 0.85rem; }
        .rag-score { font-size: 0.75rem; color: var(--gold); float: right; }
        .rag-text { font-size: 0.85rem; color: var(--muted); margin-top: 4px; font-family: monospace; }

        .transcript-item { background: #0f172a; padding: 10px; border-radius: 6px; margin-bottom: 8px; font-family: monospace; font-size: 0.85rem; border: 1px solid rgba(255,255,255,0.05); }
        .ts-time { color: var(--muted); }
        .ts-sender { color: var(--cyan); font-weight: 700; }
        .ts-msg { color: var(--text); margin-top: 4px; }
    </style>
</head>
<body>
    <header>
        <div class="logo">🌌 MICROGRAVITY TELEMETRY CONTROL CENTER</div>
        <div class="badge-live"><span class="dot"></span> LIVE STREAMING</div>
    </header>

    <div class="grid">
        <!-- System CPU & RAM Sparklines Panel -->
        <div class="card">
            <div class="card-header">
                <div class="card-title title-sys">💻 System CPU &amp; RAM Sparklines</div>
                <span id="sys-summary" style="font-size:0.85rem; color:var(--muted)">0 samples</span>
            </div>
            <div class="spark-row">
                <div class="spark-box">
                    <div class="spark-head">
                        <span class="spark-title">CPU Load</span>
                        <span class="spark-val" id="cpu-pct">0.0%</span>
                    </div>
                    <svg class="spark-svg" id="cpu-sparkline-svg" viewBox="0 0 240 48" preserveAspectRatio="none"></svg>
                    <div class="spark-meta" id="cpu-spark-txt">▁</div>
                </div>
                <div class="spark-box">
                    <div class="spark-head">
                        <span class="spark-title">RAM Usage</span>
                        <span class="spark-val" id="ram-pct" style="color:var(--mint)">0.0%</span>
                    </div>
                    <svg class="spark-svg" id="ram-sparkline-svg" viewBox="0 0 240 48" preserveAspectRatio="none"></svg>
                    <div class="spark-meta" id="ram-detail">0 B / 0 B</div>
                </div>
            </div>
        </div>

        <!-- Subagent Swarm Panel -->
        <div class="card">
            <div class="card-header">
                <div class="card-title title-swarm">🐝 Subagent Swarm Tracking</div>
                <span id="swarm-summary" style="font-size:0.85rem; color:var(--muted)">0 Active Threads</span>
            </div>
            <table>
                <thead>
                    <tr>
                        <th>ID</th>
                        <th>Role</th>
                        <th>Status</th>
                        <th>Model</th>
                        <th>Workspace</th>
                    </tr>
                </thead>
                <tbody id="swarm-tbody">
                    <tr><td colspan="5" style="color:var(--muted)">Loading subagents...</td></tr>
                </tbody>
            </table>
        </div>

        <!-- Token Usage Panel -->
        <div class="card">
            <div class="card-header">
                <div class="card-title title-tokens">⚡ Token Usage & Budget Statistics</div>
                <span id="token-pct" style="font-size:0.85rem; color:var(--gold); font-weight:700">0% Used</span>
            </div>
            <div class="meter-container">
                <div class="meter-bar" id="meter-bar"></div>
                <div class="meter-label" id="meter-label">0 / 128,000</div>
            </div>
            <div class="stats-grid">
                <div class="stat-item">
                    <div class="stat-val" id="stat-prompt">0</div>
                    <div class="stat-lbl">Prompt Tokens</div>
                </div>
                <div class="stat-item">
                    <div class="stat-val" id="stat-compl">0</div>
                    <div class="stat-lbl">Completion Tokens</div>
                </div>
                <div class="stat-item">
                    <div class="stat-val" id="stat-cost">$0.00</div>
                    <div class="stat-lbl">Est. Cost</div>
                </div>
            </div>
        </div>

        <!-- RAG Memory Search Inspection Panel -->
        <div class="card">
            <div class="card-header">
                <div class="card-title title-rag">🧠 RAG Memory Search Inspection</div>
                <span id="rag-docs" style="font-size:0.85rem; color:var(--muted)">0 Documents</span>
            </div>
            <div class="search-box">
                <input type="text" id="rag-input" placeholder="Type prompt to query vector store..." value="subagent swarm architecture">
                <button onclick="performRAGSearch()">Inspect RAG</button>
            </div>
            <div id="rag-results">
                <div style="color:var(--muted)">No search results loaded.</div>
            </div>
        </div>

        <!-- Active Session Transcript Logs Panel -->
        <div class="card">
            <div class="card-header">
                <div class="card-title title-sessions">📜 Active Session Transcript Logs</div>
                <span id="session-count" style="font-size:0.85rem; color:var(--muted)">0 Sessions</span>
            </div>
            <div id="transcript-list">
                <div style="color:var(--muted)">Loading transcript logs...</div>
            </div>
        </div>
    </div>

    <script>
        function escapeHTML(str) {
            return String(str == null ? '' : str)
                .replace(/&/g, '&amp;')
                .replace(/</g, '&lt;')
                .replace(/>/g, '&gt;')
                .replace(/"/g, '&quot;')
                .replace(/'/g, '&#39;');
        }

        function formatBytes(bytes) {
            if (!bytes || bytes <= 0) return '0 B';
            const units = ['B', 'KB', 'MB', 'GB', 'TB'];
            let idx = 0;
            let val = bytes;
            while (val >= 1024 && idx < units.length - 1) {
                val /= 1024;
                idx++;
            }
            return val.toFixed(1) + ' ' + units[idx];
        }

        function renderSparklineSVG(svgId, values, color) {
            const svg = document.getElementById(svgId);
            if (!svg) return;
            if (!values || values.length === 0) {
                svg.innerHTML = '';
                return;
            }
            const w = 240, h = 48, pad = 3;
            const n = values.length;
            const pts = values.map((v, i) => {
                const x = n === 1 ? w : (i * w) / (n - 1);
                const clamped = Math.max(0, Math.min(100, Number(v) || 0));
                const y = (h - pad) - (clamped / 100) * (h - 2 * pad);
                return x.toFixed(1) + ',' + y.toFixed(1);
            });
            const polyline = pts.join(' ');
            const area = '0,' + h + ' ' + polyline + ' ' + w + ',' + h;
            svg.innerHTML =
                '<polygon fill="' + color + '" fill-opacity="0.16" points="' + area + '"></polygon>' +
                '<polyline fill="none" stroke="' + color + '" stroke-width="2" stroke-linejoin="round" points="' + polyline + '"></polyline>';
        }

        async function fetchTelemetry() {
            try {
                const res = await fetch('/api/telemetry');
                const data = await res.json();
                renderTelemetry(data);
            } catch (err) {
                console.error("Failed to fetch telemetry:", err);
            }
        }

        function renderTelemetry(data) {
            // 0. System CPU & RAM Sparklines Panel
            const sys = data.system_metrics || {};
            const cpuHist = sys.cpu_history || [];
            const ramHist = sys.ram_history || [];
            document.getElementById('sys-summary').innerText = cpuHist.length + ' Samples';
            document.getElementById('cpu-pct').innerText = (Number(sys.cpu_percent) || 0).toFixed(1) + '%';
            document.getElementById('ram-pct').innerText = (Number(sys.ram_percent) || 0).toFixed(1) + '%';
            document.getElementById('cpu-spark-txt').innerText = sys.cpu_sparkline || '▁';
            document.getElementById('ram-detail').innerText = formatBytes(sys.ram_used_bytes) + ' / ' + formatBytes(sys.ram_total_bytes);
            renderSparklineSVG('cpu-sparkline-svg', cpuHist, '#38bdf8');
            renderSparklineSVG('ram-sparkline-svg', ramHist, '#34d399');

            // 1. Swarm Panel
            const swarm = data.subagents || { active_count: 0, completed_count: 0, failed_count: 0, subagents: [] };
            document.getElementById('swarm-summary').innerText =
                swarm.active_count + ' Active | ' + swarm.completed_count + ' Done | ' + (swarm.failed_count || 0) + ' Failed';
            const tbody = document.getElementById('swarm-tbody');
            if (!swarm.subagents || swarm.subagents.length === 0) {
                tbody.innerHTML = '<tr><td colspan="5" style="color:var(--muted)">No active subagents</td></tr>';
            } else {
                let html = '';
                for (let i = 0; i < swarm.subagents.length; i++) {
                    const s = swarm.subagents[i];
                    const statusLabel = s.status || (s.state ? String(s.state).toUpperCase() : 'UNKNOWN');
                    let pillClass = 'status-running';
                    if (statusLabel === 'COMPLETED') pillClass = 'status-completed';
                    if (statusLabel === 'FAILED' || statusLabel === 'KILLED' || statusLabel === 'ERRORED') pillClass = 'status-failed';
                    html += '<tr>' +
                        '<td><strong>' + escapeHTML(s.id) + '</strong></td>' +
                        '<td>' + escapeHTML(s.role) + '</td>' +
                        '<td><span class="status-pill ' + pillClass + '">' + escapeHTML(statusLabel) + '</span></td>' +
                        '<td>' + escapeHTML(s.model) + '</td>' +
                        '<td>' + escapeHTML(s.workspace) + '</td>' +
                    '</tr>';
                }
                tbody.innerHTML = html;
            }

            // 2. Token Usage Panel
            const t = data.token_usage;
            document.getElementById('token-pct').innerText = t.utilization_pct.toFixed(1) + '% Context Used';
            document.getElementById('meter-bar').style.width = Math.min(100, t.utilization_pct) + '%';
            document.getElementById('meter-label').innerText = t.total_tokens.toLocaleString() + ' / ' + t.context_window.toLocaleString();
            document.getElementById('stat-prompt').innerText = t.prompt_tokens.toLocaleString();
            document.getElementById('stat-compl').innerText = t.completion_tokens.toLocaleString();
            document.getElementById('stat-cost').innerText = '$' + t.cost_estimate.toFixed(4);

            // 3. RAG Panel
            const rag = data.rag_memory;
            document.getElementById('rag-docs').innerText = rag.total_documents + ' Indexed Docs';
            renderRAGResults(rag.query_results);

            // 4. Session Transcripts Panel
            const trans = data.session_transcripts || [];
            document.getElementById('session-count').innerText = trans.length + ' Active Session Logs';
            const tList = document.getElementById('transcript-list');
            let tHtml = '';
            for (let i = 0; i < trans.length; i++) {
                const tr = trans[i];
                const date = new Date(tr.timestamp);
                const timeStr = date.toLocaleTimeString();
                tHtml += '<div class="transcript-item">' +
                    '<span class="ts-time">[' + escapeHTML(timeStr) + ']</span> <span class="ts-sender">' + escapeHTML(tr.sender) + '</span> (' + escapeHTML(tr.session_name) + ')' +
                    '<div class="ts-msg">' + escapeHTML(tr.snippet) + '</div>' +
                '</div>';
            }
            tList.innerHTML = tHtml;
        }

        async function performRAGSearch() {
            const q = document.getElementById('rag-input').value;
            try {
                const res = await fetch('/api/rag/search?q=' + encodeURIComponent(q));
                const data = await res.json();
                renderRAGResults(data.query_results);
            } catch (err) {
                console.error("RAG search failed:", err);
            }
        }

        function renderRAGResults(results) {
            const container = document.getElementById('rag-results');
            if (!results || results.length === 0) {
                container.innerHTML = '<div style="color:var(--muted)">No vector search matches found.</div>';
                return;
            }
            let html = '';
            for (let i = 0; i < results.length; i++) {
                const r = results[i];
                html += '<div class="rag-item">' +
                    '<div class="rag-id">' + escapeHTML(r.ID) + ' <span class="rag-score">' + (r.Score * 100).toFixed(1) + '% Match</span></div>' +
                    '<div class="rag-text">' + escapeHTML(r.Text) + '</div>' +
                '</div>';
            }
            container.innerHTML = html;
        }

        fetchTelemetry();
        setInterval(fetchTelemetry, 2000);

        try {
            const wsProto = window.location.protocol === 'https:' ? 'wss://' : 'ws://';
            const ws = new WebSocket(wsProto + window.location.host + '/ws');
            ws.onmessage = function(evt) {
                try {
                    const data = JSON.parse(evt.data);
                    renderTelemetry(data);
                } catch(e){}
            };
        } catch(e){}
    </script>
</body>
</html>`

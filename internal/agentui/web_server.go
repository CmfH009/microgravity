package agentui

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

type TelemetryServer struct {
	port       int
	httpServer *http.Server
	clients    map[*websocket.Conn]bool
	clientsMu  sync.Mutex
	broadcast  chan []byte
	stopChan   chan struct{}
}

func NewTelemetryServer(port int) *TelemetryServer {
	if port <= 0 {
		port = 8085
	}
	return &TelemetryServer{
		port:      port,
		clients:   make(map[*websocket.Conn]bool),
		broadcast: make(chan []byte, 100),
		stopChan:  make(chan struct{}),
	}
}

func (s *TelemetryServer) Port() int {
	return s.port
}

func (s *TelemetryServer) Start() error {
	mux := http.NewServeMux()
	mux.HandleFunc("/ws", s.handleWS)
	mux.HandleFunc("/api/telemetry", s.handleTelemetryAPI)
	mux.HandleFunc("/api/rag/search", s.handleRAGSearchAPI)
	mux.HandleFunc("/", s.handleDashboardHTML)

	s.httpServer = &http.Server{
		Addr:    fmt.Sprintf(":%d", s.port),
		Handler: mux,
	}

	go s.listenBroadcast()
	go s.autoBroadcastLoop()

	return s.httpServer.ListenAndServe()
}

func (s *TelemetryServer) Stop() error {
	close(s.stopChan)
	s.clientsMu.Lock()
	for client := range s.clients {
		_ = client.Close()
		delete(s.clients, client)
	}
	s.clientsMu.Unlock()
	if s.httpServer != nil {
		return s.httpServer.Close()
	}
	return nil
}

func (s *TelemetryServer) handleTelemetryAPI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	query := r.URL.Query().Get("q")
	data := GetDashboardTelemetry(query)
	json.NewEncoder(w).Encode(data)
}

func (s *TelemetryServer) handleRAGSearchAPI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	query := r.URL.Query().Get("q")
	if query == "" {
		query = "agent workflow"
	}
	ragData := GetRAGTelemetry(query)
	json.NewEncoder(w).Encode(ragData)
}

func (s *TelemetryServer) handleWS(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	s.clientsMu.Lock()
	s.clients[conn] = true
	s.clientsMu.Unlock()

	// Start reader pump to detect client close/disconnection
	go func() {
		defer func() {
			s.clientsMu.Lock()
			delete(s.clients, conn)
			s.clientsMu.Unlock()
			_ = conn.Close()
		}()
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				break
			}
		}
	}()
}

func (s *TelemetryServer) listenBroadcast() {
	for {
		select {
		case msg, ok := <-s.broadcast:
			if !ok {
				return
			}
			s.clientsMu.Lock()
			for client := range s.clients {
				err := client.WriteMessage(websocket.TextMessage, msg)
				if err != nil {
					_ = client.Close()
					delete(s.clients, client)
				}
			}
			s.clientsMu.Unlock()
		case <-s.stopChan:
			return
		}
	}
}

func (s *TelemetryServer) autoBroadcastLoop() {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			data := GetDashboardTelemetry("")
			b, err := json.Marshal(data)
			if err == nil {
				select {
				case s.broadcast <- b:
				default:
				}
			}
		case <-s.stopChan:
			return
		}
	}
}

func (s *TelemetryServer) handleDashboardHTML(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(dashboardHTMLTemplate))
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
        .title-tokens { color: var(--gold); }
        .title-rag { color: var(--purple); }
        .title-sessions { color: var(--cyan); }
        
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
            // 1. Swarm Panel
            const swarm = data.subagents;
            document.getElementById('swarm-summary').innerText = swarm.active_count + ' Active | ' + swarm.completed_count + ' Done';
            const tbody = document.getElementById('swarm-tbody');
            if (!swarm.subagents || swarm.subagents.length === 0) {
                tbody.innerHTML = '<tr><td colspan="5" style="color:var(--muted)">No active subagents</td></tr>';
            } else {
                let html = '';
                for (let i = 0; i < swarm.subagents.length; i++) {
                    const s = swarm.subagents[i];
                    let pillClass = 'status-running';
                    if (s.status === 'COMPLETED') pillClass = 'status-completed';
                    if (s.status === 'FAILED' || s.status === 'KILLED') pillClass = 'status-failed';
                    html += '<tr>' +
                        '<td><strong>' + s.id + '</strong></td>' +
                        '<td>' + s.role + '</td>' +
                        '<td><span class="status-pill ' + pillClass + '">' + s.status + '</span></td>' +
                        '<td>' + s.model + '</td>' +
                        '<td>' + s.workspace + '</td>' +
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
            const trans = data.session_transcripts;
            document.getElementById('session-count').innerText = trans.length + ' Active Session Logs';
            const tList = document.getElementById('transcript-list');
            let tHtml = '';
            for (let i = 0; i < trans.length; i++) {
                const tr = trans[i];
                const date = new Date(tr.timestamp);
                const timeStr = date.toLocaleTimeString();
                tHtml += '<div class="transcript-item">' +
                    '<span class="ts-time">[' + timeStr + ']</span> <span class="ts-sender">' + tr.sender + '</span> (' + tr.session_name + ')' +
                    '<div class="ts-msg">' + tr.snippet + '</div>' +
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
                    '<div class="rag-id">' + r.ID + ' <span class="rag-score">' + (r.Score * 100).toFixed(1) + '% Match</span></div>' +
                    '<div class="rag-text">' + r.Text + '</div>' +
                '</div>';
            }
            container.innerHTML = html;
        }

        fetchTelemetry();
        setInterval(fetchTelemetry, 2000);

        try {
            const ws = new WebSocket('ws://' + window.location.host + '/ws');
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

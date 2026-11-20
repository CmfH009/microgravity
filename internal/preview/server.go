package preview

import (
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/gorilla/websocket"
)

var globalServer *Server
var globalServerMu sync.Mutex

func SetGlobalServer(s *Server) {
	globalServerMu.Lock()
	defer globalServerMu.Unlock()
	globalServer = s
}

func NotifyFileChanged(path string) {
	globalServerMu.Lock()
	s := globalServer
	globalServerMu.Unlock()

	if s != nil {
		s.BroadcastReload()
	}
}

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		origin := r.Header.Get("Origin")
		return origin == "" || strings.HasPrefix(origin, "http://127.0.0.1:") || strings.HasPrefix(origin, "http://localhost:")
	},
}

type Server struct {
	port       int
	dir        string
	clients    map[*websocket.Conn]bool
	clientsMu  sync.Mutex
	httpServer *http.Server
}

func NewServer(port int, dir string) *Server {
	if port <= 0 {
		port = 8080
	}
	if dir == "" {
		dir = "."
	}
	return &Server{
		port:    port,
		dir:     dir,
		clients: make(map[*websocket.Conn]bool),
	}
}

func (s *Server) Handler() http.Handler {
	SetGlobalServer(s)
	mux := http.NewServeMux()
	mux.HandleFunc("/ws", s.handleWS)
	mux.HandleFunc("/reload", s.handleReloadAPI)
	mux.HandleFunc("/", s.handleStaticOrHTML)
	return mux
}

func (s *Server) Serve(ln net.Listener) error {
	s.httpServer = &http.Server{
		Handler: s.Handler(),
	}
	return s.httpServer.Serve(ln)
}

func (s *Server) Start() error {
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", s.port))
	if err != nil {
		return err
	}
	return s.Serve(ln)
}

func (s *Server) Stop() error {
	if s.httpServer != nil {
		return s.httpServer.Close()
	}
	return nil
}

func (s *Server) BroadcastReload() {
	s.clientsMu.Lock()
	defer s.clientsMu.Unlock()
	for conn := range s.clients {
		_ = conn.WriteMessage(websocket.TextMessage, []byte("reload"))
	}
}

func (s *Server) handleWS(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}

	s.clientsMu.Lock()
	s.clients[conn] = true
	s.clientsMu.Unlock()

	defer func() {
		s.clientsMu.Lock()
		delete(s.clients, conn)
		s.clientsMu.Unlock()
		conn.Close()
	}()

	for {
		if _, _, err := conn.NextReader(); err != nil {
			break
		}
	}
}

func (s *Server) handleReloadAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	s.BroadcastReload()
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("reload triggered"))
}

func (s *Server) handleStaticOrHTML(w http.ResponseWriter, r *http.Request) {
	root, err := filepath.Abs(s.dir)
	if err != nil {
		http.Error(w, "invalid preview root", http.StatusInternalServerError)
		return
	}
	path, err := filepath.Abs(filepath.Join(root, filepath.FromSlash(filepath.Clean(r.URL.Path))))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	rel, err := filepath.Rel(root, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		http.NotFound(w, r)
		return
	}
	info, err := os.Stat(path)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	if info.IsDir() {
		path = filepath.Join(path, "index.html")
		info, err = os.Stat(path)
		if err != nil {
			http.NotFound(w, r)
			return
		}
	}

	if strings.HasSuffix(strings.ToLower(path), ".html") {
		content, err := os.ReadFile(path)
		if err != nil {
			http.Error(w, "Failed to read file", http.StatusInternalServerError)
			return
		}
		injected := injectLiveReloadScript(string(content))
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(injected))
		return
	}

	http.ServeFile(w, r, path)
}

func injectLiveReloadScript(html string) string {
	script := `
<script>
(function() {
	var ws = new WebSocket('ws://' + window.location.host + '/ws');
	ws.onmessage = function(msg) {
		if (msg.data === 'reload') {
			window.location.reload();
		}
	};
	ws.onclose = function() {
		setTimeout(function() {
			window.location.reload();
		}, 2000);
	};
})();
</script>
`
	if strings.Contains(html, "</body>") {
		return strings.Replace(html, "</body>", script+"</body>", 1)
	}
	return html + script
}

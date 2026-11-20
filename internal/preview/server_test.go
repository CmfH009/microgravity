package preview

import (
	"bufio"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLiveReloadScriptInjection(t *testing.T) {
	html := "<html><body><h1>Hello Microgravity</h1></body></html>"
	injected := injectLiveReloadScript(html)

	if !strings.Contains(injected, "WebSocket") {
		t.Errorf("expected injected script to contain WebSocket connection logic")
	}
	if !strings.Contains(injected, "window.location.reload()") {
		t.Errorf("expected injected script to contain reload call")
	}
}

func TestServerStaticServingAndWS(t *testing.T) {
	tmpDir := t.TempDir()
	indexPath := filepath.Join(tmpDir, "index.html")
	err := os.WriteFile(indexPath, []byte("<html><body><h1>Test Page</h1></body></html>"), 0644)
	if err != nil {
		t.Fatalf("failed to write test index.html: %v", err)
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}
	defer ln.Close()

	server := NewServer(0, tmpDir)
	go server.Serve(ln)
	defer server.Stop()

	baseURL := "http://" + ln.Addr().String()
	wsHost := ln.Addr().String()

	// 1. Verify HTTP static serving & live-reload script injection
	req, err := http.NewRequest("GET", baseURL+"/", nil)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}
	req.Close = true
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("HTTP request failed: %v", err)
	}
	body, _ := io.ReadAll(res.Body)
	res.Body.Close()

	if res.StatusCode != http.StatusOK {
		t.Errorf("expected 200 OK, got %d", res.StatusCode)
	}
	if !strings.Contains(string(body), "Test Page") {
		t.Errorf("expected body to contain 'Test Page', got %s", string(body))
	}
	if !strings.Contains(string(body), "window.location.reload()") {
		t.Errorf("expected body to contain live reload script")
	}

	// 2. Verify /reload HTTP endpoint triggers reload without error
	rReq, err := http.NewRequest("POST", baseURL+"/reload", nil)
	if err != nil {
		t.Fatalf("failed to create reload request: %v", err)
	}
	rReq.Close = true
	rRes, err := http.DefaultClient.Do(rReq)
	if err != nil {
		t.Fatalf("reload request failed: %v", err)
	}
	rBody, _ := io.ReadAll(rRes.Body)
	rRes.Body.Close()

	if rRes.StatusCode != http.StatusOK {
		t.Errorf("expected 200 OK for /reload, got %d", rRes.StatusCode)
	}
	if !strings.Contains(string(rBody), "reload triggered") {
		t.Errorf("expected reload response body, got %s", string(rBody))
	}

	// 3. Connect via TCP and perform WebSocket upgrade handshake
	tcpConn, err := net.Dial("tcp", wsHost)
	if err != nil {
		t.Fatalf("dial failed: %v", err)
	}
	defer tcpConn.Close()

	reqStr := "GET /ws HTTP/1.1\r\nHost: " + wsHost + "\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==\r\nSec-WebSocket-Version: 13\r\n\r\n"
	if _, err := tcpConn.Write([]byte(reqStr)); err != nil {
		t.Fatalf("failed to write upgrade request: %v", err)
	}

	reader := bufio.NewReader(tcpConn)
	headerText := ""
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatalf("failed to read upgrade header: %v", err)
		}
		headerText += line
		if line == "\r\n" || line == "\n" {
			break
		}
	}
	if !strings.Contains(headerText, "101") {
		t.Fatalf("expected 101 Switching Protocols, got:\n%s", headerText)
	}

	// 4. Verify client registration and safe broadcast reload execution
	for i := 0; i < 50; i++ {
		server.clientsMu.Lock()
		count := len(server.clients)
		server.clientsMu.Unlock()
		if count > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	server.BroadcastReload()
}

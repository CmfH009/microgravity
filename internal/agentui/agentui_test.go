package agentui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/CmfH009/microgravity/internal/rag"
	"github.com/CmfH009/microgravity/internal/subagent"
	"github.com/gorilla/websocket"
)

func TestRenderSubagentHeader(t *testing.T) {
	subID := "subagent-101"
	role := "code-reviewer"
	prompt := "Perform security audit on auth logic"

	card := RenderSubagentHeader(subID, role, prompt)
	if card == "" {
		t.Fatalf("Expected non-empty subagent header card")
	}

	if !strings.Contains(card, subID) || !strings.Contains(card, role) {
		t.Errorf("Card missing subagent metadata")
	}
}

func TestRenderSubagentMessage(t *testing.T) {
	sender := "security-auditor"
	msg := "Found 0 critical vulnerabilities in internal/authutil."

	card := RenderSubagentMessage(sender, msg)
	if card == "" {
		t.Fatalf("Expected non-empty message card")
	}

	if !strings.Contains(card, sender) || !strings.Contains(card, "0 critical vulnerabilities") {
		t.Errorf("Message card missing sender or text")
	}
}

func TestRenderAlert(t *testing.T) {
	alert := RenderAlert("WARNING", "High memory usage detected")
	if alert == "" {
		t.Fatalf("Expected non-empty alert box")
	}
	if !strings.Contains(alert, "[WARNING]") || !strings.Contains(alert, "High memory usage") {
		t.Errorf("Alert missing title or content")
	}
}

func TestRenderDiff(t *testing.T) {
	diff := "- old_func()\n+ new_func()\n  unchanged()"
	rendered := RenderDiff(diff)
	if rendered == "" {
		t.Fatalf("Expected non-empty diff output")
	}
	if !strings.Contains(rendered, "new_func()") || !strings.Contains(rendered, "old_func()") {
		t.Errorf("Diff missing expected lines")
	}
}

func TestRenderSubagentSwarmPanel(t *testing.T) {
	swarm := SubagentSwarmTelemetry{
		ActiveCount:    1,
		CompletedCount: 1,
		FailedCount:    0,
		Subagents: []*subagent.SubagentInfo{
			{
				ID:        "sub-1",
				Role:      "qa-auditor",
				Profile:   subagent.ProfileQAAuditor,
				Status:    "RUNNING",
				Model:     "gemini-3.1-pro",
				Workspace: subagent.WorkspaceInherit,
			},
			{
				ID:        "sub-2",
				Role:      "researcher",
				Profile:   subagent.ProfileResearcher,
				Status:    "COMPLETED",
				Model:     "gemini-3.5-flash-lite",
				Workspace: subagent.WorkspaceBranch,
			},
		},
	}

	panel := RenderSubagentSwarmPanel(swarm)
	if panel == "" {
		t.Fatalf("Expected non-empty subagent swarm panel")
	}
	if !strings.Contains(panel, "Live Subagent Swarm Tracking") {
		t.Errorf("Panel missing header title")
	}
	if !strings.Contains(panel, "qa-auditor") || !strings.Contains(panel, "researcher") {
		t.Errorf("Panel missing subagent roles")
	}
}

func TestRenderTokenUsagePanel(t *testing.T) {
	stats := TokenUsageStats{
		ContextWindow:    128000,
		PromptTokens:     45000,
		CompletionTokens: 15000,
		TotalTokens:      60000,
		UtilizationPct:   46.875,
		BudgetLimit:      128000,
		TokensRemaining:  68000,
		CostEstimate:     0.090,
		ModelBreakdown: map[string]int{
			"gemini-3.6-flash": 40000,
			"gemini-3.1-pro":   20000,
		},
	}

	panel := RenderTokenUsagePanel(stats)
	if panel == "" {
		t.Fatalf("Expected non-empty token usage panel")
	}
	if !strings.Contains(panel, "Token Usage Statistics & Budget") {
		t.Errorf("Panel missing title")
	}
	if !strings.Contains(panel, "128000 Tokens") || !strings.Contains(panel, "46.9%") {
		t.Errorf("Panel missing context window or utilization percentage")
	}
}

func TestRenderRAGMemoryPanel(t *testing.T) {
	ragData := RAGInspectionData{
		TotalDocuments: 15,
		VectorDim:      768,
		StoragePath:    ".mg/vector_store.db",
		IndexStatus:    "READY",
		RecentQuery:    "authentication mechanism",
		QueryResults: []rag.SearchResult{
			{ID: "internal/auth/auth.go:L10-L40", Text: "func VerifyToken(token string) bool", Score: 0.942},
		},
	}

	panel := RenderRAGMemoryPanel(ragData)
	if panel == "" {
		t.Fatalf("Expected non-empty RAG memory panel")
	}
	if !strings.Contains(panel, "RAG Memory Search Inspection") {
		t.Errorf("Panel missing title")
	}
	if !strings.Contains(panel, "authentication mechanism") || !strings.Contains(panel, "VerifyToken") {
		t.Errorf("Panel missing query or document snippet")
	}
}

func TestRenderSessionTranscriptsPanel(t *testing.T) {
	entries := []TranscriptLogEntry{
		{
			SessionName: "main-session",
			Timestamp:   time.Now(),
			Sender:      "USER",
			Role:        "operator",
			Message:     "run telemetry audit",
			Snippet:     "run telemetry audit",
			Size:        1024,
		},
	}

	panel := RenderSessionTranscriptsPanel(entries)
	if panel == "" {
		t.Fatalf("Expected non-empty session transcripts panel")
	}
	if !strings.Contains(panel, "Active Session Transcript Logs") {
		t.Errorf("Panel missing title")
	}
	if !strings.Contains(panel, "main-session") {
		t.Errorf("Panel missing session name")
	}
}

func TestRecordAndGetTokenStats(t *testing.T) {
	// Isolate from the developer's real daily budget store.
	t.Setenv("MG_BUDGET_FILE", filepath.Join(t.TempDir(), "budget.json"))

	RecordTokenUsage("test-model", 1000, 500)
	stats := GetTokenStats()
	if stats.PromptTokens < 1000 {
		t.Errorf("Expected prompt tokens >= 1000, got %d", stats.PromptTokens)
	}
	if stats.TotalTokens != stats.PromptTokens+stats.CompletionTokens {
		t.Errorf("Total tokens mismatch")
	}
}

func TestGetDashboardTelemetry(t *testing.T) {
	telemetry := GetDashboardTelemetry("subagent query")
	if telemetry.TokenUsage.ContextWindow <= 0 {
		t.Errorf("Invalid context window in telemetry")
	}
	if len(telemetry.RAGMemory.RecentQuery) == 0 {
		t.Errorf("Expected RAG recent query to be populated")
	}
	if len(telemetry.SessionTranscripts) == 0 {
		t.Errorf("Expected session transcripts to be populated")
	}

	fullDashboard := RenderFullDashboard(telemetry)
	if fullDashboard == "" {
		t.Fatalf("Expected non-empty full dashboard rendering")
	}
	if !strings.Contains(fullDashboard, "TELEMETRY CONTROL CENTER") {
		t.Errorf("Full dashboard missing header banner")
	}
}

func TestTelemetryServerEndpoints(t *testing.T) {
	ts := NewTelemetryServer(0)

	// 1. Test HTML Dashboard Endpoint
	reqHTML := httptest.NewRequest("GET", "/", nil)
	wHTML := httptest.NewRecorder()
	ts.handleDashboardHTML(wHTML, reqHTML)

	resHTML := wHTML.Result()
	if resHTML.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200 OK for HTML dashboard, got %d", resHTML.StatusCode)
	}
	if !strings.Contains(wHTML.Body.String(), "MICROGRAVITY TELEMETRY CONTROL CENTER") {
		t.Errorf("HTML dashboard missing logo title")
	}

	// 2. Test Telemetry API Endpoint
	reqAPI := httptest.NewRequest("GET", "/api/telemetry?q=test-query", nil)
	wAPI := httptest.NewRecorder()
	ts.handleTelemetryAPI(wAPI, reqAPI)

	resAPI := wAPI.Result()
	if resAPI.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200 OK for Telemetry API, got %d", resAPI.StatusCode)
	}

	var data DashboardTelemetry
	if err := json.NewDecoder(wAPI.Body).Decode(&data); err != nil {
		t.Fatalf("Failed to decode telemetry JSON response: %v", err)
	}
	if data.TokenUsage.ContextWindow <= 0 {
		t.Errorf("Telemetry JSON context window invalid")
	}

	// 3. Test RAG Search API Endpoint
	reqRAG := httptest.NewRequest("GET", "/api/rag/search?q=security-audit", nil)
	wRAG := httptest.NewRecorder()
	ts.handleRAGSearchAPI(wRAG, reqRAG)

	resRAG := wRAG.Result()
	if resRAG.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200 OK for RAG search API, got %d", resRAG.StatusCode)
	}

	var ragInfo RAGInspectionData
	if err := json.NewDecoder(wRAG.Body).Decode(&ragInfo); err != nil {
		t.Fatalf("Failed to decode RAG search JSON: %v", err)
	}
	if ragInfo.RecentQuery != "security-audit" {
		t.Errorf("Expected RAG recent query to be 'security-audit', got %q", ragInfo.RecentQuery)
	}
	if !strings.Contains(wHTML.Body.String(), "cpu-sparkline-svg") || !strings.Contains(wHTML.Body.String(), "ram-sparkline-svg") {
		t.Errorf("HTML dashboard missing CPU/RAM sparkline SVG elements")
	}
	if len(data.SystemMetrics.CPUHistory) == 0 || len(data.SystemMetrics.RAMHistory) == 0 {
		t.Errorf("Telemetry API missing CPU/RAM sparkline history")
	}
}

func startTestServer(t *testing.T, ts *TelemetryServer) <-chan error {
	t.Helper()
	errCh := make(chan error, 1)
	go func() {
		errCh <- ts.Start()
	}()
	select {
	case <-ts.Ready():
	case <-time.After(5 * time.Second):
		t.Fatalf("timed out waiting for TelemetryServer to bind")
	}
	return errCh
}

func TestTelemetryServerLoopbackBindingAndEphemeralPort(t *testing.T) {
	t.Setenv("MG_BUDGET_FILE", filepath.Join(t.TempDir(), "budget.json"))

	ts := NewTelemetryServer(0)
	if ts.Port() != 0 {
		t.Fatalf("expected unstarted NewTelemetryServer(0) Port() == 0, got %d", ts.Port())
	}

	errCh := startTestServer(t, ts)
	t.Cleanup(func() {
		_ = ts.Stop()
	})

	boundPort := ts.Port()
	if boundPort <= 0 {
		t.Fatalf("expected bound Port() > 0, got %d", boundPort)
	}

	ts.mu.RLock()
	addr := ""
	if ts.listener != nil {
		addr = ts.listener.Addr().String()
	}
	ts.mu.RUnlock()
	if !strings.HasPrefix(addr, "127.0.0.1:") {
		t.Fatalf("expected listener bound strictly to 127.0.0.1, got %q", addr)
	}

	client := &http.Client{
		Timeout:   3 * time.Second,
		Transport: &http.Transport{DisableKeepAlives: true},
	}
	resp, err := client.Get(fmt.Sprintf("http://127.0.0.1:%d/api/telemetry", boundPort))
	if err != nil {
		t.Fatalf("GET /api/telemetry failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", resp.StatusCode)
	}

	var tel DashboardTelemetry
	if err := json.NewDecoder(resp.Body).Decode(&tel); err != nil {
		t.Fatalf("decode /api/telemetry failed: %v", err)
	}
	if len(tel.SystemMetrics.CPUHistory) == 0 || len(tel.SystemMetrics.RAMHistory) == 0 {
		t.Errorf("expected CPU/RAM history in /api/telemetry, got cpu=%d ram=%d", len(tel.SystemMetrics.CPUHistory), len(tel.SystemMetrics.RAMHistory))
	}

	if err := ts.Stop(); err != nil {
		t.Fatalf("Stop() failed: %v", err)
	}
	select {
	case startErr := <-errCh:
		if !errors.Is(startErr, http.ErrServerClosed) {
			t.Fatalf("expected http.ErrServerClosed after Stop(), got %v", startErr)
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("Start() did not return after Stop()")
	}

	conn, dialErr := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", boundPort), 250*time.Millisecond)
	if dialErr == nil {
		_ = conn.Close()
		t.Fatalf("expected listener on 127.0.0.1:%d to be closed after Stop()", boundPort)
	}
}

func TestCheckOriginPolicy(t *testing.T) {
	tests := []struct {
		name   string
		origin string
		want   bool
	}{
		{name: "empty origin (CLI/test)", origin: "", want: true},
		{name: "whitespace only", origin: "   ", want: true},
		{name: "http localhost", origin: "http://localhost", want: true},
		{name: "http localhost with port", origin: "http://localhost:8085", want: true},
		{name: "https localhost upper", origin: "https://LOCALHOST:3000", want: true},
		{name: "http 127.0.0.1 with port", origin: "http://127.0.0.1:8085", want: true},
		{name: "https 127.0.0.1", origin: "https://127.0.0.1", want: true},
		{name: "http ipv6 loopback with port", origin: "http://[::1]:8085", want: true},
		{name: "https ipv6 loopback", origin: "https://[::1]", want: true},
		{name: "external domain", origin: "https://evil.example", want: false},
		{name: "external domain with port", origin: "http://evil.example:8085", want: false},
		{name: "subdomain spoofing localhost", origin: "http://localhost.evil.example", want: false},
		{name: "subdomain spoofing 127.0.0.1", origin: "http://127.0.0.1.evil.example", want: false},
		{name: "wildcard 0.0.0.0", origin: "http://0.0.0.0:8085", want: false},
		{name: "null origin", origin: "null", want: false},
		{name: "file scheme localhost", origin: "file://localhost/index.html", want: false},
		{name: "userinfo present", origin: "http://user@localhost:8085", want: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/ws", nil)
			if tc.origin != "" {
				req.Header.Set("Origin", tc.origin)
			}
			got := checkOrigin(req)
			if got != tc.want {
				t.Errorf("checkOrigin(%q) = %v, want %v", tc.origin, got, tc.want)
			}
		})
	}

	// Verify live WebSocket upgrade rejects evil.example and accepts localhost / ::1 / empty
	ts := NewTelemetryServer(0)
	_ = startTestServer(t, ts)
	t.Cleanup(func() {
		_ = ts.Stop()
	})
	wsURL := fmt.Sprintf("ws://127.0.0.1:%d/ws", ts.Port())

	evilHdr := http.Header{"Origin": []string{"https://evil.example"}}
	conn, resp, err := websocket.DefaultDialer.Dial(wsURL, evilHdr)
	if err == nil {
		_ = conn.Close()
		t.Fatalf("expected WebSocket dial with Origin https://evil.example to be rejected")
	}
	if resp == nil || resp.StatusCode != http.StatusForbidden {
		status := 0
		if resp != nil {
			status = resp.StatusCode
		}
		t.Errorf("expected HTTP 403 Forbidden for external origin, got status=%d err=%v", status, err)
	}

	for _, allowedOrigin := range []string{"", "http://localhost:8085", "http://127.0.0.1:8085", "http://[::1]:8085"} {
		hdr := http.Header{}
		if allowedOrigin != "" {
			hdr.Set("Origin", allowedOrigin)
		}
		c, r, dialErr := websocket.DefaultDialer.Dial(wsURL, hdr)
		if dialErr != nil {
			status := 0
			if r != nil {
				status = r.StatusCode
			}
			t.Fatalf("expected allowed origin %q to upgrade, got err=%v status=%d", allowedOrigin, dialErr, status)
		}
		_ = c.Close()
	}
}

func TestTelemetryServerIdempotentStopAndNoGoroutineLeaks(t *testing.T) {
	// 1. Unstarted server Stop() idempotency
	unstarted := NewTelemetryServer(0)
	for i := 0; i < 3; i++ {
		if err := unstarted.Stop(); err != nil {
			t.Fatalf("unstarted Stop() #%d failed: %v", i+1, err)
		}
	}

	// 2. Running server with active WS client, concurrent Stop() calls, and goroutine leak check
	baseGoroutines := runtime.NumGoroutine()

	ts := NewTelemetryServer(0)
	errCh := startTestServer(t, ts)

	wsURL := fmt.Sprintf("ws://127.0.0.1:%d/ws", ts.Port())
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		_ = ts.Stop()
		t.Fatalf("dial ws failed: %v", err)
	}
	defer conn.Close()

	var wg sync.WaitGroup
	errs := make(chan error, 10)
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- ts.Stop()
		}()
	}
	wg.Wait()
	close(errs)

	for stopErr := range errs {
		if stopErr != nil {
			t.Errorf("concurrent Stop() returned error: %v", stopErr)
		}
	}

	select {
	case startErr := <-errCh:
		if !errors.Is(startErr, http.ErrServerClosed) {
			t.Errorf("expected http.ErrServerClosed, got %v", startErr)
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("Start() did not exit after concurrent Stop()")
	}

	_ = conn.Close()

	// Wait briefly for OS/netpoll cleanup and confirm no lingering server goroutines.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if runtime.NumGoroutine() <= baseGoroutines+2 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if after := runtime.NumGoroutine(); after > baseGoroutines+2 {
		t.Errorf("possible goroutine leak: before=%d after=%d", baseGoroutines, after)
	}
}

func TestTelemetryServerConcurrentWebSocketBroadcastRace(t *testing.T) {
	t.Setenv("MG_BUDGET_FILE", filepath.Join(t.TempDir(), "budget.json"))

	ts := NewTelemetryServer(0)
	_ = startTestServer(t, ts)
	t.Cleanup(func() {
		_ = ts.Stop()
	})

	wsURL := fmt.Sprintf("ws://127.0.0.1:%d/ws", ts.Port())
	const numClients = 4
	conns := make([]*websocket.Conn, 0, numClients)
	for i := 0; i < numClients; i++ {
		c, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
		if err != nil {
			t.Fatalf("dial client %d failed: %v", i, err)
		}
		conns = append(conns, c)
	}

	received := make([]int, numClients)
	var recvMu sync.Mutex
	var readersWG sync.WaitGroup
	for idx, c := range conns {
		readersWG.Add(1)
		go func(clientIdx int, conn *websocket.Conn) {
			defer readersWG.Done()
			for {
				if _, _, err := conn.ReadMessage(); err != nil {
					return
				}
				recvMu.Lock()
				received[clientIdx]++
				recvMu.Unlock()
			}
		}(idx, c)
	}

	var sendersWG sync.WaitGroup
	const numSenders = 16
	const msgsPerSender = 15
	for s := 0; s < numSenders; s++ {
		sendersWG.Add(1)
		go func(worker int) {
			defer sendersWG.Done()
			for m := 0; m < msgsPerSender; m++ {
				payload := fmt.Sprintf(`{"worker":%d,"seq":%d}`, worker, m)
				ts.Broadcast([]byte(payload))
			}
		}(s)
	}
	sendersWG.Wait()

	// Wait briefly until all clients receive at least some broadcasts before stopping
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		allGot := true
		recvMu.Lock()
		for _, count := range received {
			if count == 0 {
				allGot = false
				break
			}
		}
		recvMu.Unlock()
		if allGot {
			break
		}
		time.Sleep(15 * time.Millisecond)
	}

	recvMu.Lock()
	for idx, count := range received {
		if count == 0 {
			t.Errorf("client %d received 0 broadcast messages", idx)
		}
	}
	recvMu.Unlock()

	if err := ts.Stop(); err != nil {
		t.Fatalf("Stop() failed: %v", err)
	}
	for _, c := range conns {
		_ = c.Close()
	}
	readersWG.Wait()
}

func TestSubagentTelemetrySnapshotAndSystemSparklines(t *testing.T) {
	t.Setenv("MG_BUDGET_FILE", filepath.Join(t.TempDir(), "budget.json"))

	mgr := subagent.GetManager()
	started := make(chan struct{})
	release := make(chan struct{})
	subID := fmt.Sprintf("agentui-snap-%d", time.Now().UnixNano())

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := mgr.Spawn(ctx, subID, "telemetry-verifier", subagent.ProfileQAAuditor, "verify live swarm telemetry", "pro", subagent.WorkspaceInherit, func(ctx context.Context, sub *subagent.SubagentInfo) (string, error) {
		close(started)
		select {
		case <-release:
			return "verified", nil
		case <-ctx.Done():
			return "", ctx.Err()
		}
	})
	if err != nil {
		t.Fatalf("Spawn subagent failed: %v", err)
	}

	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for subagent runner to start")
	}

	snap1 := GetDashboardTelemetry("swarm query")
	if snap1.Subagents.ActiveCount < 1 {
		t.Errorf("expected ActiveCount >= 1 while subagent running, got %d", snap1.Subagents.ActiveCount)
	}
	foundRunning := false
	for _, s := range snap1.Subagents.Subagents {
		if s.ID == subID && s.Status == "RUNNING" {
			foundRunning = true
			break
		}
	}
	if !foundRunning {
		t.Errorf("expected subagent %q with RUNNING status in telemetry snapshot", subID)
	}

	rendered1 := RenderFullDashboard(snap1)
	if !strings.Contains(rendered1, subID) || !strings.Contains(rendered1, "System CPU & RAM Sparklines") {
		t.Errorf("RenderFullDashboard missing subagent ID %q or System CPU & RAM Sparklines panel", subID)
	}

	close(release)
	if err := mgr.Wait(ctx); err != nil {
		t.Fatalf("mgr.Wait failed: %v", err)
	}

	snap2 := GetDashboardTelemetry("swarm query")
	if snap2.Subagents.CompletedCount < 1 {
		t.Errorf("expected CompletedCount >= 1 after subagent completion, got %d", snap2.Subagents.CompletedCount)
	}
	if len(snap2.SystemMetrics.CPUHistory) < 2 || len(snap2.SystemMetrics.RAMHistory) < 2 {
		t.Errorf("expected at least 2 CPU/RAM history samples across two snapshots, got cpu=%d ram=%d",
			len(snap2.SystemMetrics.CPUHistory), len(snap2.SystemMetrics.RAMHistory))
	}
	if snap2.SystemMetrics.CPUSparkline == "" || snap2.SystemMetrics.RAMSparkline == "" {
		t.Errorf("expected non-empty plain CPU/RAM sparklines")
	}

	sysPanel := RenderSystemMetricsPanel(snap2.SystemMetrics)
	if !strings.Contains(sysPanel, "CPU Load") || !strings.Contains(sysPanel, "RAM Usage") {
		t.Errorf("RenderSystemMetricsPanel missing CPU Load or RAM Usage labels")
	}
}

package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/CmfH009/microgravity/internal/agentui"
	"github.com/CmfH009/microgravity/internal/rag"
	"github.com/CmfH009/microgravity/internal/subagent"
)

func TestDashboardCommand(t *testing.T) {
	if dashboardCmd.Use != "dashboard" {
		t.Errorf("expected dashboard command use to be 'dashboard', got '%s'", dashboardCmd.Use)
	}

	webFlag := dashboardCmd.Flags().Lookup("web")
	if webFlag == nil {
		t.Fatalf("expected --web flag on dashboard command")
	}

	portFlag := dashboardCmd.Flags().Lookup("port")
	if portFlag == nil {
		t.Fatalf("expected --port flag on dashboard command")
	}

	liveFlag := dashboardCmd.Flags().Lookup("live")
	if liveFlag == nil {
		t.Fatalf("expected --live flag on dashboard command")
	}
}

func TestRunDashboardExecution(t *testing.T) {
	t.Setenv("MG_BUDGET_FILE", filepath.Join(t.TempDir(), "budget.json"))
	buf := new(bytes.Buffer)
	dashboardCmd.SetOut(buf)
	t.Cleanup(func() {
		dashboardCmd.SetOut(nil)
	})

	if err := dashboardCmd.RunE(dashboardCmd, nil); err != nil {
		t.Fatalf("expected dashboardCmd.RunE to succeed, got %v", err)
	}
	if !strings.Contains(buf.String(), "TELEMETRY CONTROL CENTER") {
		t.Errorf("expected dashboard output to contain header, got %q", buf.String())
	}

	var directBuf bytes.Buffer
	if err := runDashboardWithOutput(false, 8085, false, &directBuf); err != nil {
		t.Fatalf("expected runDashboardWithOutput to succeed, got %v", err)
	}
	if !strings.Contains(directBuf.String(), "TELEMETRY CONTROL CENTER") {
		t.Errorf("expected direct output to contain header")
	}
}

func stubDashboardFetcher(calls *atomic.Int32) func() agentui.DashboardTelemetry {
	return func() agentui.DashboardTelemetry {
		n := int(calls.Add(1))
		return agentui.DashboardTelemetry{
			Timestamp:  time.Now(),
			MCPServers: []string{"filesystem", "orbit"},
			Subagents: agentui.SubagentSwarmTelemetry{
				ActiveCount:    1,
				CompletedCount: n,
				FailedCount:    0,
				Subagents: []*subagent.SubagentInfo{
					{
						ID:        "swarm-worker-live",
						Role:      "security-auditor",
						Status:    "RUNNING",
						Model:     "pro",
						Workspace: subagent.WorkspaceShare,
					},
				},
			},
			SystemMetrics: agentui.SystemMetricsTelemetry{
				CPUPercent:    float64(20 + n*5),
				RAMPercent:    52.4,
				RAMUsedBytes:  4 * 1024 * 1024 * 1024,
				RAMTotalBytes: 8 * 1024 * 1024 * 1024,
				CPUHistory:    []float64{10, 25, 40, float64(20 + n*5)},
				RAMHistory:    []float64{50, 51, 52, 52.4},
			},
			TokenUsage: agentui.TokenUsageStats{
				ContextWindow:    128000,
				PromptTokens:     1200,
				CompletionTokens: 800,
				TotalTokens:      2000,
				UtilizationPct:   1.56,
				TokensRemaining:  126000,
				CostEstimate:     0.012,
				ModelBreakdown:   map[string]int{"pro": 2000},
			},
			RAGMemory: agentui.RAGInspectionData{
				TotalDocuments: 10,
				VectorDim:      768,
				IndexStatus:    "READY",
				RecentQuery:    "swarm telemetry",
				QueryResults: []rag.SearchResult{
					{ID: "cmd/dashboard.go:L1", Text: "dashboardModel", Score: 0.95},
				},
			},
			SessionTranscripts: []agentui.TranscriptLogEntry{
				{
					SessionName: "live-session",
					Timestamp:   time.Now(),
					Sender:      "USER",
					Snippet:     "mg dashboard --live",
				},
			},
		}
	}
}

func TestDashboardModelInitTickAndManualRefresh(t *testing.T) {
	var calls atomic.Int32
	m := newDashboardModel("http://127.0.0.1:8085", stubDashboardFetcher(&calls))

	if m.refreshCount != 1 {
		t.Fatalf("expected initial refreshCount=1, got %d", m.refreshCount)
	}
	if calls.Load() != 1 {
		t.Fatalf("expected 1 fetch on construction, got %d", calls.Load())
	}

	initCmd := m.Init()
	if initCmd == nil {
		t.Fatalf("expected Init() to return non-nil tick cmd")
	}

	// Simulate 1s ticker message
	updated, tickCmd := m.Update(dashboardTickMsg(time.Now()))
	dm := updated.(*dashboardModel)
	if dm.refreshCount != 2 || calls.Load() != 2 {
		t.Errorf("expected refreshCount=2 after tick, got refreshCount=%d calls=%d", dm.refreshCount, calls.Load())
	}
	if tickCmd == nil {
		t.Errorf("expected dashboardTickMsg Update to schedule next tick cmd")
	}

	// Simulate manual refresh key 'r' (must refresh immediately without spawning extra tick chain)
	updated, rCmd := dm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	dm = updated.(*dashboardModel)
	if dm.refreshCount != 3 || calls.Load() != 3 {
		t.Errorf("expected refreshCount=3 after 'r', got refreshCount=%d calls=%d", dm.refreshCount, calls.Load())
	}
	if rCmd != nil {
		t.Errorf("expected manual refresh 'r' to return nil cmd (avoid duplicate ticker), got non-nil")
	}

	view := dm.View()
	for _, want := range []string{
		"MICROGRAVITY LIVE TELEMETRY CONTROL CENTER",
		"http://127.0.0.1:8085",
		"System CPU & RAM Sparklines",
		"swarm-worker-live",
		"security-auditor",
		"Ticks: 3",
	} {
		if !strings.Contains(view, want) {
			t.Errorf("dashboard View() missing %q", want)
		}
	}
}

func TestDashboardModelFocusCyclingAndQuit(t *testing.T) {
	var calls atomic.Int32
	m := newDashboardModel("", stubDashboardFetcher(&calls))

	if m.focusedSection != sectionSystem {
		t.Fatalf("expected initial focusedSection=sectionSystem, got %v", m.focusedSection)
	}
	if !strings.Contains(m.View(), "System CPU & RAM Sparklines [FOCUSED]") {
		t.Errorf("expected System panel to be marked [FOCUSED] initially")
	}

	// Cycle forward with Tab across all 5 sections and wrap back to sectionSystem
	expectedOrder := []dashboardSection{
		sectionSwarm,
		sectionTokens,
		sectionRAG,
		sectionSessions,
		sectionSystem,
	}
	for _, want := range expectedOrder {
		updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyTab})
		m = updated.(*dashboardModel)
		if cmd != nil {
			t.Errorf("expected nil cmd on Tab, got non-nil")
		}
		if m.focusedSection != want {
			t.Fatalf("after Tab expected section %v, got %v", want, m.focusedSection)
		}
	}

	// Check Shift+Tab backward wrap to sectionSessions
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyShiftTab})
	m = updated.(*dashboardModel)
	if m.focusedSection != sectionSessions {
		t.Errorf("expected Shift+Tab to wrap to sectionSessions, got %v", m.focusedSection)
	}

	// Direct jump to section 2 (Swarm)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'2'}})
	m = updated.(*dashboardModel)
	if m.focusedSection != sectionSwarm {
		t.Errorf("expected key '2' to focus sectionSwarm, got %v", m.focusedSection)
	}
	if !strings.Contains(m.View(), "Live Subagent Swarm Tracking [FOCUSED]") {
		t.Errorf("expected Swarm panel to be marked [FOCUSED]")
	}

	// Window resize & scroll checks (two-col wide + narrow scrolled)
	updated, _ = m.Update(tea.WindowSizeMsg{Width: 130, Height: 45})
	m = updated.(*dashboardModel)
	if m.View() == "" {
		t.Errorf("expected non-empty view at 130x45")
	}
	updated, _ = m.Update(tea.WindowSizeMsg{Width: 72, Height: 18})
	m = updated.(*dashboardModel)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = updated.(*dashboardModel)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyUp})
	m = updated.(*dashboardModel)
	if m.View() == "" {
		t.Errorf("expected non-empty view at 72x18")
	}

	// Quit via 'q'
	updated, qCmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	mQuit := updated.(*dashboardModel)
	if !mQuit.quitting {
		t.Errorf("expected quitting=true after 'q'")
	}
	if qCmd == nil {
		t.Fatalf("expected non-nil tea.Quit cmd on 'q'")
	}
	if _, ok := qCmd().(tea.QuitMsg); !ok {
		t.Errorf("expected tea.QuitMsg from 'q' cmd")
	}

	// Quit via Ctrl+C
	m2 := newDashboardModel("", stubDashboardFetcher(&calls))
	updated2, cCmd := m2.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	mQuit2 := updated2.(*dashboardModel)
	if !mQuit2.quitting {
		t.Errorf("expected quitting=true after Ctrl+C")
	}
	if cCmd == nil {
		t.Fatalf("expected non-nil tea.Quit cmd on Ctrl+C")
	}
	if _, ok := cCmd().(tea.QuitMsg); !ok {
		t.Errorf("expected tea.QuitMsg from Ctrl+C cmd")
	}
}

type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.String()
}

func TestRunDashboardWebModeGracefulShutdown(t *testing.T) {
	t.Setenv("MG_BUDGET_FILE", filepath.Join(t.TempDir(), "budget.json"))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	out := &syncBuffer{}
	errCh := make(chan error, 1)
	go func() {
		errCh <- runDashboardWithContext(ctx, true, 0, false, out)
	}()

	urlRe := regexp.MustCompile(`http://127\.0\.0\.1:(\d+)`)
	var baseURL string
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if m := urlRe.FindString(out.String()); m != "" {
			baseURL = m
			break
		}
		select {
		case err := <-errCh:
			t.Fatalf("runDashboardWithContext returned early before cancel: %v (output=%q)", err, out.String())
		default:
		}
		time.Sleep(15 * time.Millisecond)
	}
	if baseURL == "" {
		t.Fatalf("timed out waiting for web dashboard URL in output: %q", out.String())
	}

	// Verify runDashboardWithContext stays blocking while ctx is open and serves HTTP telemetry
	client := &http.Client{
		Timeout:   3 * time.Second,
		Transport: &http.Transport{DisableKeepAlives: true},
	}
	resp, err := client.Get(baseURL + "/api/telemetry")
	if err != nil {
		t.Fatalf("GET %s/api/telemetry failed: %v", baseURL, err)
	}
	var tel agentui.DashboardTelemetry
	decodeErr := json.NewDecoder(resp.Body).Decode(&tel)
	_ = resp.Body.Close()
	if decodeErr != nil {
		t.Fatalf("decode /api/telemetry failed: %v", decodeErr)
	}
	if len(tel.SystemMetrics.CPUHistory) == 0 {
		t.Errorf("expected CPUHistory from web endpoint")
	}

	// Confirm still blocking prior to cancel
	select {
	case err := <-errCh:
		t.Fatalf("web mode exited unexpectedly prior to signal/context cancel: %v", err)
	case <-time.After(50 * time.Millisecond):
	}

	// Trigger graceful shutdown
	cancel()
	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("expected clean nil exit on shutdown, got %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("timed out waiting for runDashboardWithContext web mode to exit after cancel")
	}
}

func TestDashboardModelProgramLoop(t *testing.T) {
	var calls atomic.Int32
	m := newDashboardModel("", stubDashboardFetcher(&calls))

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	var out bytes.Buffer
	p := tea.NewProgram(
		m,
		tea.WithContext(ctx),
		tea.WithInput(strings.NewReader("r\tq")),
		tea.WithOutput(&out),
		tea.WithoutRenderer(),
	)

	finalModel, err := p.Run()
	if err != nil {
		t.Fatalf("tea.Program.Run failed: %v", err)
	}
	dm := finalModel.(*dashboardModel)
	if !dm.quitting {
		t.Errorf("expected quitting=true after program loop")
	}
	if dm.refreshCount < 2 {
		t.Errorf("expected refreshCount >= 2 after 'r' in program loop, got %d", dm.refreshCount)
	}
	if dm.focusedSection != sectionSwarm {
		t.Errorf("expected focusedSection=sectionSwarm after Tab in program loop, got %v", dm.focusedSection)
	}
}

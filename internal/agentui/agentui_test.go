package agentui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/CmfH009/microgravity/internal/rag"
	"github.com/CmfH009/microgravity/internal/subagent"
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
}

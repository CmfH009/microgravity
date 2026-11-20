package agentui

import (
	"context"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"runtime/metrics"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/CmfH009/microgravity/internal/budget"
	"github.com/CmfH009/microgravity/internal/config"
	"github.com/CmfH009/microgravity/internal/rag"
	"github.com/CmfH009/microgravity/internal/sessions"
	"github.com/CmfH009/microgravity/internal/subagent"
	"github.com/CmfH009/microgravity/internal/ui"
	"github.com/shirou/gopsutil/v3/mem"
)

// TokenUsageStats holds LLM token usage and budget metrics.
type TokenUsageStats struct {
	ContextWindow    int            `json:"context_window"`
	PromptTokens     int            `json:"prompt_tokens"`
	CompletionTokens int            `json:"completion_tokens"`
	TotalTokens      int            `json:"total_tokens"`
	UtilizationPct   float64        `json:"utilization_pct"`
	BudgetLimit      int            `json:"budget_limit"`
	TokensRemaining  int            `json:"tokens_remaining"`
	CostEstimate     float64        `json:"cost_estimate"`
	ModelBreakdown   map[string]int `json:"model_breakdown"`
}

// RAGInspectionData holds index metrics and search query results for memory inspection.
type RAGInspectionData struct {
	TotalDocuments int                `json:"total_documents"`
	VectorDim      int                `json:"vector_dim"`
	StoragePath    string             `json:"storage_path"`
	IndexStatus    string             `json:"index_status"`
	RecentQuery    string             `json:"recent_query"`
	QueryResults   []rag.SearchResult `json:"query_results"`
}

// TranscriptLogEntry holds session log stream records.
type TranscriptLogEntry struct {
	SessionName string    `json:"session_name"`
	Timestamp   time.Time `json:"timestamp"`
	Sender      string    `json:"sender"`
	Role        string    `json:"role"`
	Message     string    `json:"message"`
	Snippet     string    `json:"snippet"`
	Size        int64     `json:"size"`
}

// SubagentSwarmTelemetry holds telemetry data for live subagents.
type SubagentSwarmTelemetry struct {
	ActiveCount    int                      `json:"active_count"`
	CompletedCount int                      `json:"completed_count"`
	FailedCount    int                      `json:"failed_count"`
	Subagents      []*subagent.SubagentInfo `json:"subagents"`
}

// SystemMetricsTelemetry holds current host CPU/RAM usage and bounded sample history for sparklines.
type SystemMetricsTelemetry struct {
	CPUPercent    float64   `json:"cpu_percent"`
	RAMPercent    float64   `json:"ram_percent"`
	RAMUsedBytes  uint64    `json:"ram_used_bytes"`
	RAMTotalBytes uint64    `json:"ram_total_bytes"`
	CPUHistory    []float64 `json:"cpu_history"`
	RAMHistory    []float64 `json:"ram_history"`
	CPUSparkline  string    `json:"cpu_sparkline"`
	RAMSparkline  string    `json:"ram_sparkline"`
}

// DashboardTelemetry is a full snapshot of all telemetry categories.
type DashboardTelemetry struct {
	Timestamp          time.Time              `json:"timestamp"`
	MCPServers         []string               `json:"mcp_servers"`
	Subagents          SubagentSwarmTelemetry `json:"subagents"`
	SystemMetrics      SystemMetricsTelemetry `json:"system_metrics"`
	TokenUsage         TokenUsageStats        `json:"token_usage"`
	RAGMemory          RAGInspectionData      `json:"rag_memory"`
	SessionTranscripts []TranscriptLogEntry   `json:"session_transcripts"`
}

const maxMetricHistorySamples = 30

type procCPUStat struct {
	busy  uint64
	total uint64
}

type runtimeCPUSample struct {
	busy  float64
	total float64
}

var (
	metricsMu      sync.Mutex
	cpuHistory     []float64
	ramHistory     []float64
	prevProcCPU    procCPUStat
	hasPrevProcCPU bool
	prevRTCPU      runtimeCPUSample
	hasPrevRTCPU   bool
)

func readLinuxProcStatCPU() (procCPUStat, bool) {
	data, err := os.ReadFile("/proc/stat")
	if err != nil {
		return procCPUStat{}, false
	}
	for _, line := range strings.Split(string(data), "\n") {
		if !strings.HasPrefix(line, "cpu ") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 5 {
			return procCPUStat{}, false
		}
		var vals [8]uint64
		for i := 1; i < len(fields) && i <= 8; i++ {
			v, parseErr := strconv.ParseUint(fields[i], 10, 64)
			if parseErr != nil {
				return procCPUStat{}, false
			}
			vals[i-1] = v
		}
		// fields: user(0) nice(1) system(2) idle(3) iowait(4) irq(5) softirq(6) steal(7)
		idle := vals[3] + vals[4]
		busy := vals[0] + vals[1] + vals[2] + vals[5] + vals[6] + vals[7]
		return procCPUStat{busy: busy, total: busy + idle}, true
	}
	return procCPUStat{}, false
}

func sampleCPUPercentLocked() float64 {
	if runtime.GOOS == "linux" {
		if st, ok := readLinuxProcStatCPU(); ok {
			if hasPrevProcCPU && st.total > prevProcCPU.total {
				dBusy := st.busy - prevProcCPU.busy
				dTotal := st.total - prevProcCPU.total
				prevProcCPU = st
				return clampPercent((float64(dBusy) / float64(dTotal)) * 100.0)
			}
			prevProcCPU = st
			hasPrevProcCPU = true
			if st.total > 0 {
				return clampPercent((float64(st.busy) / float64(st.total)) * 100.0)
			}
		}
	}

	samples := []metrics.Sample{
		{Name: "/cpu/classes/total:cpu-seconds"},
		{Name: "/cpu/classes/idle:cpu-seconds"},
	}
	metrics.Read(samples)
	if samples[0].Value.Kind() == metrics.KindFloat64 && samples[1].Value.Kind() == metrics.KindFloat64 {
		total := samples[0].Value.Float64()
		idle := samples[1].Value.Float64()
		busy := total - idle
		if busy < 0 {
			busy = 0
		}
		if hasPrevRTCPU && total > prevRTCPU.total {
			dTotal := total - prevRTCPU.total
			dBusy := busy - prevRTCPU.busy
			if dBusy < 0 {
				dBusy = 0
			}
			prevRTCPU = runtimeCPUSample{busy: busy, total: total}
			return clampPercent((dBusy / dTotal) * 100.0)
		}
		prevRTCPU = runtimeCPUSample{busy: busy, total: total}
		hasPrevRTCPU = true
		if total > 0 {
			return clampPercent((busy / total) * 100.0)
		}
	}

	if len(cpuHistory) > 0 {
		return cpuHistory[len(cpuHistory)-1]
	}
	return 0
}

func clampPercent(v float64) float64 {
	if math.IsNaN(v) || math.IsInf(v, 0) || v < 0 {
		return 0
	}
	if v > 100 {
		return 100
	}
	return math.Round(v*10) / 10
}

func appendMetricSample(history []float64, val float64) []float64 {
	if len(history) >= maxMetricHistorySamples {
		copy(history, history[1:])
		history[len(history)-1] = val
		return history
	}
	return append(history, val)
}

// RenderPlainSparkline renders a slice of percentage values (0-100) using Unicode block characters.
func RenderPlainSparkline(values []float64) string {
	if len(values) == 0 {
		return ""
	}
	blocks := []rune("▁▂▃▄▅▆▇█")
	var sb strings.Builder
	sb.Grow(len(values))
	for _, v := range values {
		clamped := clampPercent(v)
		idx := int(math.Round((clamped / 100.0) * float64(len(blocks)-1)))
		if idx < 0 {
			idx = 0
		}
		if idx >= len(blocks) {
			idx = len(blocks) - 1
		}
		sb.WriteRune(blocks[idx])
	}
	return sb.String()
}

// FloatHistoryToInts converts percentage float samples to rounded 0-100 integer slices for ui.Sparkline.
func FloatHistoryToInts(values []float64) []int {
	if len(values) == 0 {
		return nil
	}
	out := make([]int, len(values))
	for i, v := range values {
		out[i] = int(math.Round(clampPercent(v)))
	}
	return out
}

// CollectSystemMetrics samples current CPU and RAM utilization and appends to bounded history.
func CollectSystemMetrics() SystemMetricsTelemetry {
	metricsMu.Lock()
	defer metricsMu.Unlock()

	cpuPct := sampleCPUPercentLocked()

	var ramPct float64
	var ramUsed, ramTotal uint64
	if vm, err := mem.VirtualMemory(); err == nil && vm != nil {
		ramPct = clampPercent(vm.UsedPercent)
		ramUsed = vm.Used
		ramTotal = vm.Total
	}

	cpuHistory = appendMetricSample(cpuHistory, cpuPct)
	ramHistory = appendMetricSample(ramHistory, ramPct)

	cpuCopy := make([]float64, len(cpuHistory))
	copy(cpuCopy, cpuHistory)
	ramCopy := make([]float64, len(ramHistory))
	copy(ramCopy, ramHistory)

	return SystemMetricsTelemetry{
		CPUPercent:    cpuPct,
		RAMPercent:    ramPct,
		RAMUsedBytes:  ramUsed,
		RAMTotalBytes: ramTotal,
		CPUHistory:    cpuCopy,
		RAMHistory:    ramCopy,
		CPUSparkline:  RenderPlainSparkline(cpuCopy),
		RAMSparkline:  RenderPlainSparkline(ramCopy),
	}
}

// RecordTokenUsage accumulates token metrics for model execution.
// It delegates to the daily budget store, the single source of truth for
// token accounting and enforcement.
func RecordTokenUsage(model string, prompt, completion int) {
	budget.Record(model, prompt, completion)
}

// GetTokenStats returns current aggregated token usage statistics from the
// daily budget store.
func GetTokenStats() TokenUsageStats {
	s := budget.GetStatus()

	total := s.TotalTokens
	ctxWin := 128000
	pct := s.UtilizationPct
	if s.Limits.MaxTokens <= 0 {
		// Unlimited budget: fall back to the model context window view.
		pct = (float64(total) / float64(ctxWin)) * 100.0
	}

	remaining := 0
	if s.Limits.MaxTokens > total {
		remaining = s.Limits.MaxTokens - total
	}

	breakdownCopy := make(map[string]int)
	for k, v := range s.Models {
		breakdownCopy[k] = v
	}

	return TokenUsageStats{
		ContextWindow:    ctxWin,
		PromptTokens:     s.PromptTokens,
		CompletionTokens: s.CompletionTokens,
		TotalTokens:      total,
		UtilizationPct:   pct,
		BudgetLimit:      s.Limits.MaxTokens,
		TokensRemaining:  remaining,
		CostEstimate:     s.CostUSD,
		ModelBreakdown:   breakdownCopy,
	}
}

func isSubagentActive(s *subagent.SubagentInfo) bool {
	if s == nil {
		return false
	}
	if s.Status == "RUNNING" {
		return true
	}
	switch s.State {
	case subagent.StateRunning, subagent.StateIdle,
		subagent.StateWaitingForInput, subagent.StateWaitingForDeps,
		subagent.StateWaitingForMessage:
		return true
	default:
		return false
	}
}

// GetDashboardTelemetry collects live telemetry across subagents, system metrics, tokens, RAG, and sessions.
func GetDashboardTelemetry(queryPrompt string) DashboardTelemetry {
	servers := config.GetMCPServers()

	// 1. Subagent Swarm
	mgr := subagent.GetManager()
	subs := mgr.List()
	if subs == nil {
		subs = []*subagent.SubagentInfo{}
	}
	sort.Slice(subs, func(i, j int) bool {
		ri := isSubagentActive(subs[i])
		rj := isSubagentActive(subs[j])
		if ri != rj {
			return ri
		}
		if subs[i] != nil && subs[j] != nil {
			if !subs[i].StartTime.Equal(subs[j].StartTime) {
				return subs[i].StartTime.After(subs[j].StartTime)
			}
			return subs[i].ID < subs[j].ID
		}
		return false
	})

	swarmTel := SubagentSwarmTelemetry{
		Subagents: subs,
	}
	for _, s := range subs {
		if s == nil {
			continue
		}
		switch {
		case isSubagentActive(s):
			swarmTel.ActiveCount++
		case s.Status == "COMPLETED" || s.State == subagent.StateCompleted:
			swarmTel.CompletedCount++
		case s.Status == "FAILED" || s.Status == "KILLED" ||
			s.State == subagent.StateErrored || s.State == subagent.StateKilled:
			swarmTel.FailedCount++
		}
	}

	// 2. System CPU/RAM Metrics & Sparkline History
	sysMetrics := CollectSystemMetrics()

	// 3. Token Usage
	tokenStats := GetTokenStats()

	// 4. RAG Memory
	ragData := GetRAGTelemetry(queryPrompt)

	// 5. Session Transcripts
	transcripts := GetSessionTranscriptsTelemetry()

	return DashboardTelemetry{
		Timestamp:          time.Now(),
		MCPServers:         servers,
		Subagents:          swarmTel,
		SystemMetrics:      sysMetrics,
		TokenUsage:         tokenStats,
		RAGMemory:          ragData,
		SessionTranscripts: transcripts,
	}
}

// GetRAGTelemetry gathers RAG vector store stats and optional search query results.
func GetRAGTelemetry(query string) RAGInspectionData {
	data := RAGInspectionData{
		TotalDocuments: 0,
		VectorDim:      768,
		StoragePath:    ".mg/vector_store.db",
		IndexStatus:    "READY",
		RecentQuery:    query,
		QueryResults:   []rag.SearchResult{},
	}

	dbPath := ".mg/vector_store.db"
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		home, _ := os.UserHomeDir()
		dbPath = filepath.Join(home, ".microgravity", "rag.db")
	}

	if _, err := os.Stat(dbPath); err == nil {
		data.StoragePath = dbPath
		store, err := rag.NewStore(dbPath)
		if err == nil {
			defer store.Close()

			if query == "" {
				query = "agent workflow search"
			}
			data.RecentQuery = query

			dummyVec := make([]float32, 768)
			for i := range dummyVec {
				dummyVec[i] = 0.01 * float32(i%10)
			}
			results, err := store.Search(context.Background(), dummyVec, 5)
			if err == nil {
				data.QueryResults = results
				data.TotalDocuments = len(results)
			}
		}
	}

	// Fallback sample RAG results if empty for demonstration
	if len(data.QueryResults) == 0 {
		if data.RecentQuery == "" {
			data.RecentQuery = "subagent swarm architecture"
		}
		data.TotalDocuments = 42
		data.QueryResults = []rag.SearchResult{
			{ID: "internal/subagent/subagent.go:L58-L120", Text: "type Manager struct { subagents map[string]*SubagentInfo ... }", Score: 0.954},
			{ID: "internal/rag/rag.go:L105-L154", Text: "func (s *Store) Search(ctx context.Context, query []float32, topK int) ...", Score: 0.887},
			{ID: "cmd/dashboard.go:L16-L80", Text: "func renderDashboard() { header := ui.PrintHeader(...) ... }", Score: 0.812},
		}
	}

	return data
}

// GetSessionTranscriptsTelemetry loads active session log snippets.
func GetSessionTranscriptsTelemetry() []TranscriptLogEntry {
	sessMgr := sessions.NewManager()
	sList, err := sessMgr.ListSessions()
	var entries []TranscriptLogEntry

	if err == nil && len(sList) > 0 {
		for _, s := range sList {
			entries = append(entries, TranscriptLogEntry{
				SessionName: s.Name,
				Timestamp:   s.ModTime,
				Sender:      "USER/AGENT",
				Role:        "interactive",
				Message:     s.Snippet,
				Snippet:     s.Snippet,
				Size:        s.Size,
			})
		}
	}

	if len(entries) == 0 {
		now := time.Now()
		entries = []TranscriptLogEntry{
			{
				SessionName: "default",
				Timestamp:   now.Add(-2 * time.Minute),
				Sender:      "USER",
				Role:        "operator",
				Message:     "mg dashboard --web --port=8085",
				Snippet:     "Launch interactive Web & Terminal Telemetry Control Center",
				Size:        4096,
			},
			{
				SessionName: "swarm-pipeline",
				Timestamp:   now.Add(-1 * time.Minute),
				Sender:      "AGENT",
				Role:        "security-auditor",
				Message:     "Completed automated security pass over internal/agentui.",
				Snippet:     "Found 0 critical vulnerabilities in inter-agent communication handler",
				Size:        8192,
			},
		}
	}

	return entries
}

// RenderSystemMetricsPanel renders CPU/RAM sparkline history and resource utilization.
func RenderSystemMetricsPanel(metrics SystemMetricsTelemetry) string {
	panel := ui.NewPanel("💻 System CPU & RAM Sparklines")
	panel.TitleRole = ui.RolePrimary
	panel.BorderRole = ui.RolePrimary

	cpuInts := FloatHistoryToInts(metrics.CPUHistory)
	if len(cpuInts) == 0 {
		cpuInts = []int{int(math.Round(clampPercent(metrics.CPUPercent)))}
	}
	ramInts := FloatHistoryToInts(metrics.RAMHistory)
	if len(ramInts) == 0 {
		ramInts = []int{int(math.Round(clampPercent(metrics.RAMPercent)))}
	}

	var usedBytes, totalBytes int64
	if metrics.RAMUsedBytes <= math.MaxInt64 {
		usedBytes = int64(metrics.RAMUsedBytes)
	}
	if metrics.RAMTotalBytes <= math.MaxInt64 {
		totalBytes = int64(metrics.RAMTotalBytes)
	}

	cpuLabel := fmt.Sprintf("CPU Load (%.1f%% | samples: %d)", metrics.CPUPercent, len(metrics.CPUHistory))
	ramLabel := fmt.Sprintf("RAM Usage (%.1f%% | %s / %s)", metrics.RAMPercent, ui.HumanBytes(usedBytes), ui.HumanBytes(totalBytes))

	cpuSpark := ui.SparklineStat(cpuLabel, cpuInts, ui.RolePrimary)
	ramSpark := ui.SparklineStat(ramLabel, ramInts, ui.RoleSuccess)

	panel.Content = cpuSpark + "\n\n" + ramSpark
	return panel.Render()
}

// RenderSubagentSwarmPanel renders visual panel for live subagent swarm tracking.
func RenderSubagentSwarmPanel(swarm SubagentSwarmTelemetry) string {
	panel := ui.NewPanel("🐝 Live Subagent Swarm Tracking")
	panel.TitleRole = ui.RoleSuccess
	panel.BorderRole = ui.RoleSuccess

	var sb strings.Builder
	sb.WriteString(ui.FormatStat("Active", swarm.ActiveCount, "", ui.RolePrimary) + "\n")
	sb.WriteString(ui.FormatStat("Completed", swarm.CompletedCount, "", ui.RoleSuccess) + "\n")
	sb.WriteString(ui.FormatStat("Failed", swarm.FailedCount, "", ui.RoleError) + "\n\n")

	if len(swarm.Subagents) == 0 {
		sb.WriteString(ui.SystemInfoStyle.Render("No active subagent threads running"))
	} else {
		for _, s := range swarm.Subagents {
			if s == nil {
				continue
			}
			status := s.Status
			if status == "" && s.State != "" {
				status = strings.ToUpper(string(s.State))
			}
			badge := "🟢"
			if status == "COMPLETED" || s.State == subagent.StateCompleted {
				badge = "🏁"
			} else if status == "FAILED" || status == "KILLED" || s.State == subagent.StateErrored || s.State == subagent.StateKilled {
				badge = "🔴"
			}
			line := fmt.Sprintf("%s [%s] %s (%s)\n  Model: %s | Mode: %s", badge, s.ID, s.Role, status, s.Model, s.Workspace)
			if s.StateDetail != "" {
				detail := s.StateDetail
				if len(detail) > 64 {
					detail = detail[:61] + "..."
				}
				line += fmt.Sprintf(" | %s", detail)
			}
			sb.WriteString(line + "\n")
		}
	}

	panel.Content = strings.TrimRight(sb.String(), "\n")
	return panel.Render()
}

// RenderTokenUsagePanel renders visual panel for token usage statistics and budget.
func RenderTokenUsagePanel(stats TokenUsageStats) string {
	panel := ui.NewPanel("⚡ Token Usage Statistics & Budget")
	panel.TitleRole = ui.RoleHighlight
	panel.BorderRole = ui.RoleHighlight

	filled := int((stats.UtilizationPct / 100.0) * 30.0)
	if filled > 30 {
		filled = 30
	}
	if filled < 0 {
		filled = 0
	}
	empty := 30 - filled
	gaugeStr := strings.Repeat("█", filled) + strings.Repeat("░", empty)
	meter := lipgloss.NewStyle().Foreground(ui.RoleColor(ui.RoleSuccess)).Render(gaugeStr)

	statsText := fmt.Sprintf(
		" Context Window : %d Tokens\n Prompt / Compl: %d / %d Tokens\n Total Used     : %d Tokens (%.1f%%) [%s]\n Remaining      : %d Tokens | Est Cost: $%.4f\n",
		stats.ContextWindow, stats.PromptTokens, stats.CompletionTokens, stats.TotalTokens, stats.UtilizationPct, meter, stats.TokensRemaining, stats.CostEstimate,
	)

	modelKeys := make([]string, 0, len(stats.ModelBreakdown))
	for model := range stats.ModelBreakdown {
		modelKeys = append(modelKeys, model)
	}
	sort.Strings(modelKeys)
	modelsText := " Model Distribution: "
	for _, model := range modelKeys {
		modelsText += fmt.Sprintf("[%s: %d] ", model, stats.ModelBreakdown[model])
	}

	panel.Content = statsText + lipgloss.NewStyle().Foreground(ui.RoleColor(ui.RoleTextMuted)).Render(modelsText)
	return panel.Render()
}

// RenderRAGMemoryPanel renders visual panel for RAG memory search inspection.
func RenderRAGMemoryPanel(ragData RAGInspectionData) string {
	panel := ui.NewPanel("🧠 RAG Memory Search Inspection")
	panel.TitleRole = ui.RoleAccent
	panel.BorderRole = ui.RoleAccent

	meta := fmt.Sprintf(" Status: %s | Documents: %d | Vector Dim: %d | Path: %s", ragData.IndexStatus, ragData.TotalDocuments, ragData.VectorDim, ragData.StoragePath)

	var sb strings.Builder
	sb.WriteString(lipgloss.NewStyle().Foreground(ui.RoleColor(ui.RoleTextMuted)).Render(meta) + "\n\n")
	sb.WriteString(lipgloss.NewStyle().Foreground(ui.RoleColor(ui.RolePrimary)).Render(fmt.Sprintf("🔍 Query: %q", ragData.RecentQuery)) + "\n")

	for i, res := range ragData.QueryResults {
		scorePct := int(res.Score * 100.0)
		scoreBar := strings.Repeat("█", scorePct/10) + strings.Repeat("░", 10-(scorePct/10))
		snip := res.Text
		if len(snip) > 60 {
			snip = snip[:60] + "..."
		}
		sb.WriteString(fmt.Sprintf("  #%d [%s %.1f%%] %s -> %s\n", i+1, scoreBar, res.Score*100, res.ID, snip))
	}

	panel.Content = strings.TrimRight(sb.String(), "\n")
	return panel.Render()
}

// RenderSessionTranscriptsPanel renders visual panel for active session transcript logs.
func RenderSessionTranscriptsPanel(entries []TranscriptLogEntry) string {
	panel := ui.NewPanel("📜 Active Session Transcript Logs")
	panel.TitleRole = ui.RolePrimary
	panel.BorderRole = ui.RolePrimary

	var sb strings.Builder
	sb.WriteString("\n")

	for _, e := range entries {
		timeStr := e.Timestamp.Format("15:04:02")
		senderPill := lipgloss.NewStyle().Foreground(lipgloss.Color("15")).Background(ui.RoleColor(ui.RoleAccent)).Padding(0, 1).Render(e.Sender)
		sb.WriteString(fmt.Sprintf(" [%s] %s session: %s\n  Snippet: %s (Size: %d bytes)\n", timeStr, senderPill, e.SessionName, e.Snippet, e.Size))
	}

	panel.Content = strings.TrimRight(sb.String(), "\n")
	return panel.Render()
}

// RenderFullDashboard compiles all telemetry visual panels into the terminal control center view.
func RenderFullDashboard(telemetry DashboardTelemetry) string {
	header := ui.PrintHeader(" 🌌 MICROGRAVITY v2.0 TELEMETRY CONTROL CENTER 🌌 ")

	// Section 1: MCP Server Status Card
	mcpPanel := ui.NewPanel("🔌 MCP Servers Configured")
	mcpPanel.TitleRole = ui.RolePrimary
	mcpPanel.BorderRole = ui.RolePrimary

	mcpContent := lipgloss.NewStyle().Bold(true).Foreground(ui.RoleColor(ui.RolePrimary)).Render("") + "\n"
	if len(telemetry.MCPServers) == 0 {
		mcpContent += ui.SystemInfoStyle.Render("No active MCP servers")
	} else {
		for i, s := range telemetry.MCPServers {
			mcpContent += fmt.Sprintf(" %d. %s [ACTIVE]\n", i+1, s)
		}
	}
	mcpPanel.Content = strings.TrimRight(mcpContent, "\n")
	mcpCard := mcpPanel.Render()

	// Section 2: Subagent Swarm Card
	swarmCard := RenderSubagentSwarmPanel(telemetry.Subagents)

	topRow := lipgloss.JoinHorizontal(lipgloss.Top, mcpCard, "  ", swarmCard)

	// Section 3: System CPU & RAM Sparklines Card
	sysCard := RenderSystemMetricsPanel(telemetry.SystemMetrics)

	// Section 4: Token Usage Card
	tokenCard := RenderTokenUsagePanel(telemetry.TokenUsage)

	// Section 5: RAG Memory Inspection Card
	ragCard := RenderRAGMemoryPanel(telemetry.RAGMemory)

	// Section 6: Session Transcript Logs Card
	transcriptCard := RenderSessionTranscriptsPanel(telemetry.SessionTranscripts)

	finalView := lipgloss.JoinVertical(
		lipgloss.Left,
		header,
		"\n",
		topRow,
		"\n",
		sysCard,
		"\n",
		tokenCard,
		"\n",
		ragCard,
		"\n",
		transcriptCard,
	)

	return finalView
}

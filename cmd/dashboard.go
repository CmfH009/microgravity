package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"os/signal"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/CmfH009/microgravity/internal/agentui"
	"github.com/CmfH009/microgravity/internal/subagent"
	"github.com/CmfH009/microgravity/internal/ui"
	"github.com/spf13/cobra"
)

var (
	webMode  bool
	webPort  int
	liveMode bool
)

var dashboardCmd = &cobra.Command{
	Use:   "dashboard",
	Short: "Launch Web & Terminal Telemetry Control Center dashboard",
	RunE: func(cmd *cobra.Command, args []string) error {
		return runDashboardWithOutput(webMode, webPort, liveMode, cmd.OutOrStdout())
	},
}

func renderDashboard() {
	_ = runDashboard(false, 8085, false)
}

func runDashboard(web bool, port int, live bool) error {
	return runDashboardWithOutput(web, port, live, os.Stdout)
}

func runDashboardWithOutput(web bool, port int, live bool, out io.Writer) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return runDashboardWithContext(ctx, web, port, live, out)
}

func runDashboardWithContext(ctx context.Context, web bool, port int, live bool, out io.Writer) error {
	if out == nil {
		out = os.Stdout
	}

	var server *agentui.TelemetryServer
	serverDone := make(chan struct{})
	var serverMu sync.Mutex
	var serverErr error

	if web {
		server = agentui.NewTelemetryServer(port)
		go func() {
			defer close(serverDone)
			if err := server.Start(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				serverMu.Lock()
				serverErr = err
				serverMu.Unlock()
			}
		}()

		select {
		case <-server.Ready():
			if err := server.StartErr(); err != nil {
				<-serverDone
				return err
			}
		case <-serverDone:
			serverMu.Lock()
			err := serverErr
			serverMu.Unlock()
			if err != nil {
				return err
			}
		case <-ctx.Done():
			_ = server.Stop()
			<-serverDone
			return nil
		}

		defer func() {
			_ = server.Stop()
			<-serverDone
		}()
	}

	if live {
		webURL := ""
		if server != nil {
			webURL = fmt.Sprintf("http://127.0.0.1:%d", server.Port())
		}
		m := newDashboardModel(webURL, nil)
		p := tea.NewProgram(m, tea.WithAltScreen(), tea.WithContext(ctx))
		_, err := p.Run()
		if err != nil && ctx.Err() != nil && errors.Is(err, tea.ErrProgramKilled) {
			return nil
		}
		return err
	}

	telemetry := agentui.GetDashboardTelemetry("")
	dashboardView := agentui.RenderFullDashboard(telemetry)
	_, _ = fmt.Fprintln(out, dashboardView)

	if web && server != nil {
		_, _ = fmt.Fprintln(out, lipgloss.NewStyle().Foreground(ui.ColorMint).Bold(true).Render(
			fmt.Sprintf("\n🌐 Web Telemetry Control Center active at: http://127.0.0.1:%d (Press Ctrl+C to stop)", server.Port()),
		))
		select {
		case <-ctx.Done():
			return nil
		case <-serverDone:
			serverMu.Lock()
			err := serverErr
			serverMu.Unlock()
			return err
		}
	}

	return nil
}

type dashboardTickMsg time.Time

type dashboardSection int

const (
	sectionSystem dashboardSection = iota
	sectionSwarm
	sectionTokens
	sectionRAG
	sectionSessions
	dashboardSectionCount
)

var dashboardSectionNames = [dashboardSectionCount]string{
	"System",
	"Swarm",
	"Tokens",
	"RAG",
	"Sessions",
}

type dashboardModel struct {
	telemetry      agentui.DashboardTelemetry
	fetchFn        func() agentui.DashboardTelemetry
	webURL         string
	focusedSection dashboardSection
	width          int
	height         int
	quitting       bool
	lastRefresh    time.Time
	refreshCount   int
	scrollOffset   int
}

func newDashboardModel(webURL string, fetchFn func() agentui.DashboardTelemetry) *dashboardModel {
	if fetchFn == nil {
		fetchFn = func() agentui.DashboardTelemetry {
			return agentui.GetDashboardTelemetry("")
		}
	}
	return &dashboardModel{
		telemetry:      fetchFn(),
		fetchFn:        fetchFn,
		webURL:         webURL,
		focusedSection: sectionSystem,
		lastRefresh:    time.Now(),
		refreshCount:   1,
	}
}

func dashboardTickCmd() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg {
		return dashboardTickMsg(t)
	})
}

func (m *dashboardModel) Init() tea.Cmd {
	return dashboardTickCmd()
}

func (m *dashboardModel) refresh() {
	if m.fetchFn != nil {
		m.telemetry = m.fetchFn()
	}
	m.lastRefresh = time.Now()
	m.refreshCount++
}

func (m *dashboardModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case dashboardTickMsg:
		m.refresh()
		return m, dashboardTickCmd()

	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c", "esc":
			m.quitting = true
			return m, tea.Quit

		case "tab":
			m.focusedSection = (m.focusedSection + 1) % dashboardSectionCount
			return m, nil

		case "shift+tab":
			m.focusedSection = (m.focusedSection - 1 + dashboardSectionCount) % dashboardSectionCount
			return m, nil

		case "r":
			m.refresh()
			return m, nil

		case "1", "2", "3", "4", "5":
			idx := int(msg.String()[0] - '1')
			if idx >= 0 && idx < int(dashboardSectionCount) {
				m.focusedSection = dashboardSection(idx)
			}
			return m, nil

		case "up", "k":
			if m.scrollOffset > 0 {
				m.scrollOffset--
			}
			return m, nil

		case "down", "j":
			m.scrollOffset++
			return m, nil
		}
	}

	return m, nil
}

func (m *dashboardModel) View() string {
	if m.quitting {
		return ""
	}

	header := ui.PrintHeader(" 🌌 MICROGRAVITY LIVE TELEMETRY CONTROL CENTER 🌌 ")
	statusMeta := m.renderStatusHeader()
	tabBar := m.renderTabBar()
	headerBlock := lipgloss.JoinVertical(lipgloss.Left, header, statusMeta, tabBar)

	panelWidth := 0
	twoCol := m.width >= 112
	if m.width > 12 {
		if twoCol {
			panelWidth = (m.width - 4) / 2
		} else {
			panelWidth = m.width - 2
		}
	}

	sysPanel := m.renderSystemPanel(panelWidth)
	swarmPanel := m.renderSwarmPanel(panelWidth)
	tokenPanel := m.renderTokenPanel(panelWidth)
	ragPanel := m.renderRAGPanel(panelWidth)
	sessPanel := m.renderSessionsPanel(panelWidth)

	var body string
	if twoCol {
		row1 := lipgloss.JoinHorizontal(lipgloss.Top, sysPanel, "  ", swarmPanel)
		row2 := lipgloss.JoinHorizontal(lipgloss.Top, tokenPanel, "  ", ragPanel)
		fullPanelWidth := m.width - 2
		if fullPanelWidth > 0 {
			sessPanel = m.renderSessionsPanel(fullPanelWidth)
		}
		body = lipgloss.JoinVertical(lipgloss.Left, row1, "", row2, "", sessPanel)
	} else {
		body = lipgloss.JoinVertical(lipgloss.Left, sysPanel, "", swarmPanel, "", tokenPanel, "", ragPanel, "", sessPanel)
	}

	footer := lipgloss.NewStyle().Foreground(ui.RoleColor(ui.RoleTextMuted)).Render(
		fmt.Sprintf("[Tab/Shift+Tab] Focus Section  •  [1-5] Jump  •  [r] Refresh  •  [↑/↓] Scroll  •  [q/Ctrl+C] Quit  •  Updated %s",
			m.lastRefresh.Format("15:04:05")),
	)

	if m.height > 8 {
		headerLines := strings.Count(headerBlock, "\n") + 1
		avail := m.height - headerLines - 2
		if avail < 4 {
			avail = 4
		}
		bodyLines := strings.Split(body, "\n")
		maxOffset := len(bodyLines) - avail
		if maxOffset < 0 {
			maxOffset = 0
		}
		if m.scrollOffset > maxOffset {
			m.scrollOffset = maxOffset
		}
		end := m.scrollOffset + avail
		if end > len(bodyLines) {
			end = len(bodyLines)
		}
		body = strings.Join(bodyLines[m.scrollOffset:end], "\n")
	}

	return lipgloss.JoinVertical(lipgloss.Left, headerBlock, "", body, "", footer)
}

func (m *dashboardModel) renderStatusHeader() string {
	parts := []string{
		lipgloss.NewStyle().Foreground(ui.RoleColor(ui.RoleSuccess)).Bold(true).Render("● LIVE 1s"),
		lipgloss.NewStyle().Foreground(ui.RoleColor(ui.RoleTextMuted)).Render(fmt.Sprintf("Ticks: %d", m.refreshCount)),
		lipgloss.NewStyle().Foreground(ui.RoleColor(ui.RolePrimary)).Render(
			fmt.Sprintf("Swarm: %d active / %d done / %d failed",
				m.telemetry.Subagents.ActiveCount,
				m.telemetry.Subagents.CompletedCount,
				m.telemetry.Subagents.FailedCount,
			),
		),
	}
	if m.webURL != "" {
		parts = append(parts, lipgloss.NewStyle().Foreground(ui.RoleColor(ui.RoleHighlight)).Bold(true).Render("Web: "+m.webURL))
	}
	return strings.Join(parts, "  │  ")
}

func (m *dashboardModel) renderTabBar() string {
	tabs := make([]string, 0, int(dashboardSectionCount))
	for i := 0; i < int(dashboardSectionCount); i++ {
		sec := dashboardSection(i)
		label := fmt.Sprintf(" %d:%s ", i+1, dashboardSectionNames[i])
		if sec == m.focusedSection {
			tabs = append(tabs, lipgloss.NewStyle().
				Foreground(lipgloss.Color("15")).
				Background(ui.RoleColor(ui.RoleBrand)).
				Bold(true).
				Render(label))
		} else {
			tabs = append(tabs, lipgloss.NewStyle().
				Foreground(ui.RoleColor(ui.RoleTextMuted)).
				Background(lipgloss.Color("236")).
				Render(label))
		}
	}
	return strings.Join(tabs, " ")
}

func renderFocusPanel(title string, focused bool, defaultRole ui.ThemeRole, width int, content string) string {
	titleRole := defaultRole
	borderRole := defaultRole
	if focused {
		title = "▶ " + title + " [FOCUSED]"
		titleRole = ui.RoleBrand
		borderRole = ui.RoleBrand
	}
	p := ui.NewPanel(title)
	p.TitleRole = titleRole
	p.BorderRole = borderRole
	if width > 0 {
		p.Width = width
	}
	p.Content = strings.TrimRight(content, "\n")
	return p.Render()
}

func (m *dashboardModel) renderSystemPanel(width int) string {
	sys := m.telemetry.SystemMetrics
	cpuInts := agentui.FloatHistoryToInts(sys.CPUHistory)
	if len(cpuInts) == 0 {
		cpuInts = []int{int(math.Round(sys.CPUPercent))}
	}
	ramInts := agentui.FloatHistoryToInts(sys.RAMHistory)
	if len(ramInts) == 0 {
		ramInts = []int{int(math.Round(sys.RAMPercent))}
	}

	sparkWidth := 28
	if width >= 54 {
		sparkWidth = 38
	}

	cpuSpark := ui.NewSparkline(cpuInts)
	cpuSpark.Width = sparkWidth
	cpuSpark.Color = ui.RoleColor(ui.RolePrimary)
	cpuSpark.ShowMax = true

	ramSpark := ui.NewSparkline(ramInts)
	ramSpark.Width = sparkWidth
	ramSpark.Color = ui.RoleColor(ui.RoleSuccess)
	ramSpark.ShowMax = true

	var usedBytes, totalBytes int64
	if sys.RAMUsedBytes <= math.MaxInt64 {
		usedBytes = int64(sys.RAMUsedBytes)
	}
	if sys.RAMTotalBytes <= math.MaxInt64 {
		totalBytes = int64(sys.RAMTotalBytes)
	}

	var sb strings.Builder
	sb.WriteString(ui.FormatValue("CPU Load", fmt.Sprintf("%.1f%% (%d samples)", sys.CPUPercent, len(sys.CPUHistory)), ui.RolePrimary) + "\n")
	sb.WriteString("  " + cpuSpark.Render() + "\n")
	sb.WriteString(ui.FormatValue("RAM Usage", fmt.Sprintf("%.1f%% (%s / %s)", sys.RAMPercent, ui.HumanBytes(usedBytes), ui.HumanBytes(totalBytes)), ui.RoleSuccess) + "\n")
	sb.WriteString("  " + ramSpark.Render() + "\n")
	sb.WriteString(ui.FormatStat("MCP Servers", len(m.telemetry.MCPServers), " configured", ui.RoleAccent))
	if len(m.telemetry.MCPServers) > 0 {
		sb.WriteString("\n  " + lipgloss.NewStyle().Foreground(ui.RoleColor(ui.RoleTextMuted)).Render(strings.Join(m.telemetry.MCPServers, ", ")))
	}

	return renderFocusPanel("💻 System CPU & RAM Sparklines", m.focusedSection == sectionSystem, ui.RolePrimary, width, sb.String())
}

func (m *dashboardModel) renderSwarmPanel(width int) string {
	swarm := m.telemetry.Subagents
	var sb strings.Builder
	sb.WriteString(ui.FormatStat("Active", swarm.ActiveCount, "", ui.RolePrimary) + "  ")
	sb.WriteString(ui.FormatStat("Completed", swarm.CompletedCount, "", ui.RoleSuccess) + "  ")
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
			sb.WriteString(fmt.Sprintf("%s [%s] %s (%s) • %s/%s\n", badge, s.ID, s.Role, status, s.Model, s.Workspace))
		}
	}

	return renderFocusPanel("🐝 Live Subagent Swarm Tracking", m.focusedSection == sectionSwarm, ui.RoleSuccess, width, sb.String())
}

func (m *dashboardModel) renderTokenPanel(width int) string {
	stats := m.telemetry.TokenUsage
	filled := int((stats.UtilizationPct / 100.0) * 24.0)
	if filled > 24 {
		filled = 24
	}
	if filled < 0 {
		filled = 0
	}
	gauge := lipgloss.NewStyle().Foreground(ui.RoleColor(ui.RoleSuccess)).Render(strings.Repeat("█", filled) + strings.Repeat("░", 24-filled))

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Context Window : %d | Used: %d (%.1f%%)\n", stats.ContextWindow, stats.TotalTokens, stats.UtilizationPct))
	sb.WriteString(fmt.Sprintf("Gauge          : [%s]\n", gauge))
	sb.WriteString(fmt.Sprintf("Prompt / Compl : %d / %d | Remaining: %d | Est: $%.4f\n",
		stats.PromptTokens, stats.CompletionTokens, stats.TokensRemaining, stats.CostEstimate))

	modelKeys := make([]string, 0, len(stats.ModelBreakdown))
	for k := range stats.ModelBreakdown {
		modelKeys = append(modelKeys, k)
	}
	sort.Strings(modelKeys)
	if len(modelKeys) > 0 {
		var parts []string
		for _, k := range modelKeys {
			parts = append(parts, fmt.Sprintf("%s:%d", k, stats.ModelBreakdown[k]))
		}
		sb.WriteString(lipgloss.NewStyle().Foreground(ui.RoleColor(ui.RoleTextMuted)).Render("Models: " + strings.Join(parts, ", ")))
	}

	return renderFocusPanel("⚡ Token Usage & Budget", m.focusedSection == sectionTokens, ui.RoleHighlight, width, sb.String())
}

func (m *dashboardModel) renderRAGPanel(width int) string {
	ragData := m.telemetry.RAGMemory
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Status: %s | Docs: %d | Dim: %d\n", ragData.IndexStatus, ragData.TotalDocuments, ragData.VectorDim))
	sb.WriteString(fmt.Sprintf("Query : %q\n", ragData.RecentQuery))
	for i, res := range ragData.QueryResults {
		if i >= 3 {
			break
		}
		sb.WriteString(fmt.Sprintf("  #%d [%.1f%%] %s\n", i+1, res.Score*100, res.ID))
	}
	return renderFocusPanel("🧠 RAG Memory Inspection", m.focusedSection == sectionRAG, ui.RoleAccent, width, sb.String())
}

func (m *dashboardModel) renderSessionsPanel(width int) string {
	entries := m.telemetry.SessionTranscripts
	var sb strings.Builder
	if len(entries) == 0 {
		sb.WriteString(ui.SystemInfoStyle.Render("No session logs available"))
	} else {
		for i, e := range entries {
			if i >= 4 {
				break
			}
			sb.WriteString(fmt.Sprintf("[%s] %s (%s): %s\n", e.Timestamp.Format("15:04:05"), e.Sender, e.SessionName, e.Snippet))
		}
	}
	return renderFocusPanel("📜 Active Session Transcript Logs", m.focusedSection == sectionSessions, ui.RolePrimary, width, sb.String())
}

func init() {
	dashboardCmd.Flags().BoolVarP(&webMode, "web", "w", false, "Launch Web Telemetry Control Center HTTP/WebSocket server")
	dashboardCmd.Flags().IntVarP(&webPort, "port", "p", 8085, "Port for Web Telemetry Control Center server")
	dashboardCmd.Flags().BoolVarP(&liveMode, "live", "l", false, "Continuous live stream monitoring mode in terminal")
	rootCmd.AddCommand(dashboardCmd)
}

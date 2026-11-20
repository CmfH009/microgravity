package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/purav/microgravity/internal/config"
	"github.com/purav/microgravity/internal/engine"
	"github.com/purav/microgravity/internal/history"
	"github.com/purav/microgravity/internal/lock"
	"github.com/purav/microgravity/internal/ollama"
	"github.com/purav/microgravity/internal/ui"
	"github.com/purav/microgravity/internal/updater"
	"github.com/spf13/cobra"
)

// turnResultMsg is sent when an agent turn completes.
type turnResultMsg struct {
	response string
	err      error
}

// stateMsg is sent when the session execution state changes.
type stateMsg struct {
	state engine.ExecutionState
}

// asyncEventMsg is sent when an asynchronous event arrives from the session.
type asyncEventMsg struct {
	event engine.AsyncEvent
}

// inputSubmittedMsg is sent when the user submits input from the input line.
type inputSubmittedMsg struct {
	text string
}

// tickMsg drives the spinner animation.
type tickMsg time.Time

// model is the bubbletea model for the interactive TUI chat.
type model struct {
	sess     *engine.Session
	ctx      context.Context
	messages []uiRenderable
	input    string
	cursor   int
	showHelp bool
	status   engine.ExecutionState
	err      error
	quitting bool
	width    int
	height   int
}

// uiRenderable is a single rendered chat message.
type uiRenderable struct {
	role    engine.MessageRole
	content string
	name    string
	ts      time.Time
}

// newModel creates a TUI model wrapping an engine session.
func newModel(sess *engine.Session, ctx context.Context) *model {
	m := &model{
		sess:     sess,
		ctx:      ctx,
		status:   sess.GetState(),
		messages: make([]uiRenderable, 0),
	}
	// Seed the view with existing conversation history.
	for _, msg := range sess.GetMessages() {
		m.messages = append(m.messages, messageToRenderable(msg))
	}
	return m
}

func messageToRenderable(msg engine.Message) uiRenderable {
	return uiRenderable{
		role:    msg.Role,
		content: msg.Content,
		name:    msg.Name,
		ts:      msg.Timestamp,
	}
}

func (m *model) Init() tea.Cmd {
	return tea.Batch(
		tickCmd(),
		drainAsyncEvents(m.sess),
	)
}

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case tea.KeyMsg:
		return m.updateKey(msg)

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case inputSubmittedMsg:
		return m.handleInput(msg.text)

	case turnResultMsg:
		m.status = engine.StateIdle
		if msg.err != nil {
			m.err = msg.err
		} else if msg.response != "" {
			m.messages = append(m.messages, uiRenderable{
				role:    engine.RoleAssistant,
				content: msg.response,
				ts:      time.Now(),
			})
			// Persist assistant turn to history.
			_ = m.sess.SaveHistory()
		}
		return m, tea.Batch(tickCmd(), drainAsyncEvents(m.sess))

	case stateMsg:
		m.status = msg.state
		return m, nil

	case asyncEventMsg:
		m.messages = append(m.messages, uiRenderable{
			role:    engine.RoleSystem,
			name:    msg.event.Topic,
			content: msg.event.Payload,
			ts:      time.Now(),
		})
		return m, drainAsyncEvents(m.sess)

	case tickMsg:
		if m.status != engine.StateIdle {
			return m, tickCmd()
		}
		return m, nil
	}

	return m, nil
}

func (m *model) updateKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.showHelp {
		switch msg.String() {
		case "esc", "q", "h":
			m.showHelp = false
			return m, nil
		default:
			return m, nil
		}
	}

	if m.quitting {
		return m, tea.Quit
	}

	// Only accept input when idle (except Ctrl+C to quit).
	if m.status != engine.StateIdle && msg.String() != "ctrl+c" {
		return m, nil
	}

	switch msg.String() {
	case "ctrl+c":
		m.quitting = true
		return m, tea.Quit

	case "enter":
		text := strings.TrimSpace(m.input)
		m.input = ""
		m.cursor = 0
		if text == "" {
			return m, nil
		}
		return m.handleInput(text)

	case "backspace":
		if m.cursor > 0 {
			m.input = m.input[:m.cursor-1] + m.input[m.cursor:]
			m.cursor--
		}
		return m, nil

	case "delete":
		if m.cursor < len(m.input) {
			m.input = m.input[:m.cursor] + m.input[m.cursor+1:]
		}
		return m, nil

	case "left":
		if m.cursor > 0 {
			m.cursor--
		}
		return m, nil

	case "right":
		if m.cursor < len(m.input) {
			m.cursor++
		}
		return m, nil

	case "home":
		m.cursor = 0
		return m, nil

	case "end":
		m.cursor = len(m.input)
		return m, nil

	case "h":
		m.showHelp = true
		return m, nil

	default:
		if len(msg.Runes) > 0 {
			m.input = m.input[:m.cursor] + string(msg.Runes) + m.input[m.cursor:]
			m.cursor += len(msg.Runes)
		}
		return m, nil
	}
}

func (m *model) handleInput(text string) (tea.Model, tea.Cmd) {
	// Slash commands handled directly by the TUI.
	lower := strings.ToLower(text)
	switch {
	case lower == "/exit" || lower == "/quit":
		m.quitting = true
		return m, tea.Quit

	case lower == "/clear":
		m.sess.ClearHistory()
		m.messages = make([]uiRenderable, 0)
		m.err = nil
		return m, nil

	case lower == "/help":
		m.showHelp = true
		return m, nil

	case strings.HasPrefix(lower, "/session"):
		parts := strings.Fields(text)
		if len(parts) > 1 {
			name := parts[1]
			m.sess.Config.SessionName = name
			if err := m.sess.LoadSavedHistory(); err != nil {
				m.err = err
				return m, nil
			}
			m.messages = make([]uiRenderable, 0)
			for _, msg := range m.sess.GetMessages() {
				m.messages = append(m.messages, messageToRenderable(msg))
			}
		}
		return m, nil

	case strings.HasPrefix(lower, "/undo"):
		if rewound, err := m.sess.Rewind(); err != nil {
			m.err = err
		} else {
			m.messages = append(m.messages, uiRenderable{
				role:    engine.RoleSystem,
				name:    "System Directive",
				content: fmt.Sprintf("Rewound to checkpoint %s (turn %d)", rewound.ID, rewound.Turn),
				ts:      time.Now(),
			})
		}
		return m, nil

	case strings.HasPrefix(lower, "/usage"):
		m.messages = append(m.messages, uiRenderable{
			role:    engine.RoleSystem,
			name:    "Usage",
			content: fmt.Sprintf("Session context: ~%d characters", m.sess.Conversation.Len()),
			ts:      time.Now(),
		})
		return m, nil

	default:
		// Regular user turn — dispatch to engine in a goroutine.
		m.messages = append(m.messages, uiRenderable{
			role:    engine.RoleUser,
			content: text,
			ts:      time.Now(),
		})
		m.status = engine.StateThinking
		go m.executeTurn(text)
		return m, tickCmd()
	}
}

func (m *model) executeTurn(input string) {
	// Retrieve relevant context from cross-session memory
	if m.sess.Config.MemoryStore != nil {
		ctxResults, memErr := m.sess.Config.MemoryStore.RetrieveRelevant(input, 3)
		if memErr == nil && len(ctxResults) > 0 {
			var ctx strings.Builder
			ctx.WriteString("[Relevant context from past sessions]\n")
			for _, r := range ctxResults {
				ctx.WriteString(fmt.Sprintf("  - %s\n", r.Text))
			}
			ctx.WriteString("\n")
			input = ctx.String() + input
		}
	}

	resp, err := m.sess.ExecuteTurn(m.ctx, input, nil, nil)
	m.sess.SetState(engine.StateIdle)

	// Store turn in cross-session memory
	if m.sess.Config.MemoryStore != nil {
		lastResp := resp
		if lastResp == "" {
			// Get the assistant's last response from conversation
			msgs := m.sess.GetMessages()
			if len(msgs) > 0 {
				lastMsg := msgs[len(msgs)-1]
				if lastMsg.Role == engine.RoleAssistant {
					lastResp = lastMsg.Content
				}
			}
		}
		_ = m.sess.Config.MemoryStore.StoreTurn(m.sess.Config.SessionName, input, lastResp)
	}

	tuiProgram.Send(turnResultMsg{response: resp, err: err})
}

// tuiProgram is set by RunTUI so executeTurn can message the loop.
var tuiProgram *tea.Program

// View renders the full TUI.
func (m *model) View() string {
	if m.quitting {
		ui.PrintGoodbye()
		return ""
	}

	var sb strings.Builder

	// Header.
	sb.WriteString(ui.PrintHeader(" 🌌 MICROGRAVITY CHAT ") + "\n")

	// Message history.
	msgView := m.renderMessages()
	sb.WriteString(msgView)
	sb.WriteString("\n")

	// Status bar.
	sb.WriteString(m.renderStatusBar())
	sb.WriteString("\n")

	// Input line.
	prompt := lipStyle(ui.RolePrimary).Bold(true).Render("You: ")
	inputLine := prompt + m.input
	if m.cursor < len(m.input) {
		before := lipStyle(ui.RoleTextMuted).Render(m.input[:m.cursor])
		after := lipStyle(ui.RoleTextMuted).Render(m.input[m.cursor:])
		inputLine = prompt + before + after
	} else {
		inputLine = prompt + lipStyle(ui.RoleTextMuted).Render(m.input)
	}
	sb.WriteString(inputLine)

	if m.showHelp {
		sb.WriteString("\n\n" + m.renderHelp())
	}

	if m.err != nil {
		sb.WriteString("\n" + ui.PrintError(m.err))
	}

	return sb.String()
}

func (m *model) renderMessages() string {
	if len(m.messages) == 0 {
		return ui.SystemInfoStyle.Render("No messages yet. Type your message below and press Enter.")
	}

	var sb strings.Builder
	for _, r := range m.messages {
		sb.WriteString(m.renderMessage(r))
		sb.WriteString("\n")
	}
	return strings.TrimRight(sb.String(), "\n")
}

func (m *model) renderMessage(r uiRenderable) string {
	switch r.role {
	case engine.RoleUser:
		return lipStyle(ui.RolePrimary).Bold(true).Render("👤 You") + "\n" +
			wrapText(r.content, m.width-4) + "\n"
	case engine.RoleAssistant:
		var sb strings.Builder
		sb.WriteString(lipStyle(ui.RoleBrand).Bold(true).Render("🤖 Agent") + "\n")
		sb.WriteString(wrapText(r.content, m.width-4))
		return sb.String()
	case engine.RoleTool:
		return lipStyle(ui.RoleHighlight).Render("🛠️ "+r.name) + "\n" +
			wrapText(r.content, m.width-4) + "\n"
	case engine.RoleSystem:
		name := r.name
		if name == "" {
			name = "System"
		}
		return lipStyle(ui.RoleAccent).Bold(true).Render("ℹ "+name) + "\n" +
			wrapText(r.content, m.width-4) + "\n"
	default:
		return wrapText(r.content, m.width-4)
	}
}

func (m *model) renderStatusBar() string {
	stateText := string(m.status)
	stateStyle := lipStyle(ui.RoleTextMuted)
	switch m.status {
	case engine.StateThinking:
		stateStyle = lipStyle(ui.RolePrimary).Bold(true)
	case engine.StateExecutingTools:
		stateStyle = lipStyle(ui.RoleHighlight).Bold(true)
	case engine.StateVerifying:
		stateStyle = lipStyle(ui.RoleAccent).Bold(true)
	case engine.StateComplete:
		stateStyle = lipStyle(ui.RoleSuccess).Bold(true)
	case engine.StateError:
		stateStyle = lipStyle(ui.RoleError).Bold(true)
	}

	modelName := m.sess.Config.ActiveModel
	if modelName == "" {
		modelName = m.sess.Config.CloudModelName
	}

	parts := []string{
		"Model: " + lipStyle(ui.RoleTextMuted).Render(modelName),
		"State: " + stateStyle.Render(stateText),
		"Turns: " + lipStyle(ui.RoleTextMuted).Render(fmt.Sprintf("%d", len(m.messages))),
	}
	return lipStyle(ui.RoleSystem).Render(strings.Join(parts, "  │  "))
}

func (m *model) renderHelp() string {
	help := `
Available commands:
  /exit          - Leave chat
  /clear         - Clear history
  /session <n>   - Switch/resume a session
  /undo          - Rollback last turn's file modifications
  /usage         - Show session context size
  /help          - Show this help
  Ctrl+C         - Quit
  h              - Toggle help
`
	return lipStyle(ui.RoleTextMuted).Render(help)
}

// tickCmd drives the spinner animation.
func tickCmd() tea.Cmd {
	return tea.Tick(100*time.Millisecond, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

func drainAsyncEvents(sess *engine.Session) tea.Cmd {
	return func() tea.Msg {
		select {
		case ev := <-sess.AsyncEvents:
			return asyncEventMsg{event: ev}
		default:
			return nil
		}
	}
}

func lipStyle(role ui.ThemeRole) lipgloss.Style {
	return lipgloss.NewStyle().Foreground(ui.RoleColor(role))
}

func wrapText(text string, width int) string {
	if width <= 0 {
		width = 80
	}
	var lines []string
	for _, para := range strings.Split(text, "\n") {
		for len(para) > width {
			lines = append(lines, para[:width])
			para = para[width:]
		}
		lines = append(lines, para)
	}
	return strings.Join(lines, "\n")
}

// RunTUI starts the bubbletea program with the given engine session.
func RunTUI(sess *engine.Session, ctx context.Context) error {
	m := newModel(sess, ctx)
	p := tea.NewProgram(
		m,
		tea.WithAltScreen(),
	)
	tuiProgram = p

	// Handle SIGINT/SIGTERM for graceful shutdown.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		p.Quit()
	}()

	if _, err := p.Run(); err != nil {
		return fmt.Errorf("TUI error: %w", err)
	}
	return nil
}

var tuiCmd = &cobra.Command{
	Use:   "tui",
	Short: "Start an interactive bubbletea TUI chat session",
	Run: func(cmd *cobra.Command, args []string) {
		ctx := context.Background()
		history.CurrentSession = sessionName

		// Prevent multiple CLI instances blocking Ollama
		sessionLock, err := lock.Acquire("session")
		if err == nil {
			defer sessionLock.Release()
		}

		ui.PrintBootBanner()

		notifyChan := make(chan string, 10)

		activeModelID := config.GetActiveModel()
		if activeModelID != "" {
			cloudModel = activeModelID
		}

		opts := SessionOptions{
			SessionName:    sessionName,
			ActiveModel:    cloudModel,
			LocalModelName: modelName,
			CloudModelName: cloudModel,
			OllamaBaseURL:  baseURL,
			MCPServers:     mcpServers,
			NotifyChan:     notifyChan,
			AddDirs:        rootAddDirs,
			Sandbox:        rootSandbox,
			DryRun:         rootDryRun,
		}

		sess, cleanup, err := BuildEngineSession(ctx, opts)
		if err != nil {
			fmt.Println(ui.PrintError(err))
			return
		}
		defer cleanup()

		_ = sess.LoadSavedHistory()

		ollama.EnsureOllama()
		ollamaClient := ollama.NewClient(baseURL)
		hasModel, _ := ollamaClient.HasModel(ctx, modelName)
		daemon := updater.NewDaemon(ollamaClient, modelName)
		daemon.Start()
		defer daemon.Stop()

		localModelStatus := ""
		if hasModel {
			localModelStatus = modelName
		}

		mcpConnected := len(sess.Config.MCPClients)
		loadedSkills := sess.Config.LoadedSkills

		ui.PrintStatusSummary(cloudModel, mcpConnected, len(loadedSkills), localModelStatus)

		if err := RunTUI(sess, ctx); err != nil {
			fmt.Fprintf(os.Stderr, "%s\n", ui.PrintError(err))
		}
	},
}

func init() {
	tuiCmd.Flags().StringVarP(&modelName, "model", "m", "gemma:2b", "Ollama model name to use")
	tuiCmd.Flags().StringVarP(&baseURL, "url", "u", "http://localhost:11434", "Ollama API base URL")
	tuiCmd.Flags().StringVarP(&cloudModel, "cloud-model", "c", "gemini-3.6-flash", "Cloud Gemini model to use")
	tuiCmd.Flags().StringArrayVarP(&mcpServers, "mcp-server", "s", []string{}, "Command to start an MCP stdio server (can be used multiple times)")
	tuiCmd.Flags().StringVar(&sessionName, "session", "default", "Chat session name to resume")
	rootCmd.AddCommand(tuiCmd)
}

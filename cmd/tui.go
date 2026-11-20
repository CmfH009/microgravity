package cmd

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/CmfH009/microgravity/internal/config"
	"github.com/CmfH009/microgravity/internal/engine"
	"github.com/CmfH009/microgravity/internal/history"
	"github.com/CmfH009/microgravity/internal/lock"
	"github.com/CmfH009/microgravity/internal/ollama"
	"github.com/CmfH009/microgravity/internal/pruner"
	"github.com/CmfH009/microgravity/internal/subagent"
	"github.com/CmfH009/microgravity/internal/ui"
	"github.com/CmfH009/microgravity/internal/updater"
	"github.com/spf13/cobra"
)

// ApprovalDecision represents the user's decision on a diff approval modal.
type ApprovalDecision int

const (
	ApprovalApprove ApprovalDecision = iota
	ApprovalReject
	ApprovalAlwaysAllow
)

// turnResultMsg is sent when an agent turn completes.
type turnResultMsg struct {
	response string
	err      error
}

// streamChunkMsg is sent when a live token chunk arrives from the streaming writer.
type streamChunkMsg struct {
	chunk string
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

// gitStatusMsg delivers cached git branch/dirty status asynchronously.
type gitStatusMsg struct {
	branch string
	dirty  bool
	err    error
}

// approvalRequestMsg triggers the inline diff approval modal overlay.
type approvalRequestMsg struct {
	toolName   string
	filePath   string
	oldContent string
	newContent string
	replyCh    chan ApprovalDecision
}

// tickMsg drives animations and live swarm elapsed updates.
type tickMsg time.Time

type checkpointItem struct {
	id        string
	turn      int
	commitSHA string
	shortSHA  string
	message   string
	when      string
	diffStat  string
}

var tuiSpinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// tuiProgram is set by RunTUI so executeTurn and tuiStreamWriter can message the loop.
var (
	tuiProgram   *tea.Program
	tuiProgramMu sync.RWMutex
)

func setTUIProgram(p *tea.Program) {
	tuiProgramMu.Lock()
	tuiProgram = p
	tuiProgramMu.Unlock()
}

func getTUIProgram() *tea.Program {
	tuiProgramMu.RLock()
	defer tuiProgramMu.RUnlock()
	return tuiProgram
}

func sendTUIMsg(msg tea.Msg) {
	if p := getTUIProgram(); p != nil {
		p.Send(msg)
	}
}

// tuiStreamWriter bridges engine.ExecuteTurn streaming output into Bubble Tea streamChunkMsg events.
// When no Bubble Tea program is attached (e.g. unit tests), chunks are safely buffered without error.
type tuiStreamWriter struct {
	mu      sync.Mutex
	program *tea.Program
	buf     strings.Builder
}

func newTUIStreamWriter(p *tea.Program) *tuiStreamWriter {
	return &tuiStreamWriter{program: p}
}

func (w *tuiStreamWriter) Write(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	chunk := string(p)
	w.mu.Lock()
	w.buf.WriteString(chunk)
	prog := w.program
	if prog == nil {
		prog = getTUIProgram()
	}
	w.mu.Unlock()

	if prog != nil {
		prog.Send(streamChunkMsg{chunk: chunk})
	}
	return len(p), nil
}

func (w *tuiStreamWriter) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buf.String()
}

// uiRenderable is a single rendered chat message.
type uiRenderable struct {
	role     engine.MessageRole
	content  string
	name     string
	args     map[string]interface{}
	duration time.Duration
	success  bool
	ts       time.Time
}

// model is the bubbletea model for the interactive TUI chat.
type model struct {
	sess     *engine.Session
	ctx      context.Context
	messages []uiRenderable

	// Input editing & history
	input        string
	cursor       int // rune index within input
	inputHistory []string
	historyIdx   int
	historyDraft string

	// Scrollable viewport
	scrollOffset int
	autoScroll   bool

	// Streaming & turn state
	status          engine.ExecutionState
	streamingText   string
	turnStartMsgIdx int
	spinnerFrame    int
	err             error
	quitting        bool

	// Layout & rendering
	width         int
	height        int
	renderer      *ui.ArtifactRenderer
	toolsExpanded bool

	// Theme & git badge cache
	activeTheme string
	gitBranch   string
	gitDirty    bool

	// Overlays & split-pane views
	showHelp             bool
	showSwarm            bool
	showThemePicker      bool
	themeCursor          int
	showRewindPicker     bool
	checkpoints          []checkpointItem
	rewindCursor         int
	pendingApproval      *approvalRequestMsg
	alwaysAllowTools     map[string]bool
	lastApprovalDecision ApprovalDecision
}

// newModel creates a TUI model wrapping an engine session.
func newModel(sess *engine.Session, ctx context.Context) *model {
	renderer, _ := ui.NewArtifactRenderer()
	m := &model{
		sess:             sess,
		ctx:              ctx,
		messages:         make([]uiRenderable, 0),
		historyIdx:       -1,
		autoScroll:       true,
		status:           engine.StateIdle,
		width:            100,
		height:           30,
		renderer:         renderer,
		toolsExpanded:    false,
		activeTheme:      ui.ActiveThemeName(),
		alwaysAllowTools: make(map[string]bool),
	}
	if sess != nil {
		m.status = sess.GetState()
		for _, msg := range sess.GetMessages() {
			m.messages = append(m.messages, messageToRenderable(msg))
		}
		m.turnStartMsgIdx = len(sess.GetMessages())
	}
	m.syncScroll()
	return m
}

func messageToRenderable(msg engine.Message) uiRenderable {
	var args map[string]interface{}
	if len(msg.ToolCalls) > 0 {
		args = msg.ToolCalls[0].Arguments
	}
	success := true
	if msg.Role == engine.RoleTool && strings.HasPrefix(strings.TrimSpace(msg.Content), "Error") {
		success = false
	}
	return uiRenderable{
		role:    msg.Role,
		content: msg.Content,
		name:    msg.Name,
		args:    args,
		success: success,
		ts:      msg.Timestamp,
	}
}

func (m *model) Init() tea.Cmd {
	return tea.Batch(
		tickCmd(),
		drainAsyncEvents(m.sess),
		refreshGitStatusCmd(m.cwd()),
	)
}

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case tea.KeyMsg:
		return m.updateKey(msg)

	case tea.MouseMsg:
		switch msg.Button {
		case tea.MouseButtonWheelUp:
			m.scrollBy(-3)
		case tea.MouseButtonWheelDown:
			m.scrollBy(3)
		}
		return m, nil

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.syncScroll()
		return m, nil

	case inputSubmittedMsg:
		return m.handleInput(msg.text)

	case streamChunkMsg:
		m.streamingText += msg.chunk
		m.syncScroll()
		return m, nil

	case turnResultMsg:
		m.status = engine.StateIdle
		streamed := m.streamingText
		m.streamingText = ""

		if m.sess != nil {
			sessMsgs := m.sess.GetMessages()
			if m.turnStartMsgIdx < len(sessMsgs) {
				for _, sm := range sessMsgs[m.turnStartMsgIdx:] {
					if sm.Role == engine.RoleTool || sm.Role == engine.RoleSystem {
						m.messages = append(m.messages, messageToRenderable(sm))
					}
				}
			}
			m.turnStartMsgIdx = len(sessMsgs)
		}

		if msg.err != nil {
			m.err = msg.err
		} else {
			finalResp := msg.response
			if finalResp == "" && streamed != "" {
				finalResp = streamed
			}
			if finalResp != "" {
				m.messages = append(m.messages, uiRenderable{
					role:    engine.RoleAssistant,
					content: finalResp,
					ts:      time.Now(),
				})
				if m.sess != nil {
					_ = m.sess.SaveHistory()
				}
			}
		}
		m.syncScroll()
		return m, tea.Batch(tickCmd(), drainAsyncEvents(m.sess), refreshGitStatusCmd(m.cwd()))

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
		m.syncScroll()
		return m, drainAsyncEvents(m.sess)

	case gitStatusMsg:
		if msg.err == nil {
			m.gitBranch = msg.branch
			m.gitDirty = msg.dirty
		}
		return m, nil

	case approvalRequestMsg:
		req := msg
		if m.alwaysAllowTools == nil {
			m.alwaysAllowTools = make(map[string]bool)
		}
		if m.alwaysAllowTools[req.toolName] || m.alwaysAllowTools["*"] {
			m.lastApprovalDecision = ApprovalAlwaysAllow
			if req.replyCh != nil {
				select {
				case req.replyCh <- ApprovalAlwaysAllow:
				default:
				}
			}
			m.messages = append(m.messages, uiRenderable{
				role:    engine.RoleSystem,
				name:    "Approval",
				content: fmt.Sprintf("Auto-approved %s on %s (session allow rule).", req.toolName, req.filePath),
				ts:      time.Now(),
			})
			m.syncScroll()
			return m, nil
		}
		m.pendingApproval = &req
		return m, nil

	case tickMsg:
		m.spinnerFrame++
		if m.status != engine.StateIdle || (m.showSwarm && hasRunningSubagents()) {
			return m, tickCmd()
		}
		return m, nil
	}

	return m, nil
}

func (m *model) updateKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.quitting {
		return m, tea.Quit
	}

	key := msg.String()
	if key == "ctrl+c" {
		if m.pendingApproval != nil && m.pendingApproval.replyCh != nil {
			select {
			case m.pendingApproval.replyCh <- ApprovalReject:
			default:
			}
			m.pendingApproval = nil
		}
		m.quitting = true
		return m, tea.Quit
	}

	// 1. Inline diff approval modal takes precedence.
	if m.pendingApproval != nil {
		switch key {
		case "a":
			m.resolveApproval(ApprovalApprove)
			return m, nil
		case "r", "esc":
			m.resolveApproval(ApprovalReject)
			return m, nil
		case "A":
			if m.alwaysAllowTools == nil {
				m.alwaysAllowTools = make(map[string]bool)
			}
			m.alwaysAllowTools[m.pendingApproval.toolName] = true
			m.resolveApproval(ApprovalAlwaysAllow)
			return m, nil
		default:
			return m, nil
		}
	}

	// 2. Interactive rewind checkpoint picker overlay.
	if m.showRewindPicker {
		switch key {
		case "esc", "q":
			m.showRewindPicker = false
			return m, nil
		case "up", "k":
			if m.rewindCursor > 0 {
				m.rewindCursor--
			}
			return m, nil
		case "down", "j":
			if m.rewindCursor < len(m.checkpoints)-1 {
				m.rewindCursor++
			}
			return m, nil
		case "enter":
			return m.applySelectedCheckpoint()
		default:
			return m, nil
		}
	}

	// 3. Interactive theme picker overlay.
	if m.showThemePicker {
		options := themePickerOptions()
		switch key {
		case "esc", "q":
			m.showThemePicker = false
			return m, nil
		case "up", "k":
			if m.themeCursor > 0 {
				m.themeCursor--
			}
			return m, nil
		case "down", "j":
			if m.themeCursor < len(options)-1 {
				m.themeCursor++
			}
			return m, nil
		case "enter":
			if m.themeCursor >= 0 && m.themeCursor < len(options) {
				chosen := options[m.themeCursor]
				ui.SetTheme(chosen)
				m.activeTheme = ui.ActiveThemeName()
				m.messages = append(m.messages, uiRenderable{
					role:    engine.RoleSystem,
					name:    "Theme",
					content: fmt.Sprintf("Theme switched to %q (active: %s).", chosen, m.activeTheme),
					ts:      time.Now(),
				})
				m.syncScroll()
			}
			m.showThemePicker = false
			return m, nil
		default:
			return m, nil
		}
	}

	// 4. Help overlay.
	if m.showHelp {
		switch key {
		case "esc", "q", "enter", "f1":
			m.showHelp = false
			return m, nil
		default:
			return m, nil
		}
	}

	// 5. Viewport scrolling & global toggles work in both idle and active states.
	switch key {
	case "pgup":
		m.scrollBy(-maxInt(1, m.viewportHeight()-1))
		return m, nil
	case "pgdown":
		m.scrollBy(maxInt(1, m.viewportHeight()-1))
		return m, nil
	case "ctrl+u":
		m.scrollBy(-maxInt(1, m.viewportHeight()/2))
		return m, nil
	case "ctrl+d":
		m.scrollBy(maxInt(1, m.viewportHeight()/2))
		return m, nil
	case "tab":
		m.toolsExpanded = !m.toolsExpanded
		m.syncScroll()
		return m, nil
	case "f1":
		m.showHelp = true
		return m, nil
	}

	// Only accept prompt editing when idle.
	if m.status != engine.StateIdle {
		return m, nil
	}

	// Multi-line newline insertion (Alt+Enter or Ctrl+J).
	if key == "alt+enter" || key == "ctrl+j" || (msg.Type == tea.KeyEnter && msg.Alt) || msg.Type == tea.KeyCtrlJ {
		m.insertAtCursor("\n")
		return m, nil
	}

	switch key {
	case "enter":
		text := strings.TrimSpace(m.input)
		m.input = ""
		m.cursor = 0
		m.historyIdx = -1
		m.historyDraft = ""
		if text == "" {
			return m, nil
		}
		if len(m.inputHistory) == 0 || m.inputHistory[len(m.inputHistory)-1] != text {
			m.inputHistory = append(m.inputHistory, text)
		}
		return m.handleInput(text)

	case "up":
		if len(m.inputHistory) == 0 {
			return m, nil
		}
		if m.historyIdx == -1 {
			m.historyDraft = m.input
			m.historyIdx = len(m.inputHistory) - 1
		} else if m.historyIdx > 0 {
			m.historyIdx--
		}
		m.input = m.inputHistory[m.historyIdx]
		m.cursor = len([]rune(m.input))
		return m, nil

	case "down":
		if m.historyIdx == -1 {
			return m, nil
		}
		if m.historyIdx < len(m.inputHistory)-1 {
			m.historyIdx++
			m.input = m.inputHistory[m.historyIdx]
		} else {
			m.historyIdx = -1
			m.input = m.historyDraft
		}
		m.cursor = len([]rune(m.input))
		return m, nil

	case "backspace":
		m.backspaceAtCursor()
		return m, nil

	case "delete":
		m.deleteAtCursor()
		return m, nil

	case "left":
		if m.cursor > 0 {
			m.cursor--
		}
		return m, nil

	case "right":
		if m.cursor < len([]rune(m.input)) {
			m.cursor++
		}
		return m, nil

	case "home", "ctrl+a":
		m.cursor = 0
		return m, nil

	case "end", "ctrl+e":
		m.cursor = len([]rune(m.input))
		return m, nil

	default:
		if len(msg.Runes) > 0 {
			m.insertAtCursor(string(msg.Runes))
		}
		return m, nil
	}
}

func (m *model) resolveApproval(decision ApprovalDecision) {
	if m.pendingApproval == nil {
		return
	}
	req := m.pendingApproval
	m.lastApprovalDecision = decision
	if req.replyCh != nil {
		select {
		case req.replyCh <- decision:
		default:
		}
	}

	var msgText string
	switch decision {
	case ApprovalApprove:
		msgText = fmt.Sprintf("Approved %s edit on %s.", req.toolName, req.filePath)
	case ApprovalReject:
		msgText = fmt.Sprintf("Rejected %s edit on %s.", req.toolName, req.filePath)
	case ApprovalAlwaysAllow:
		msgText = fmt.Sprintf("Approved and enabled session auto-allow for %s.", req.toolName)
	}
	m.messages = append(m.messages, uiRenderable{
		role:    engine.RoleSystem,
		name:    "Approval",
		content: msgText,
		ts:      time.Now(),
	})
	m.pendingApproval = nil
	m.syncScroll()
}

func (m *model) insertAtCursor(s string) {
	runes := []rune(m.input)
	if m.cursor < 0 {
		m.cursor = 0
	}
	if m.cursor > len(runes) {
		m.cursor = len(runes)
	}
	ins := []rune(s)
	next := make([]rune, 0, len(runes)+len(ins))
	next = append(next, runes[:m.cursor]...)
	next = append(next, ins...)
	next = append(next, runes[m.cursor:]...)
	m.input = string(next)
	m.cursor += len(ins)
}

func (m *model) backspaceAtCursor() {
	runes := []rune(m.input)
	if m.cursor <= 0 || len(runes) == 0 {
		m.cursor = 0
		return
	}
	if m.cursor > len(runes) {
		m.cursor = len(runes)
	}
	next := append(runes[:m.cursor-1], runes[m.cursor:]...)
	m.input = string(next)
	m.cursor--
}

func (m *model) deleteAtCursor() {
	runes := []rune(m.input)
	if m.cursor < 0 {
		m.cursor = 0
	}
	if m.cursor >= len(runes) {
		return
	}
	next := append(runes[:m.cursor], runes[m.cursor+1:]...)
	m.input = string(next)
}

func themePickerOptions() []string {
	opts := []string{"auto"}
	opts = append(opts, ui.ThemeNames()...)
	return opts
}

func isValidThemeName(name string) bool {
	n := strings.ToLower(strings.TrimSpace(name))
	if n == "auto" {
		return true
	}
	for _, t := range ui.ThemeNames() {
		if t == n {
			return true
		}
	}
	return false
}

func (m *model) handleInput(text string) (tea.Model, tea.Cmd) {
	trimmed := strings.TrimSpace(text)
	lower := strings.ToLower(trimmed)
	switch {
	case lower == "/exit" || lower == "/quit":
		m.quitting = true
		return m, tea.Quit

	case lower == "/clear":
		if m.sess != nil {
			m.sess.ClearHistory()
			m.turnStartMsgIdx = 0
		}
		m.messages = make([]uiRenderable, 0)
		m.streamingText = ""
		m.err = nil
		m.scrollOffset = 0
		m.autoScroll = true
		return m, nil

	case lower == "/help":
		m.showHelp = true
		return m, nil

	case lower == "/theme" || strings.HasPrefix(lower, "/theme "):
		arg := ""
		if len(trimmed) > 6 {
			arg = strings.TrimSpace(trimmed[6:])
		}
		if arg == "" {
			m.showThemePicker = true
			m.themeCursor = 0
			options := themePickerOptions()
			for i, opt := range options {
				if opt == m.activeTheme {
					m.themeCursor = i
					break
				}
			}
			return m, nil
		}
		if !isValidThemeName(arg) {
			m.err = fmt.Errorf("unknown theme %q (valid: auto, %s)", arg, strings.Join(ui.ThemeNames(), ", "))
			return m, nil
		}
		ui.SetTheme(arg)
		m.activeTheme = ui.ActiveThemeName()
		m.err = nil
		m.messages = append(m.messages, uiRenderable{
			role:    engine.RoleSystem,
			name:    "Theme",
			content: fmt.Sprintf("Active theme set to %s.", m.activeTheme),
			ts:      time.Now(),
		})
		m.syncScroll()
		return m, nil

	case lower == "/swarm" || strings.HasPrefix(lower, "/swarm "):
		arg := ""
		if len(trimmed) > 6 {
			arg = strings.ToLower(strings.TrimSpace(trimmed[6:]))
		}
		switch arg {
		case "on", "open", "show":
			m.showSwarm = true
		case "off", "close", "hide":
			m.showSwarm = false
		default:
			m.showSwarm = !m.showSwarm
		}
		m.syncScroll()
		if m.showSwarm && hasRunningSubagents() {
			return m, tickCmd()
		}
		return m, nil

	case lower == "/rewind":
		m.checkpoints = loadCheckpoints(m.sess)
		m.rewindCursor = 0
		m.showRewindPicker = true
		m.err = nil
		return m, nil

	case strings.HasPrefix(lower, "/session"):
		parts := strings.Fields(text)
		if len(parts) > 1 && m.sess != nil {
			name := parts[1]
			m.sess.Config.SessionName = name
			if err := m.sess.LoadSavedHistory(); err != nil {
				m.err = err
				return m, nil
			}
			m.messages = make([]uiRenderable, 0)
			sessMsgs := m.sess.GetMessages()
			for _, msg := range sessMsgs {
				m.messages = append(m.messages, messageToRenderable(msg))
			}
			m.turnStartMsgIdx = len(sessMsgs)
			m.syncScroll()
		}
		return m, nil

	case strings.HasPrefix(lower, "/undo"):
		if m.sess == nil {
			m.err = fmt.Errorf("no active session")
			return m, nil
		}
		if rewound, err := m.sess.Rewind(); err != nil {
			m.err = err
		} else {
			m.err = nil
			m.messages = append(m.messages, uiRenderable{
				role:    engine.RoleSystem,
				name:    "System Directive",
				content: fmt.Sprintf("Rewound to checkpoint %s (turn %d)", rewound.ID, rewound.Turn),
				ts:      time.Now(),
			})
			m.syncScroll()
		}
		return m, refreshGitStatusCmd(m.cwd())

	case strings.HasPrefix(lower, "/usage"):
		usedTokens, maxTokens, pct := m.contextUsage()
		m.messages = append(m.messages, uiRenderable{
			role:    engine.RoleSystem,
			name:    "Usage",
			content: fmt.Sprintf("Context: ~%d / %d tokens (%d%%) • Messages: %d", usedTokens, maxTokens, pct, len(m.messages)),
			ts:      time.Now(),
		})
		m.syncScroll()
		return m, nil

	default:
		if m.sess == nil {
			m.err = fmt.Errorf("session is not initialized")
			return m, nil
		}
		m.err = nil
		m.messages = append(m.messages, uiRenderable{
			role:    engine.RoleUser,
			content: text,
			ts:      time.Now(),
		})
		m.turnStartMsgIdx = len(m.sess.GetMessages())
		m.streamingText = ""
		m.status = engine.StateThinking
		m.autoScroll = true
		m.syncScroll()
		go m.executeTurn(text)
		return m, tickCmd()
	}
}

func (m *model) executeTurn(input string) {
	if m.sess == nil {
		sendTUIMsg(turnResultMsg{err: fmt.Errorf("session is nil")})
		return
	}

	promptInput := input
	memStore := m.sess.Config.MemoryStore
	if memStore != nil {
		ctxResults, memErr := memStore.RetrieveRelevant(input, 3)
		if memErr == nil && len(ctxResults) > 0 {
			var ctxBuf strings.Builder
			ctxBuf.WriteString("[Relevant context from past sessions]\n")
			for _, r := range ctxResults {
				ctxBuf.WriteString(fmt.Sprintf("  - %s\n", r.Text))
			}
			ctxBuf.WriteString("\n")
			promptInput = ctxBuf.String() + input
		}
	}

	sw := newTUIStreamWriter(getTUIProgram())
	resp, err := m.sess.ExecuteTurn(m.ctx, promptInput, nil, sw)
	m.sess.SetState(engine.StateIdle)

	if memStore != nil {
		lastResp := resp
		if lastResp == "" {
			msgs := m.sess.GetMessages()
			if len(msgs) > 0 {
				lastMsg := msgs[len(msgs)-1]
				if lastMsg.Role == engine.RoleAssistant {
					lastResp = lastMsg.Content
				}
			}
		}
		_ = memStore.StoreTurn(m.sess.Config.SessionName, promptInput, lastResp)
	}

	sendTUIMsg(turnResultMsg{response: resp, err: err})
}

func (m *model) applySelectedCheckpoint() (tea.Model, tea.Cmd) {
	m.showRewindPicker = false
	if len(m.checkpoints) == 0 || m.rewindCursor < 0 || m.rewindCursor >= len(m.checkpoints) {
		return m, nil
	}
	if m.sess == nil || m.sess.CheckpointMgr == nil {
		m.err = fmt.Errorf("checkpoint manager not initialized")
		return m, nil
	}

	selected := m.checkpoints[m.rewindCursor]
	var restoreErr error
	if latest, ok := m.sess.CheckpointMgr.Latest(); ok && (latest.CommitSHA == selected.commitSHA || latest.ID == selected.id) {
		_, restoreErr = m.sess.Rewind()
	} else {
		restoreErr = m.sess.CheckpointMgr.RestoreCheckpoint(selected.commitSHA)
	}

	if restoreErr != nil {
		m.err = restoreErr
		return m, nil
	}

	m.err = nil
	m.messages = append(m.messages, uiRenderable{
		role:    engine.RoleSystem,
		name:    "Rewind",
		content: fmt.Sprintf("Restored checkpoint %s (turn %d, %s).", selected.id, selected.turn, selected.shortSHA),
		ts:      time.Now(),
	})
	m.syncScroll()
	return m, refreshGitStatusCmd(m.cwd())
}

func loadCheckpoints(sess *engine.Session) []checkpointItem {
	var items []checkpointItem
	seenSHA := make(map[string]bool)

	if sess != nil && sess.Config.Cwd != "" {
		cmd := exec.Command("git", "for-each-ref", "--sort=-creatordate",
			"--format=%(refname:short)|%(objectname:short)|%(objectname)|%(creatordate:relative)|%(subject)",
			"refs/microgravity/checkpoints/")
		cmd.Dir = sess.Config.Cwd
		if out, err := cmd.Output(); err == nil {
			for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
				line = strings.TrimSpace(line)
				if line == "" {
					continue
				}
				parts := strings.SplitN(line, "|", 5)
				if len(parts) < 3 {
					continue
				}
				refShort, shortSHA, fullSHA := parts[0], parts[1], parts[2]
				when := ""
				subject := ""
				if len(parts) > 3 {
					when = parts[3]
				}
				if len(parts) > 4 {
					subject = parts[4]
				}
				turn := 0
				refParts := strings.Split(refShort, "/")
				if len(refParts) > 0 {
					if tNum, err := strconv.Atoi(refParts[len(refParts)-1]); err == nil {
						turn = tNum
					}
				}
				diffStat := gitDiffShortStat(sess.Config.Cwd, fullSHA)
				items = append(items, checkpointItem{
					id:        fmt.Sprintf("cp-%d", turn),
					turn:      turn,
					commitSHA: fullSHA,
					shortSHA:  shortSHA,
					message:   subject,
					when:      when,
					diffStat:  diffStat,
				})
				seenSHA[fullSHA] = true
				if len(items) >= 15 {
					break
				}
			}
		}
	}

	if sess != nil && sess.CheckpointMgr != nil {
		if latest, ok := sess.CheckpointMgr.Latest(); ok && !seenSHA[latest.CommitSHA] {
			shortSHA := latest.CommitSHA
			if len(shortSHA) > 8 {
				shortSHA = shortSHA[:8]
			}
			diffStat := ""
			if sess.Config.Cwd != "" {
				diffStat = gitDiffShortStat(sess.Config.Cwd, latest.CommitSHA)
			}
			items = append([]checkpointItem{{
				id:        latest.ID,
				turn:      latest.Turn,
				commitSHA: latest.CommitSHA,
				shortSHA:  shortSHA,
				message:   latest.Message,
				when:      latest.Timestamp.Format("15:04:05"),
				diffStat:  diffStat,
			}}, items...)
		}
	}

	return items
}

func gitDiffShortStat(repoDir, commitSHA string) string {
	if repoDir == "" || commitSHA == "" {
		return ""
	}
	cmd := exec.Command("git", "diff", "--shortstat", commitSHA)
	cmd.Dir = repoDir
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// View renders the full TUI.
func (m *model) View() string {
	if m.quitting {
		return ""
	}

	totalW := m.effectiveWidth()
	totalH := m.effectiveHeight()

	header := ui.PrintHeader(" 🌌 MICROGRAVITY CHAT ")
	inputView := m.renderInput(totalW)

	var errView string
	if m.err != nil {
		errView = ui.PrintError(m.err)
	}

	chatW := totalW
	splitHorizontal := m.showSwarm && totalW >= 60
	swarmW := 0
	if splitHorizontal {
		swarmW = m.swarmPaneWidth(totalW)
		chatW = totalW - swarmW - 1
	}

	vpH := m.viewportHeightFor(totalW, totalH)
	vpContent, scrollEnd, totalLines := m.renderViewport(chatW, vpH)
	statusBar := m.renderStatusBar(totalW, scrollEnd, totalLines)

	var body string
	switch {
	case m.pendingApproval != nil:
		req := m.pendingApproval
		body = ui.RenderDiffApprovalModal(req.toolName, req.filePath, req.oldContent, req.newContent, totalW)
	case m.showRewindPicker:
		body = m.renderRewindPicker(totalW, vpH)
	case m.showThemePicker:
		body = m.renderThemePicker(totalW, vpH)
	case m.showHelp:
		body = m.renderHelp(totalW)
	case m.showSwarm:
		if splitHorizontal {
			chatPane := lipgloss.NewStyle().Width(chatW).Height(vpH).Render(vpContent)
			swarmPane := m.renderSwarmPane(swarmW, vpH)
			body = lipgloss.JoinHorizontal(lipgloss.Top, chatPane, " ", swarmPane)
		} else {
			swarmH := maxInt(5, vpH/2)
			chatPane := lipgloss.NewStyle().Width(totalW).Height(maxInt(4, vpH-swarmH)).Render(vpContent)
			swarmPane := m.renderSwarmPane(totalW, swarmH)
			body = lipgloss.JoinVertical(lipgloss.Left, chatPane, swarmPane)
		}
	default:
		body = vpContent
	}

	var sb strings.Builder
	sb.WriteString(header + "\n")
	sb.WriteString(body + "\n")
	sb.WriteString(statusBar + "\n")
	sb.WriteString(inputView)
	if errView != "" {
		sb.WriteString("\n" + errView)
	}

	return sb.String()
}

func (m *model) effectiveWidth() int {
	if m.width <= 0 {
		return 100
	}
	return m.width
}

func (m *model) effectiveHeight() int {
	if m.height <= 0 {
		return 30
	}
	return m.height
}

func (m *model) swarmPaneWidth(totalW int) int {
	w := totalW / 3
	if w < 28 {
		w = 28
	}
	if w > 46 {
		w = 46
	}
	return w
}

func (m *model) viewportWidth() int {
	totalW := m.effectiveWidth()
	if m.showSwarm && totalW >= 60 {
		return totalW - m.swarmPaneWidth(totalW) - 1
	}
	return totalW
}

func (m *model) viewportHeight() int {
	return m.viewportHeightFor(m.effectiveWidth(), m.effectiveHeight())
}

func (m *model) viewportHeightFor(totalW, totalH int) int {
	inputLines := strings.Count(m.input, "\n") + 1
	if inputLines > 6 {
		inputLines = 6
	}
	statusLines := strings.Count(m.renderStatusBar(totalW, 0, 0), "\n") + 1
	// Header (~3 lines) + status bar + input + spacers
	reserved := 3 + statusLines + inputLines + 2
	if m.err != nil {
		reserved += 3
	}
	vpH := totalH - reserved
	if vpH < 4 {
		vpH = 4
	}
	return vpH
}

func (m *model) renderedViewportLines(chatW int) []string {
	rendered := m.renderMessages(chatW)
	if rendered == "" {
		return nil
	}
	return strings.Split(rendered, "\n")
}

func (m *model) syncScroll() {
	chatW := m.viewportWidth()
	lines := m.renderedViewportLines(chatW)
	vpH := m.viewportHeight()
	maxOff := maxInt(0, len(lines)-vpH)
	if m.autoScroll {
		m.scrollOffset = maxOff
	} else {
		if m.scrollOffset > maxOff {
			m.scrollOffset = maxOff
		}
		if m.scrollOffset < 0 {
			m.scrollOffset = 0
		}
	}
}

func (m *model) scrollBy(delta int) {
	chatW := m.viewportWidth()
	lines := m.renderedViewportLines(chatW)
	vpH := m.viewportHeight()
	maxOff := maxInt(0, len(lines)-vpH)

	next := m.scrollOffset + delta
	if next <= 0 {
		m.scrollOffset = 0
		m.autoScroll = maxOff == 0
		return
	}
	if next >= maxOff {
		m.scrollOffset = maxOff
		m.autoScroll = true
		return
	}
	m.scrollOffset = next
	m.autoScroll = false
}

func (m *model) renderViewport(chatW, vpH int) (string, int, int) {
	lines := m.renderedViewportLines(chatW)
	total := len(lines)
	if total == 0 {
		return "", 0, 0
	}

	maxOff := maxInt(0, total-vpH)
	off := m.scrollOffset
	if m.autoScroll {
		off = maxOff
	}
	if off > maxOff {
		off = maxOff
	}
	if off < 0 {
		off = 0
	}

	end := off + vpH
	if end > total {
		end = total
	}
	return strings.Join(lines[off:end], "\n"), end, total
}

func (m *model) renderMessages(width int) string {
	if len(m.messages) == 0 && m.streamingText == "" && m.status == engine.StateIdle {
		return lipStyle(ui.RoleTextMuted).Italic(true).Render("No messages yet. Type your message below and press Enter (Alt+Enter for newline).")
	}

	var sb strings.Builder
	for _, r := range m.messages {
		sb.WriteString(m.renderMessage(r, width))
		sb.WriteString("\n")
	}

	if m.streamingText != "" {
		wrapW := maxInt(20, width-4)
		sb.WriteString(lipStyle(ui.RoleBrand).Bold(true).Render("🤖 Agent (streaming)") + "\n")
		sb.WriteString(wrapText(m.streamingText, wrapW) + lipStyle(ui.RolePrimary).Bold(true).Render("▌"))
	} else if m.status != engine.StateIdle {
		frame := tuiSpinnerFrames[m.spinnerFrame%len(tuiSpinnerFrames)]
		sb.WriteString(lipStyle(ui.RolePrimary).Bold(true).Render(fmt.Sprintf("%s %s...", frame, m.status)))
	}

	return strings.TrimRight(sb.String(), "\n")
}

func (m *model) renderMessage(r uiRenderable, width int) string {
	wrapW := maxInt(20, width-4)
	switch r.role {
	case engine.RoleUser:
		return lipStyle(ui.RolePrimary).Bold(true).Render("👤 You") + "\n" +
			wrapText(r.content, wrapW) + "\n"

	case engine.RoleAssistant:
		var sb strings.Builder
		sb.WriteString(lipStyle(ui.RoleBrand).Bold(true).Render("🤖 Agent") + "\n")
		rendered := ""
		if m.renderer != nil {
			if out, err := m.renderer.Render(r.content); err == nil {
				rendered = strings.TrimRight(out, "\n")
			}
		}
		if rendered == "" {
			rendered = wrapText(r.content, wrapW)
		} else if !strings.Contains(rendered, "\x1b[") {
			rendered = wrapText(rendered, wrapW)
		}
		sb.WriteString(rendered + "\n")
		return sb.String()

	case engine.RoleTool:
		toolName := r.name
		if toolName == "" {
			toolName = "tool"
		}
		return ui.RenderCollapsibleToolCard(toolName, r.args, r.duration, r.success, r.content, m.toolsExpanded) + "\n"

	case engine.RoleSystem:
		name := r.name
		if name == "" {
			name = "System"
		}
		return lipStyle(ui.RoleAccent).Bold(true).Render("ℹ "+name) + "\n" +
			wrapText(r.content, wrapW) + "\n"

	default:
		return wrapText(r.content, wrapW)
	}
}

func (m *model) renderInput(width int) string {
	prompt := lipStyle(ui.RolePrimary).Bold(true).Render("You: ")
	runes := []rune(m.input)
	cur := m.cursor
	if cur < 0 {
		cur = 0
	}
	if cur > len(runes) {
		cur = len(runes)
	}

	textStyle := lipStyle(ui.RoleText)
	cursorStyle := lipgloss.NewStyle().Reverse(true)

	before := string(runes[:cur])
	var cursorCell string
	var after string
	if cur < len(runes) {
		if runes[cur] == '\n' {
			cursorCell = cursorStyle.Render(" ") + "\n"
		} else {
			cursorCell = cursorStyle.Render(string(runes[cur]))
		}
		after = string(runes[cur+1:])
	} else {
		cursorCell = cursorStyle.Render(" ")
	}

	renderedInput := textStyle.Render(before) + cursorCell + textStyle.Render(after)
	if !strings.Contains(renderedInput, "\n") {
		return prompt + renderedInput
	}

	lines := strings.Split(renderedInput, "\n")
	var sb strings.Builder
	sb.WriteString(prompt + lines[0])
	indent := strings.Repeat(" ", 5)
	for _, line := range lines[1:] {
		sb.WriteString("\n" + lipStyle(ui.RoleTextMuted).Render(indent) + line)
	}
	return sb.String()
}

func (m *model) contextUsage() (usedTokens, maxTokens, pct int) {
	var chars int
	for _, msg := range m.messages {
		chars += len(msg.content)
	}
	chars += len(m.streamingText)
	if chars > 0 {
		usedTokens = (chars + 3) / 4
	}

	modelName := m.activeModelName()
	maxTokens = pruner.MaxTokensForModel(modelName)
	if maxTokens <= 0 {
		maxTokens = 16000
	}
	pct = (usedTokens * 100) / maxTokens
	if pct > 100 {
		pct = 100
	}
	if pct < 0 {
		pct = 0
	}
	return usedTokens, maxTokens, pct
}

func renderContextUsageBar(pct, barWidth int) string {
	if barWidth <= 0 {
		barWidth = 6
	}
	filled := (pct * barWidth) / 100
	if pct > 0 && filled == 0 {
		filled = 1
	}
	if filled > barWidth {
		filled = barWidth
	}
	bar := strings.Repeat("█", filled) + strings.Repeat("░", barWidth-filled)

	role := ui.RoleSuccess
	if pct >= 85 {
		role = ui.RoleError
	} else if pct >= 60 {
		role = ui.RoleHighlight
	}
	return lipStyle(role).Bold(true).Render(fmt.Sprintf("%s %d%%", bar, pct))
}

func (m *model) activeModelName() string {
	if m.sess == nil {
		return "local"
	}
	if m.sess.Config.ActiveModel != "" {
		return m.sess.Config.ActiveModel
	}
	if m.sess.Config.CloudModelName != "" {
		return m.sess.Config.CloudModelName
	}
	if m.sess.Config.LocalModelName != "" {
		return m.sess.Config.LocalModelName
	}
	return "default"
}

func (m *model) isLocalRoute() bool {
	if m.sess == nil {
		return false
	}
	switch strings.ToLower(m.sess.Config.ExplicitProvider) {
	case "ollama":
		return true
	case "cloud":
		return false
	}
	active := strings.ToLower(m.activeModelName())
	local := strings.ToLower(m.sess.Config.LocalModelName)
	if local != "" && active == local {
		return true
	}
	return strings.Contains(active, ":") ||
		strings.HasPrefix(active, "gemma") ||
		strings.HasPrefix(active, "llama") ||
		strings.HasPrefix(active, "qwen") ||
		strings.HasPrefix(active, "orbit-")
}

func (m *model) renderStatusBar(width, scrollEnd, totalLines int) string {
	stateText := string(m.status)
	stateStyle := lipStyle(ui.RoleTextMuted)
	switch m.status {
	case engine.StateThinking:
		frame := tuiSpinnerFrames[m.spinnerFrame%len(tuiSpinnerFrames)]
		stateText = frame + " " + stateText
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

	routeLabel := "[CLOUD]"
	routeBg := ui.RoleColor(ui.RoleAccent)
	if m.isLocalRoute() {
		routeLabel = "[LOCAL]"
		routeBg = ui.RoleColor(ui.RoleSuccess)
	}
	routePill := lipgloss.NewStyle().
		Foreground(lipgloss.Color("15")).
		Background(routeBg).
		Bold(true).
		Padding(0, 1).
		Render(routeLabel)

	_, _, pct := m.contextUsage()
	ctxBar := renderContextUsageBar(pct, 6)

	themeName := m.activeTheme
	if themeName == "" {
		themeName = ui.ActiveThemeName()
	}
	themeBadge := lipStyle(ui.RoleBrand).Render("theme:" + themeName)

	gitBadge := lipStyle(ui.RoleTextMuted).Render("git:none")
	if m.gitBranch != "" {
		dirtyMark := ""
		gitRole := ui.RoleSuccess
		if m.gitDirty {
			dirtyMark = "*"
			gitRole = ui.RoleHighlight
		}
		gitBadge = lipStyle(gitRole).Render("🌿 " + m.gitBranch + dirtyMark)
	}

	scrollBadge := lipStyle(ui.RolePrimary).Render(fmt.Sprintf("[Line %d/%d]", scrollEnd, totalLines))

	toolsMode := "collapsed"
	if m.toolsExpanded {
		toolsMode = "expanded"
	}

	leftParts := []string{
		routePill + " " + lipStyle(ui.RoleText).Bold(true).Render(m.activeModelName()),
		"Ctx:" + ctxBar,
		themeBadge,
		gitBadge,
	}
	rightParts := []string{
		"State:" + stateStyle.Render(stateText),
		fmt.Sprintf("Turns:%d", len(m.messages)),
		"Tools:" + lipStyle(ui.RoleTextMuted).Render(toolsMode),
		scrollBadge,
	}

	sep := lipStyle(ui.RoleSeparator).Render(" │ ")
	leftRow := strings.Join(leftParts, sep)
	rightRow := strings.Join(rightParts, sep)
	singleLine := leftRow + sep + rightRow
	if width > 0 && (width < 115 || lipgloss.Width(singleLine) > width) {
		return leftRow + "\n" + rightRow
	}
	return singleLine
}

func (m *model) renderSwarmPane(width, height int) string {
	workers := subagent.GetManager().List()
	sort.Slice(workers, func(i, j int) bool {
		if !workers[i].StartTime.Equal(workers[j].StartTime) {
			return workers[i].StartTime.Before(workers[j].StartTime)
		}
		return workers[i].ID < workers[j].ID
	})

	innerW := maxInt(20, width-4)
	title := lipStyle(ui.RoleBrand).Bold(true).Render(fmt.Sprintf("🐝 SWARM MONITOR (%d)", len(workers)))

	var lines []string
	lines = append(lines, title)
	lines = append(lines, lipStyle(ui.RoleSeparator).Render(strings.Repeat("─", innerW)))

	if len(workers) == 0 {
		lines = append(lines, lipStyle(ui.RoleTextMuted).Italic(true).Render("No active subagents."))
		lines = append(lines, lipStyle(ui.RoleTextMuted).Render("Run /swarm again to close."))
	} else {
		for _, w := range workers {
			roleName := w.Role
			if roleName == "" {
				roleName = w.ID
			}
			stateStr := string(w.State)
			if stateStr == "" {
				stateStr = w.Status
			}
			badgeColor := ui.RoleColor(ui.RoleSystem)
			switch w.State {
			case subagent.StateRunning:
				badgeColor = ui.RoleColor(ui.RolePrimary)
			case subagent.StateCompleted:
				badgeColor = ui.RoleColor(ui.RoleSuccess)
			case subagent.StateErrored:
				badgeColor = ui.RoleColor(ui.RoleError)
			case subagent.StateKilled:
				badgeColor = ui.RoleColor(ui.RoleHighlight)
			}
			badge := lipgloss.NewStyle().
				Foreground(lipgloss.Color("15")).
				Background(badgeColor).
				Bold(true).
				Padding(0, 1).
				Render(strings.ToUpper(stateStr))

			elapsed := time.Since(w.StartTime).Truncate(time.Second)
			if !w.EndTime.IsZero() {
				elapsed = w.EndTime.Sub(w.StartTime).Truncate(time.Millisecond)
			}

			summary := w.Result
			if summary == "" {
				summary = w.StateDetail
			}
			if summary == "" {
				summary = w.Prompt
			}
			summary = strings.ReplaceAll(strings.TrimSpace(summary), "\n", " ")
			summary = truncateRunes(summary, innerW)

			lines = append(lines, fmt.Sprintf("%s %s %s",
				lipStyle(ui.RoleText).Bold(true).Render(truncateRunes(roleName, maxInt(8, innerW-16))),
				badge,
				lipStyle(ui.RoleTextMuted).Render(elapsed.String()),
			))
			if summary != "" {
				lines = append(lines, "  "+lipStyle(ui.RoleTextMuted).Render(summary))
			}
		}
	}

	maxLines := maxInt(3, height-2)
	if len(lines) > maxLines {
		lines = append(lines[:maxLines-1], lipStyle(ui.RoleTextMuted).Render(fmt.Sprintf("... (%d more)", len(workers))))
	}

	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(ui.RoleColor(ui.RoleAccent)).
		Padding(0, 1).
		Width(width - 2).
		Height(height - 2)

	return box.Render(strings.Join(lines, "\n"))
}

func (m *model) renderRewindPicker(width, height int) string {
	boxW := minInt(maxInt(48, width-4), 100)
	innerW := boxW - 6

	var sb strings.Builder
	sb.WriteString(lipStyle(ui.RoleHighlight).Bold(true).Render("⏪ REWIND CHECKPOINT PICKER") + "\n")
	sb.WriteString(lipStyle(ui.RoleSeparator).Render(strings.Repeat("─", maxInt(20, innerW))) + "\n")

	if len(m.checkpoints) == 0 {
		sb.WriteString(lipStyle(ui.RoleTextMuted).Italic(true).Render("No git checkpoints found for this session.") + "\n")
	} else {
		for i, cp := range m.checkpoints {
			prefix := "  "
			lineStyle := lipStyle(ui.RoleText)
			if i == m.rewindCursor {
				prefix = "▶ "
				lineStyle = lipStyle(ui.RolePrimary).Bold(true)
			}
			info := fmt.Sprintf("%s (Turn %d, %s)", cp.id, cp.turn, cp.shortSHA)
			if cp.when != "" {
				info += " • " + cp.when
			}
			if cp.message != "" {
				info += " — " + cp.message
			}
			if cp.diffStat != "" {
				info += " [" + cp.diffStat + "]"
			}
			sb.WriteString(lineStyle.Render(prefix+truncateRunes(info, innerW)) + "\n")
		}
	}

	sb.WriteString(lipStyle(ui.RoleSeparator).Render(strings.Repeat("─", maxInt(20, innerW))) + "\n")
	sb.WriteString(lipStyle(ui.RoleTextMuted).Render("[↑/↓] select  [Enter] restore checkpoint  [Esc] cancel"))

	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(ui.RoleColor(ui.RoleAccent)).
		Padding(1, 2).
		Width(boxW).
		Render(sb.String())
}

func (m *model) renderThemePicker(width, height int) string {
	boxW := minInt(maxInt(44, width-4), 80)
	innerW := boxW - 6
	options := themePickerOptions()

	var sb strings.Builder
	sb.WriteString(lipStyle(ui.RoleBrand).Bold(true).Render("🎨 SELECT THEME") + "\n")
	sb.WriteString(lipStyle(ui.RoleSeparator).Render(strings.Repeat("─", maxInt(20, innerW))) + "\n")

	for i, opt := range options {
		prefix := "  "
		style := lipStyle(ui.RoleText)
		if i == m.themeCursor {
			prefix = "▶ "
			style = lipStyle(ui.RolePrimary).Bold(true)
		}
		activeTag := ""
		if opt == m.activeTheme {
			activeTag = " (active)"
		}
		sb.WriteString(style.Render(prefix+opt+activeTag) + "\n")
	}

	sb.WriteString(lipStyle(ui.RoleSeparator).Render(strings.Repeat("─", maxInt(20, innerW))) + "\n")
	sb.WriteString(lipStyle(ui.RoleTextMuted).Render("[↑/↓] select  [Enter] apply theme  [Esc] cancel"))

	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(ui.RoleColor(ui.RoleBrand)).
		Padding(1, 2).
		Width(boxW).
		Render(sb.String())
}

func (m *model) renderHelp(width int) string {
	help := `Available commands & shortcuts:
  /theme [name|auto] - Switch UI theme (dark, light, tokyo-night, catppuccin, high-contrast, auto)
  /rewind            - Interactive checkpoint picker overlay
  /undo              - Rollback last turn's file modifications
  /swarm [on|off]    - Toggle split-pane subagent/swarm monitor panel
  /session <name>    - Switch/resume a session
  /usage             - Show session context & token budget
  /clear             - Clear history
  /help / F1         - Show/hide this help
  /exit / /quit      - Leave chat

Navigation & input:
  PgUp/PgDn, Ctrl+U/Ctrl+D, Mouse Wheel - Scroll message viewport
  Tab                                   - Expand/collapse tool call cards
  Alt+Enter / Ctrl+J                    - Insert newline in input
  Up / Down                             - Recall previous prompts
  Ctrl+C                                - Quit`

	boxW := minInt(maxInt(52, width-4), 96)
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(ui.RoleColor(ui.RolePrimary)).
		Padding(1, 2).
		Width(boxW).
		Foreground(ui.RoleColor(ui.RoleTextMuted)).
		Render(help)
}

func (m *model) cwd() string {
	if m.sess != nil && m.sess.Config.Cwd != "" {
		return m.sess.Config.Cwd
	}
	wd, _ := os.Getwd()
	return wd
}

func refreshGitStatusCmd(repoDir string) tea.Cmd {
	return func() tea.Msg {
		if repoDir == "" {
			return gitStatusMsg{err: fmt.Errorf("no repo dir")}
		}
		branchCmd := exec.Command("git", "rev-parse", "--abbrev-ref", "HEAD")
		branchCmd.Dir = repoDir
		var out bytes.Buffer
		branchCmd.Stdout = &out
		if err := branchCmd.Run(); err != nil {
			return gitStatusMsg{err: err}
		}
		branch := strings.TrimSpace(out.String())

		dirtyCmd := exec.Command("git", "status", "--porcelain")
		dirtyCmd.Dir = repoDir
		var dirtyOut bytes.Buffer
		dirtyCmd.Stdout = &dirtyOut
		dirty := false
		if err := dirtyCmd.Run(); err == nil {
			dirty = strings.TrimSpace(dirtyOut.String()) != ""
		}
		return gitStatusMsg{branch: branch, dirty: dirty}
	}
}

func hasRunningSubagents() bool {
	for _, s := range subagent.GetManager().List() {
		if s.State == subagent.StateRunning || s.Status == "RUNNING" {
			return true
		}
	}
	return false
}

// tickCmd drives spinner animation and live swarm updates.
func tickCmd() tea.Cmd {
	return tea.Tick(120*time.Millisecond, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

func drainAsyncEvents(sess *engine.Session) tea.Cmd {
	if sess == nil || sess.AsyncEvents == nil {
		return nil
	}
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
		runes := []rune(para)
		for len(runes) > width {
			lines = append(lines, string(runes[:width]))
			runes = runes[width:]
		}
		lines = append(lines, string(runes))
	}
	return strings.Join(lines, "\n")
}

func truncateRunes(s string, maxLen int) string {
	if maxLen <= 3 {
		return s
	}
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	return string(runes[:maxLen-3]) + "..."
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// RunTUI starts the bubbletea program with the given engine session.
func RunTUI(sess *engine.Session, ctx context.Context) error {
	m := newModel(sess, ctx)
	p := tea.NewProgram(
		m,
		tea.WithAltScreen(),
		tea.WithMouseCellMotion(),
	)
	setTUIProgram(p)
	defer setTUIProgram(nil)

	// Handle SIGINT/SIGTERM for graceful shutdown without leaking goroutines.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	done := make(chan struct{})
	defer func() {
		signal.Stop(sigCh)
		close(done)
	}()
	go func() {
		select {
		case <-sigCh:
			p.Quit()
		case <-done:
		}
	}()

	finalModel, err := p.Run()
	if err != nil {
		return fmt.Errorf("TUI error: %w", err)
	}
	if fm, ok := finalModel.(*model); ok && fm.quitting {
		ui.PrintGoodbye()
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

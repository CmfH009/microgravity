package cmd

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/CmfH009/microgravity/internal/config"
	"github.com/CmfH009/microgravity/internal/engine"
	"github.com/CmfH009/microgravity/internal/subagent"
	"github.com/CmfH009/microgravity/internal/ui"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestTUISession(t *testing.T, cwd string) *engine.Session {
	t.Helper()
	if cwd == "" {
		cwd = t.TempDir()
	}
	sess, err := engine.NewSession(engine.SessionConfig{
		SessionName:      "tui-test",
		ActiveModel:      "gemma:2b",
		LocalModelName:   "gemma:2b",
		CloudModelName:   "gemini-3.6-flash",
		ExplicitProvider: "ollama",
		Cwd:              cwd,
		CoreMemory:       engine.DefaultCoreMemory,
	})
	require.NoError(t, err)
	t.Cleanup(func() {
		sess.Close()
	})
	return sess
}

func typeTextIntoModel(m *model, s string) {
	for _, r := range s {
		m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
}

func TestTUI_ViewportScrolling(t *testing.T) {
	sess := newTestTUISession(t, "")
	m := newModel(sess, context.Background())
	m.Update(tea.WindowSizeMsg{Width: 90, Height: 18})

	for i := 1; i <= 30; i++ {
		m.messages = append(m.messages, uiRenderable{
			role:    engine.RoleUser,
			content: fmt.Sprintf("Message line %02d for viewport scroll check", i),
			ts:      time.Now(),
		})
	}
	m.syncScroll()

	assert.True(t, m.autoScroll)
	bottomOffset := m.scrollOffset
	require.Greater(t, bottomOffset, 0)

	viewAtBottom := m.View()
	assert.Contains(t, viewAtBottom, "[Line ")
	totalLines := len(m.renderedViewportLines(m.viewportWidth()))
	assert.Contains(t, viewAtBottom, fmt.Sprintf("[Line %d/%d]", totalLines, totalLines))

	// Scroll up with PgUp
	m.Update(tea.KeyMsg{Type: tea.KeyPgUp})
	assert.False(t, m.autoScroll)
	assert.Less(t, m.scrollOffset, bottomOffset)

	afterPgUp := m.scrollOffset
	// Scroll up further with Ctrl+U
	m.Update(tea.KeyMsg{Type: tea.KeyCtrlU})
	assert.Less(t, m.scrollOffset, afterPgUp)

	// Mouse wheel down moves down; PgDn reaches bottom and re-enables autoScroll
	afterCtrlU := m.scrollOffset
	m.Update(tea.MouseMsg{Button: tea.MouseButtonWheelDown})
	assert.Greater(t, m.scrollOffset, afterCtrlU)

	m.Update(tea.MouseMsg{Button: tea.MouseButtonWheelUp})
	assert.Equal(t, afterCtrlU, m.scrollOffset)

	for i := 0; i < 20 && !m.autoScroll; i++ {
		m.Update(tea.KeyMsg{Type: tea.KeyPgDown})
	}
	assert.True(t, m.autoScroll)
	assert.Equal(t, bottomOffset, m.scrollOffset)
}

func TestTUI_LiveStreamingChunks(t *testing.T) {
	// Ensure tuiStreamWriter is safe and buffers when tuiProgram == nil.
	setTUIProgram(nil)
	sw := newTUIStreamWriter(nil)
	n, err := sw.Write([]byte("Hello "))
	require.NoError(t, err)
	assert.Equal(t, 6, n)
	_, err = sw.Write([]byte("streaming!"))
	require.NoError(t, err)
	assert.Equal(t, "Hello streaming!", sw.String())

	// Model receives live streamChunkMsg updates while StateThinking.
	sess := newTestTUISession(t, "")
	m := newModel(sess, context.Background())
	m.Update(tea.WindowSizeMsg{Width: 90, Height: 24})
	m.Update(stateMsg{state: engine.StateThinking})

	m.Update(streamChunkMsg{chunk: "First chunk "})
	m.Update(streamChunkMsg{chunk: "second chunk"})

	viewStreaming := m.View()
	assert.Contains(t, viewStreaming, "streaming")
	assert.Contains(t, viewStreaming, "First chunk second chunk")

	m.Update(turnResultMsg{response: "First chunk second chunk"})
	assert.Equal(t, engine.StateIdle, m.status)
	assert.Empty(t, m.streamingText)
	require.Len(t, m.messages, 1)
	assert.Equal(t, engine.RoleAssistant, m.messages[0].role)
	assert.Equal(t, "First chunk second chunk", m.messages[0].content)
}

func TestTUI_ExecuteTurnWithStreamWriterLoopback(t *testing.T) {
	// Local loopback httptest mock for Ollama streaming (zero external API calls).
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/chat" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/x-ndjson")
		flusher, _ := w.(http.Flusher)
		chunks := []string{
			`{"message":{"role":"assistant","content":"Local "},"done":false}`,
			`{"message":{"role":"assistant","content":"stream ok"},"done":true}`,
		}
		for _, c := range chunks {
			fmt.Fprintln(w, c)
			if flusher != nil {
				flusher.Flush()
			}
		}
	}))
	defer srv.Close()

	sess, err := engine.NewSession(engine.SessionConfig{
		SessionName:      "stream-loopback-test",
		ActiveModel:      "gemma:2b",
		LocalModelName:   "gemma:2b",
		ExplicitProvider: "ollama",
		OllamaBaseURL:    srv.URL,
		Cwd:              t.TempDir(),
		CoreMemory:       engine.DefaultCoreMemory,
	})
	require.NoError(t, err)
	defer sess.Close()

	sw := newTUIStreamWriter(nil)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	resp, err := sess.ExecuteTurn(ctx, "hello", nil, sw)
	require.NoError(t, err)
	assert.Equal(t, "Local stream ok", resp)
	assert.Equal(t, "Local stream ok", strings.TrimSpace(sw.String()))
}

func TestTUI_MarkdownAndCollapsibleToolCards(t *testing.T) {
	sess := newTestTUISession(t, "")
	m := newModel(sess, context.Background())
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})

	m.messages = append(m.messages,
		uiRenderable{
			role:    engine.RoleAssistant,
			content: "> [!NOTE]\n> Rendered alert block.\n",
		},
		uiRenderable{
			role:     engine.RoleTool,
			name:     "read_file",
			args:     map[string]interface{}{"path": "cmd/tui.go"},
			duration: 12 * time.Millisecond,
			success:  true,
			content:  "line A\nline B\nline C",
		},
	)
	m.syncScroll()

	// Collapsed by default
	assert.False(t, m.toolsExpanded)
	collapsedView := m.View()
	assert.Contains(t, collapsedView, "NOTE")
	assert.Contains(t, collapsedView, "Rendered alert block.")
	assert.Contains(t, collapsedView, "read_file")
	assert.NotContains(t, collapsedView, "Output Preview:")

	// Tab expands tool cards
	m.Update(tea.KeyMsg{Type: tea.KeyTab})
	assert.True(t, m.toolsExpanded)
	expandedView := m.View()
	assert.Contains(t, expandedView, "Output Preview:")
	assert.Contains(t, expandedView, "line A")

	// Tab again collapses back
	m.Update(tea.KeyMsg{Type: tea.KeyTab})
	assert.False(t, m.toolsExpanded)
	assert.NotContains(t, m.View(), "Output Preview:")

	// Graceful fallback when renderer is nil
	m.renderer = nil
	fallbackView := m.renderMessage(uiRenderable{
		role:    engine.RoleAssistant,
		content: "Plain fallback response",
	}, 80)
	assert.Contains(t, fallbackView, "Plain fallback response")
}

func TestTUI_CursorMultiLineAndHistoryRecall(t *testing.T) {
	sess := newTestTUISession(t, "")
	m := newModel(sess, context.Background())
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 24})

	// Typing 'h' must insert 'h' (not hijack help)
	typeTextIntoModel(m, "hello")
	assert.False(t, m.showHelp)
	assert.Equal(t, "hello", m.input)
	assert.Equal(t, 5, m.cursor)

	// Move left and insert middle text
	m.Update(tea.KeyMsg{Type: tea.KeyLeft})
	m.Update(tea.KeyMsg{Type: tea.KeyLeft})
	assert.Equal(t, 3, m.cursor)
	typeTextIntoModel(m, "XX")
	assert.Equal(t, "helXXlo", m.input)
	assert.Equal(t, 5, m.cursor)

	// Alt+Enter and Ctrl+J insert newlines without submitting
	m.Update(tea.KeyMsg{Type: tea.KeyEnd})
	m.Update(tea.KeyMsg{Type: tea.KeyEnter, Alt: true})
	typeTextIntoModel(m, "line2")
	m.Update(tea.KeyMsg{Type: tea.KeyCtrlJ})
	typeTextIntoModel(m, "line3")
	assert.Equal(t, "helXXlo\nline2\nline3", m.input)
	assert.Contains(t, m.renderInput(100), "line2")
	assert.Contains(t, m.renderInput(100), "line3")

	// Submit via /clear or /theme (local commands so no network call)
	m.input = "/theme dark"
	m.cursor = len([]rune(m.input))
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	assert.Empty(t, m.input)
	assert.Equal(t, 0, m.cursor)

	m.input = "/usage"
	m.cursor = len([]rune(m.input))
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})

	// Up/Down history recall preserves draft
	typeTextIntoModel(m, "draft")
	m.Update(tea.KeyMsg{Type: tea.KeyUp})
	assert.Equal(t, "/usage", m.input)
	m.Update(tea.KeyMsg{Type: tea.KeyUp})
	assert.Equal(t, "/theme dark", m.input)
	m.Update(tea.KeyMsg{Type: tea.KeyDown})
	assert.Equal(t, "/usage", m.input)
	m.Update(tea.KeyMsg{Type: tea.KeyDown})
	assert.Equal(t, "draft", m.input)
}

func TestTUI_UpgradedStatusBar(t *testing.T) {
	defer ui.SetTheme("dark")
	ui.SetTheme("tokyo-night")

	sess := newTestTUISession(t, "")
	m := newModel(sess, context.Background())
	m.Update(tea.WindowSizeMsg{Width: 140, Height: 24})
	m.messages = append(m.messages, uiRenderable{
		role:    engine.RoleUser,
		content: strings.Repeat("token ", 500),
	})
	m.Update(gitStatusMsg{branch: "swarm/tui-ux-v2", dirty: true})

	view := m.View()
	assert.Contains(t, view, "[LOCAL]")
	assert.Contains(t, view, "gemma:2b")
	assert.Contains(t, view, "theme:tokyo-night")
	assert.Contains(t, view, "swarm/tui-ux-v2*")
	assert.Contains(t, view, "State:idle")
	assert.Contains(t, view, "Turns:1")
	assert.Contains(t, view, "%")
	assert.Contains(t, view, "[Line ")

	// Switch session config to cloud route and verify [CLOUD] pill even on 80-col width
	sess.Config.ExplicitProvider = "cloud"
	sess.Config.ActiveModel = "gemini-3.6-flash"
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	cloudView := m.View()
	assert.Contains(t, cloudView, "[CLOUD]")
	assert.Contains(t, cloudView, "gemini-3.6-flash")
	assert.Contains(t, cloudView, "[Line ")
}

func TestTUI_ThemeSwitcherAndPicker(t *testing.T) {
	isolateCmdTestEnv(t)
	t.Cleanup(func() {
		lipgloss.SetHasDarkBackground(true)
		ui.SetTheme("dark")
	})

	sess := newTestTUISession(t, "")
	m := newModel(sess, context.Background())
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 24})

	// Direct /theme <name> updates UI AND persists via config.SetTheme
	m.handleInput("/theme catppuccin")
	assert.Equal(t, "catppuccin", m.activeTheme)
	assert.Equal(t, "catppuccin", ui.ActiveThemeName())
	assert.Equal(t, "catppuccin", config.GetTheme())

	lipgloss.SetHasDarkBackground(false)
	m.handleInput("/theme auto")
	assert.Equal(t, "light", m.activeTheme)
	assert.Equal(t, "light", ui.ActiveThemeName())
	assert.Equal(t, "auto", config.GetTheme())

	// Unknown theme reports error without changing active or persisted theme
	m.handleInput("/theme nonexistent")
	require.Error(t, m.err)
	assert.Equal(t, "light", m.activeTheme)
	assert.Equal(t, "auto", config.GetTheme())

	// /theme without arguments opens interactive theme picker
	m.handleInput("/theme")
	assert.True(t, m.showThemePicker)
	pickerView := m.View()
	assert.Contains(t, pickerView, "SELECT THEME")
	assert.Contains(t, pickerView, "tokyo-night")

	// Navigate to tokyo-night (options: auto=0, dark=1, light=2, tokyo-night=3)
	m.themeCursor = 3
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	assert.False(t, m.showThemePicker)
	assert.Equal(t, "tokyo-night", ui.ActiveThemeName())
	assert.Equal(t, "tokyo-night", m.activeTheme)
	assert.Equal(t, "tokyo-night", config.GetTheme())
}

func TestTUI_SharedSlashCommands(t *testing.T) {
	isolateCmdTestEnv(t)
	sess := newTestTUISession(t, "")
	m := newModel(sess, context.Background())
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 24})

	// /local on & /local status now work in TUI via internal/slash
	m.handleInput("/local on")
	require.NoError(t, m.err)
	assert.Equal(t, "local-only", string(sess.GetRoutingMode()))
	assert.Equal(t, "local-only", config.GetRoutingMode())

	m.handleInput("/local status")
	require.NoError(t, m.err)
	require.NotEmpty(t, m.messages)
	assert.Contains(t, m.messages[len(m.messages)-1].content, "local-only")

	// /caveman ultra & /caveman off now work in TUI
	m.handleInput("/caveman ultra")
	require.NoError(t, m.err)
	assert.Equal(t, "ultra", sess.Config.CavemanLevel)
	m.handleInput("/caveman off")
	assert.Equal(t, "off", sess.Config.CavemanLevel)

	// /diff, /mcp, /rules, /learn all handled locally in TUI
	m.handleInput("/mcp")
	require.NoError(t, m.err)
	assert.Contains(t, m.messages[len(m.messages)-1].content, "MCP")

	m.handleInput("/learn Keep tests fast and isolated")
	require.NoError(t, m.err)
	rulesBytes, err := os.ReadFile(filepath.Join(sess.Config.Cwd, ".microgravity", "rules.md"))
	require.NoError(t, err)
	assert.Contains(t, string(rulesBytes), "Keep tests fast and isolated")
}

func TestTUI_RewindCheckpointPicker(t *testing.T) {
	repoDir := t.TempDir()
	require.NoError(t, exec.Command("git", "-C", repoDir, "init").Run())
	require.NoError(t, exec.Command("git", "-C", repoDir, "config", "user.name", "TUI Test").Run())
	require.NoError(t, exec.Command("git", "-C", repoDir, "config", "user.email", "tui@test.local").Run())
	require.NoError(t, exec.Command("git", "-C", repoDir, "config", "commit.gpgsign", "false").Run())
	require.NoError(t, exec.Command("git", "-C", repoDir, "config", "core.autocrlf", "false").Run())

	targetFile := filepath.Join(repoDir, "sample.txt")
	require.NoError(t, os.WriteFile(targetFile, []byte("original state\n"), 0644))
	require.NoError(t, exec.Command("git", "-C", repoDir, "add", ".").Run())
	require.NoError(t, exec.Command("git", "-C", repoDir, "commit", "-m", "initial commit").Run())

	sess := newTestTUISession(t, repoDir)
	cp, err := sess.CreateCheckpoint(1, "Pre-edit snapshot")
	require.NoError(t, err)
	require.NotNil(t, cp)

	// Modify file after checkpoint
	require.NoError(t, os.WriteFile(targetFile, []byte("modified state\n"), 0644))

	m := newModel(sess, context.Background())
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 24})

	// Open /rewind overlay and verify checkpoint listed
	m.handleInput("/rewind")
	assert.True(t, m.showRewindPicker)
	require.NotEmpty(t, m.checkpoints)

	overlayView := m.View()
	assert.Contains(t, overlayView, "REWIND CHECKPOINT PICKER")
	assert.Contains(t, overlayView, "cp-1")

	// Esc cancels without modifying disk
	m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	assert.False(t, m.showRewindPicker)
	content, err := os.ReadFile(targetFile)
	require.NoError(t, err)
	assert.Equal(t, "modified state\n", string(content))

	// Re-open /rewind and press Enter to restore
	m.handleInput("/rewind")
	assert.True(t, m.showRewindPicker)
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	assert.False(t, m.showRewindPicker)
	assert.NoError(t, m.err)

	restored, err := os.ReadFile(targetFile)
	require.NoError(t, err)
	assert.Equal(t, "original state\n", strings.ReplaceAll(string(restored), "\r\n", "\n"))
}

func TestTUI_SwarmPaneToggle(t *testing.T) {
	mgr := subagent.GetManager()
	gate := make(chan struct{})
	workerID := fmt.Sprintf("tui-swarm-worker-%d", time.Now().UnixNano())
	_, err := mgr.Spawn(context.Background(), workerID, "UI Reviewer", subagent.ProfileResearcher, "Inspect TUI layout", "", subagent.WorkspaceInherit, func(ctx context.Context, sub *subagent.SubagentInfo) (string, error) {
		select {
		case <-gate:
			return "Layout verified", nil
		case <-ctx.Done():
			return "", ctx.Err()
		}
	})
	require.NoError(t, err)
	t.Cleanup(func() {
		select {
		case <-gate:
		default:
			close(gate)
		}
		waitCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = mgr.Wait(waitCtx)
	})

	sess := newTestTUISession(t, "")
	m := newModel(sess, context.Background())
	m.Update(tea.WindowSizeMsg{Width: 110, Height: 24})

	assert.False(t, m.showSwarm)
	m.handleInput("/swarm")
	assert.True(t, m.showSwarm)

	swarmView := m.View()
	assert.Contains(t, swarmView, "SWARM MONITOR")
	assert.Contains(t, swarmView, "UI Reviewer")
	assert.Contains(t, swarmView, "RUNNING")

	close(gate)
	waitCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	require.NoError(t, mgr.Wait(waitCtx))

	completedView := m.View()
	assert.Contains(t, completedView, "COMPLETED")
	assert.Contains(t, completedView, "Layout verified")

	// Toggle off via /swarm
	m.handleInput("/swarm")
	assert.False(t, m.showSwarm)
	assert.NotContains(t, m.View(), "SWARM MONITOR")
}

func TestTUI_ApprovalModalTransitions(t *testing.T) {
	sess := newTestTUISession(t, "")
	m := newModel(sess, context.Background())
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 26})

	// 1. Approve ('a')
	ch1 := make(chan ApprovalDecision, 1)
	m.Update(approvalRequestMsg{
		toolName:   "edit_file",
		filePath:   "app.go",
		oldContent: "line1\nold\nline3\n",
		newContent: "line1\nnew\nline3\n",
		replyCh:    ch1,
	})
	require.NotNil(t, m.pendingApproval)
	modalView := m.View()
	assert.Contains(t, modalView, "TOOL APPROVAL REQUIRED")
	assert.Contains(t, modalView, "edit_file")
	assert.Contains(t, modalView, "-old")
	assert.Contains(t, modalView, "+new")
	assert.Contains(t, modalView, "[a]pprove  [r]eject  [A]lways allow in session")

	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	assert.Nil(t, m.pendingApproval)
	assert.Equal(t, ApprovalApprove, <-ch1)

	// 2. Reject ('r')
	ch2 := make(chan ApprovalDecision, 1)
	m.Update(approvalRequestMsg{
		toolName:   "write_file",
		filePath:   "bad.go",
		oldContent: "",
		newContent: "danger\n",
		replyCh:    ch2,
	})
	require.NotNil(t, m.pendingApproval)
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	assert.Nil(t, m.pendingApproval)
	assert.Equal(t, ApprovalReject, <-ch2)

	// 3. Always allow in session ('A')
	ch3 := make(chan ApprovalDecision, 1)
	m.Update(approvalRequestMsg{
		toolName:   "edit_file",
		filePath:   "next.go",
		oldContent: "a\n",
		newContent: "b\n",
		replyCh:    ch3,
	})
	require.NotNil(t, m.pendingApproval)
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'A'}})
	assert.Nil(t, m.pendingApproval)
	assert.Equal(t, ApprovalAlwaysAllow, <-ch3)
	assert.True(t, m.alwaysAllowTools["edit_file"])

	// 4. Subsequent edit_file request auto-approves immediately without modal
	ch4 := make(chan ApprovalDecision, 1)
	m.Update(approvalRequestMsg{
		toolName:   "edit_file",
		filePath:   "auto.go",
		oldContent: "x\n",
		newContent: "y\n",
		replyCh:    ch4,
	})
	assert.Nil(t, m.pendingApproval)
	assert.Equal(t, ApprovalAlwaysAllow, <-ch4)
}

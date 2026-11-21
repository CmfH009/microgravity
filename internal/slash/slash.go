package slash

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/CmfH009/microgravity/internal/audit"
	"github.com/CmfH009/microgravity/internal/changelog"
	"github.com/CmfH009/microgravity/internal/config"
	"github.com/CmfH009/microgravity/internal/git"
	"github.com/CmfH009/microgravity/internal/pruner"
	"github.com/CmfH009/microgravity/internal/rules"
	"github.com/CmfH009/microgravity/internal/skills"
	"github.com/CmfH009/microgravity/internal/subagent"
	"github.com/CmfH009/microgravity/internal/ui"
)

// Action identifies a shell/UI transition requested by a slash command.
type Action string

const (
	ActionNone             Action = ""
	ActionExit             Action = "exit"
	ActionClear            Action = "clear"
	ActionHelp             Action = "help"
	ActionOpenThemePicker  Action = "open_theme_picker"
	ActionOpenRewindPicker Action = "open_rewind_picker"
	ActionToggleSwarm      Action = "toggle_swarm"
	ActionReloadSession    Action = "reload_session"
	ActionPickSession      Action = "pick_session"
	ActionPickModel        Action = "pick_model"
	ActionPickSkill        Action = "pick_skill"
	ActionPaste            Action = "paste"
	ActionRefreshGit       Action = "refresh_git"
)

// Routing mode constants shared across interactive shells.
const (
	RoutingModeLocalOnly = "local-only"
	RoutingModeHybrid    = "hybrid"
	RoutingModeCloudOnly = "cloud-only"
)

// Result is the structured outcome of evaluating raw input through Handler.
type Result struct {
	Handled   bool
	Title     string
	Output    string
	Prompt    string
	Action    Action
	ActionArg string
	Err       error
}

// CommandSpec describes a slash command for help and autocomplete.
type CommandSpec struct {
	Name        string
	Description string
}

// Catalog returns the canonical list of interactive slash commands shared by
// the CLI prompt and Bubble Tea TUI.
func Catalog() []CommandSpec {
	return []CommandSpec{
		{Name: "/exit", Description: "Exit the chat session"},
		{Name: "/quit", Description: "Exit the chat session"},
		{Name: "/help", Description: "Show available commands"},
		{Name: "/clear", Description: "Clear context history"},
		{Name: "/skills", Description: "List loaded skills"},
		{Name: "/subagents", Description: "List, spawn, or kill subagents (/subagents list|spawn|kill-all)"},
		{Name: "/schedule", Description: "Schedule timers or cron jobs (/schedule timer 10s 'task')"},
		{Name: "/audit", Description: "Run project setup & directive health audit"},
		{Name: "/plan", Description: "Generate structured execution plan before proceeding"},
		{Name: "/goal", Description: "Run autonomous goal mode until fully complete"},
		{Name: "/learn", Description: "Persist custom directive to workspace rules"},
		{Name: "/paste", Description: "Open multi-line paste buffer mode"},
		{Name: "/grill-me", Description: "Interactive interview mode to clarify requirements"},
		{Name: "/model", Description: "Inspect catalog or switch active model (e.g. /model gemini-3.1-pro)"},
		{Name: "/diff", Description: "View git diff or working tree changes"},
		{Name: "/rewind", Description: "Rollback or open interactive checkpoint picker"},
		{Name: "/undo", Description: "Instant rollback of last turn's file modifications"},
		{Name: "/usage", Description: "View token usage, budget, and model quotas"},
		{Name: "/quota", Description: "View token usage, budget, and model quotas"},
		{Name: "/credits", Description: "Inspect remaining G1 credits & tier status"},
		{Name: "/changelog", Description: "View release notes & changelog in TUI"},
		{Name: "/statusline", Description: "Render current status bar line"},
		{Name: "/caveman", Description: "Toggle caveman mode (/caveman lite|full|ultra|off)"},
		{Name: "/dashboard", Description: "Launch Web & Terminal Telemetry Control Center"},
		{Name: "/fix", Description: "Run autonomous self-healing test repair & doubt loop"},
		{Name: "/ci", Description: "Generate production CI/CD GitHub Actions workflows"},
		{Name: "/mcp", Description: "Inspect and manage MCP servers (/mcp list|add|remove)"},
		{Name: "/rules", Description: "Inspect active project and global rules"},
		{Name: "/local", Description: "Toggle or set routing mode (/local [on|off|hybrid|local-only|cloud-only|status])"},
		{Name: "/theme", Description: "Switch UI theme (/theme [dark|light|auto|tokyo-night|catppuccin|high-contrast])"},
		{Name: "/swarm", Description: "Toggle split-pane subagent/swarm monitor panel"},
		{Name: "/session", Description: "Switch or resume a named session (/session <name>)"},
		{Name: "/sessions", Description: "Open session selector or switch session"},
		{Name: "/memory", Description: "Search persistent conversation memory (/memory <query>)"},
		{Name: "/teamwork-preview", Description: "Multi-agent swarm collaboration preview"},
		{Name: "/teamwork", Description: "Multi-agent swarm collaboration preview"},
	}
}

// SupportedThemes returns all valid theme arguments including "auto" followed
// by the concrete UI theme names.
func SupportedThemes() []string {
	opts := []string{"auto"}
	opts = append(opts, ui.ThemeNames()...)
	return opts
}

// IsSupportedTheme reports whether name is "auto" or a registered UI theme.
func IsSupportedTheme(name string) bool {
	n := strings.ToLower(strings.TrimSpace(name))
	if n == "" {
		return false
	}
	for _, t := range SupportedThemes() {
		if t == n {
			return true
		}
	}
	return false
}

// SessionInfo is a point-in-time snapshot of interactive session state.
type SessionInfo struct {
	SessionName      string
	Cwd              string
	ActiveModel      string
	CloudModelName   string
	LocalModelName   string
	LocalFastModel   string
	LocalHeavyModel  string
	OpenAIBaseURL    string
	RoutingMode      string
	ConversationText string
	MessageChars     int
	MessageCount     int
	MCPCount         int
	LoadedSkills     []*skills.Skill
}

// Session is the seam connecting slash commands to an engine session (or an
// in-memory test fake).
type Session interface {
	Snapshot() SessionInfo
	ClearHistory()
	SetRoutingMode(mode string)
	SetModel(modelID, provider string)
	SetMode(mode string)
	SetCavemanLevel(level string)
	SwitchSession(name string) error
	Rewind() (*git.Checkpoint, error)
	RestoreCheckpoint(commitSHA string) error
	LatestCheckpoint() (*git.Checkpoint, bool)
	ScheduleTimer(dur time.Duration, msg string) (int, error)
}

// ModelOption describes an entry from the model catalog.
type ModelOption struct {
	ID       string
	Name     string
	Provider string // "cloud", "ollama", "gguf", etc.
	Badge    string
}

// ConfigStore is the persistence and UI theme seam used by Handler.
type ConfigStore interface {
	GetRoutingMode() string
	SetRoutingMode(mode string) error
	GetTheme() string
	SetTheme(theme string) error
	ApplyUITheme(theme string) string
	ActiveUITheme() string
	GetActiveModel() string
	SetActiveModel(modelID string) error
	GetMCPServers() []string
}

type defaultConfigStore struct{}

func (defaultConfigStore) GetRoutingMode() string           { return config.GetRoutingMode() }
func (defaultConfigStore) SetRoutingMode(mode string) error { return config.SetRoutingMode(mode) }
func (defaultConfigStore) GetTheme() string                 { return config.GetTheme() }
func (defaultConfigStore) SetTheme(theme string) error      { return config.SetTheme(theme) }
func (defaultConfigStore) ApplyUITheme(theme string) string {
	ui.SetTheme(theme)
	return ui.ActiveThemeName()
}
func (defaultConfigStore) ActiveUITheme() string             { return ui.ActiveThemeName() }
func (defaultConfigStore) GetActiveModel() string            { return config.GetActiveModel() }
func (defaultConfigStore) SetActiveModel(modelID string) error { return config.SetActiveModel(modelID) }
func (defaultConfigStore) GetMCPServers() []string           { return config.GetMCPServers() }

// DefaultConfigStore returns the production ConfigStore backed by
// internal/config and internal/ui.
func DefaultConfigStore() ConfigStore {
	return defaultConfigStore{}
}

// SubagentController is the seam for listing, killing, and spawning subagents.
type SubagentController interface {
	List() []*subagent.SubagentInfo
	KillAll() int
	Spawn(ctx context.Context, promptTask string) (*subagent.SubagentInfo, error)
}

type defaultSubagentController struct{}

func (defaultSubagentController) List() []*subagent.SubagentInfo {
	return subagent.GetManager().List()
}

func (defaultSubagentController) KillAll() int {
	return subagent.GetManager().KillAll()
}

func (defaultSubagentController) Spawn(ctx context.Context, promptTask string) (*subagent.SubagentInfo, error) {
	id := fmt.Sprintf("sub-%d", time.Now().UnixNano()%1000000)
	return subagent.GetManager().Spawn(ctx, id, "Subagent Worker", subagent.ProfileGeneral, promptTask, "", subagent.WorkspaceInherit, nil)
}

// Options configures optional seams and shell capabilities on Handler.
type Options struct {
	Config            ConfigStore
	Subagents         SubagentController
	ScanModels        func(ctx context.Context) ([]ModelOption, error)
	EnsureLocalModel  func(ctx context.Context, modelID string) (downloading bool)
	GitDiff           func(cwd string) (string, error)
	AuditWorkspace    func(cwd string) (string, error)
	LoadRules         func(cwd string) (string, error)
	RenderChangelog   func() string
	DefaultLocalModel string
	SupportsOverlays  bool
}

// Handler executes interactive slash commands against a Session seam and
// injectable ConfigStore/service seams.
type Handler struct {
	Sess Session
	Opts Options
}

// NewHandler creates a Handler for sess with production defaults filled in for
// any unset Option fields.
func NewHandler(sess Session, opts Options) *Handler {
	if opts.Config == nil {
		opts.Config = DefaultConfigStore()
	}
	if opts.Subagents == nil {
		opts.Subagents = defaultSubagentController{}
	}
	if opts.GitDiff == nil {
		opts.GitDiff = func(cwd string) (string, error) {
			return git.GitDiff(cwd, false)
		}
	}
	if opts.AuditWorkspace == nil {
		opts.AuditWorkspace = func(cwd string) (string, error) {
			pa, err := audit.AuditWorkspace(cwd)
			if err != nil {
				return "", err
			}
			return pa.RenderScorecard(), nil
		}
	}
	if opts.LoadRules == nil {
		opts.LoadRules = func(cwd string) (string, error) {
			r, err := rules.LoadDefaultRules(cwd)
			if r == nil {
				return "", err
			}
			return r.FormatForSystemPrompt(), err
		}
	}
	if opts.RenderChangelog == nil {
		opts.RenderChangelog = changelog.Render
	}
	return &Handler{
		Sess: sess,
		Opts: opts,
	}
}

// Execute parses and executes a slash command if raw begins with "/".
// When raw is not a recognized slash command, Execute returns Result{Handled: false}.
func (h *Handler) Execute(ctx context.Context, raw string) Result {
	if ctx == nil {
		ctx = context.Background()
	}
	trimmed := strings.TrimSpace(raw)
	if !strings.HasPrefix(trimmed, "/") {
		return Result{Handled: false}
	}

	cmdWord, arg := splitCommand(trimmed)
	switch cmdWord {
	case "/exit", "/quit":
		return Result{
			Handled: true,
			Action:  ActionExit,
		}

	case "/clear":
		if h.Sess != nil {
			h.Sess.ClearHistory()
		}
		return Result{
			Handled: true,
			Title:   "Clear",
			Output:  "🧹 Chat history cleared.",
			Action:  ActionClear,
		}

	case "/help":
		return Result{
			Handled: true,
			Title:   "Help",
			Output:  HelpText(),
			Action:  ActionHelp,
		}

	case "/theme":
		return h.handleTheme(arg)

	case "/local":
		return h.handleLocal(arg)

	case "/model":
		return h.handleModel(ctx, arg)

	case "/caveman":
		return h.handleCaveman(arg)

	case "/plan":
		if arg == "" {
			return Result{
				Handled: true,
				Title:   "Plan",
				Output:  "Usage: /plan <task description>",
			}
		}
		if h.Sess != nil {
			h.Sess.SetMode("plan")
		}
		return Result{
			Handled: true,
			Title:   "Plan",
			Prompt:  "[PLAN MODE ACTIVATED] Formulate a clear, step-by-step Markdown execution plan with task checkboxes before taking implementation action: " + arg,
		}

	case "/grill-me":
		if arg == "" {
			return Result{
				Handled: true,
				Title:   "Interview",
				Output:  "Usage: /grill-me <project or feature idea>",
			}
		}
		return Result{
			Handled: true,
			Title:   "Interview",
			Prompt:  "[INTERVIEW MODE ACTIVATED] Conduct a 1-question-at-a-time interactive interview to clarify requirements and constraints: " + arg,
		}

	case "/goal":
		if arg == "" {
			return Result{
				Handled: true,
				Title:   "Goal",
				Output:  "Usage: /goal <long-running goal>",
			}
		}
		return Result{
			Handled: true,
			Title:   "Goal",
			Prompt:  "[GOAL MODE ACTIVATED] Relentlessly pursue this objective, self-correcting and executing until verified complete: " + arg,
		}

	case "/diff":
		diff, err := h.Opts.GitDiff(h.cwd())
		if err != nil || strings.TrimSpace(diff) == "" {
			return Result{
				Handled: true,
				Title:   "Git Diff",
				Output:  "No git changes or working tree is clean.",
			}
		}
		return Result{
			Handled: true,
			Title:   "Working Copy Git Diff",
			Output:  diff,
		}

	case "/undo":
		return h.handleImmediateRewind()

	case "/rewind":
		if h.Opts.SupportsOverlays && strings.TrimSpace(arg) == "" {
			return Result{
				Handled: true,
				Title:   "Rewind",
				Action:  ActionOpenRewindPicker,
			}
		}
		return h.handleImmediateRewind()

	case "/swarm":
		mode := "toggle"
		switch strings.ToLower(strings.TrimSpace(arg)) {
		case "on", "open", "show":
			mode = "on"
		case "off", "close", "hide":
			mode = "off"
		}
		return Result{
			Handled:   true,
			Title:     "Swarm",
			Output:    "Swarm monitor toggled. Launch parallel tasks with: mg swarm <task 1> <task 2>",
			Action:    ActionToggleSwarm,
			ActionArg: mode,
		}

	case "/teamwork", "/teamwork-preview":
		return Result{
			Handled: true,
			Title:   "Autonomous Agent Teamwork & Swarm Mode",
			Output: strings.Join([]string{
				" • Swarm Workers: Researcher, Code Reviewer, Security Auditor, Test Engineer",
				" • Workspaces: Branch / Share / Inherit",
				" • Launch parallel tasks with: mg swarm <task 1> <task 2>",
			}, "\n"),
		}

	case "/session", "/sessions":
		return h.handleSession(cmdWord, arg)

	case "/usage", "/quota", "/credits":
		return h.handleUsage()

	case "/audit":
		scorecard, err := h.Opts.AuditWorkspace(h.cwd())
		if err != nil {
			return Result{Handled: true, Title: "Audit", Err: err}
		}
		return Result{
			Handled: true,
			Title:   "Audit",
			Output:  scorecard,
		}

	case "/subagents":
		return h.handleSubagents(ctx, arg)

	case "/schedule":
		return h.handleSchedule(arg)

	case "/paste":
		return Result{
			Handled: true,
			Title:   "Paste",
			Output:  "Multi-line input: use Alt+Enter or Ctrl+J in TUI, or paste buffer in CLI.",
			Action:  ActionPaste,
		}

	case "/skills":
		return h.handleSkills(arg)

	case "/memory":
		return h.handleMemory(arg)

	case "/learn":
		return h.handleLearn(arg)

	case "/changelog":
		return Result{
			Handled: true,
			Title:   "Changelog",
			Output:  h.Opts.RenderChangelog(),
		}

	case "/statusline":
		info := h.snapshot()
		statusLine := ui.RenderStatusLine(h.activeModelName(info), len(info.LoadedSkills), info.MCPCount, h.cwd())
		return Result{
			Handled: true,
			Title:   "Statusline",
			Output:  "Statusline: " + statusLine,
		}

	case "/rules":
		formatted, _ := h.Opts.LoadRules(h.cwd())
		return Result{
			Handled: true,
			Title:   "Active Antigravity Rules",
			Output:  formatted,
		}

	case "/mcp":
		servers := h.Opts.Config.GetMCPServers()
		if len(servers) == 0 {
			return Result{
				Handled: true,
				Title:   "MCP Servers & Extensions",
				Output:  " • No external MCP servers configured. (Use 'mg mcp add' or config.json)",
			}
		}
		var lines []string
		for _, s := range servers {
			lines = append(lines, " • "+s)
		}
		return Result{
			Handled: true,
			Title:   "MCP Servers & Extensions",
			Output:  strings.Join(lines, "\n"),
		}

	case "/dashboard":
		return Result{
			Handled: true,
			Title:   "Dashboard",
			Output:  "Launching Telemetry Control Center (mg dashboard)... Run in separate terminal or use 'mg dashboard -w'.",
		}

	case "/fix":
		return Result{
			Handled: true,
			Title:   "Fix",
			Output:  "Running autonomous test healing verifier (mg fix)...",
		}

	case "/ci":
		return Result{
			Handled: true,
			Title:   "CI",
			Output:  "Generate CI workflow with: mg ci -o .github/workflows",
		}

	default:
		return Result{Handled: false}
	}
}

// HelpText returns the shared command overview.
func HelpText() string {
	return strings.Join([]string{
		"Available commands:",
		"  /exit, /quit - Leave chat",
		"  /clear       - Clear history",
		"  /local       - Toggle local-only routing (/local [on|off|hybrid|local-only|cloud-only|status])",
		"  /theme       - Switch UI theme (/theme [dark|light|auto|tokyo-night|catppuccin|high-contrast])",
		"  /model       - Interactive model selector / switch model (/model [id])",
		"  /caveman     - Toggle caveman mode (/caveman [lite|full|ultra|off])",
		"  /subagents   - Inspect/spawn/kill subagents (/subagents list|spawn|kill-all)",
		"  /swarm       - Toggle swarm monitor panel (/swarm [on|off])",
		"  /schedule    - Set timers (/schedule timer 10s 'msg')",
		"  /session     - Switch/resume a session (/session <name>)",
		"  /rewind      - Rollback or open checkpoint picker",
		"  /undo        - Rollback last turn's file modifications",
		"  /diff        - View working tree git diff",
		"  /usage       - Show session context & token budget",
		"  /audit       - Run project health & directive audit",
		"  /paste       - Multi-line paste buffer mode",
		"  /skills      - List or search loaded skills",
		"  /memory      - Search session conversation memory",
		"  /rules       - Show active workspace & global rules",
		"  /mcp         - List configured MCP servers",
		"  /plan        - Request structured execution plan",
		"  /goal        - Execute autonomous goal loop",
		"  /learn       - Persist custom directive to rules.md",
		"  /grill-me    - Interactive requirements interview",
		"  /help        - Show this message",
	}, "\n")
}

// ApplyTheme validates, applies to UI, and persists the chosen theme via the
// Handler's ConfigStore seam.
func (h *Handler) ApplyTheme(raw string) Result {
	arg := strings.ToLower(strings.TrimSpace(raw))
	if !IsSupportedTheme(arg) {
		return Result{
			Handled: true,
			Title:   "Theme",
			Err:     fmt.Errorf("unknown theme %q (supported: %s)", arg, strings.Join(SupportedThemes(), ", ")),
		}
	}
	active := h.Opts.Config.ApplyUITheme(arg)
	if err := h.Opts.Config.SetTheme(arg); err != nil {
		return Result{
			Handled: true,
			Title:   "Theme",
			Err:     err,
		}
	}
	msg := fmt.Sprintf("Theme switched to %s.", arg)
	if active != "" && active != arg {
		msg = fmt.Sprintf("Theme switched to %s (active: %s).", arg, active)
	}
	return Result{
		Handled:   true,
		Title:     "Theme",
		Output:    msg,
		ActionArg: active,
	}
}

func (h *Handler) handleTheme(raw string) Result {
	arg := strings.ToLower(strings.TrimSpace(raw))
	active := h.Opts.Config.ActiveUITheme()
	if active == "" {
		active = "dark"
	}
	statusMsg := fmt.Sprintf("Active theme: %s (available: %s)", active, strings.Join(SupportedThemes(), ", "))
	if arg == "" {
		act := ActionNone
		if h.Opts.SupportsOverlays {
			act = ActionOpenThemePicker
		}
		return Result{
			Handled: true,
			Title:   "Theme",
			Output:  statusMsg,
			Action:  act,
		}
	}
	if arg == "status" {
		return Result{
			Handled: true,
			Title:   "Theme",
			Output:  statusMsg,
		}
	}
	return h.ApplyTheme(arg)
}

func (h *Handler) handleLocal(raw string) Result {
	arg := strings.ToLower(strings.TrimSpace(raw))
	switch arg {
	case "", "status":
		info := h.snapshot()
		mode := info.RoutingMode
		if mode == "" {
			mode = h.Opts.Config.GetRoutingMode()
		}
		fast := h.Opts.DefaultLocalModel
		if fast == "" {
			fast = "gemma:2b"
		}
		if info.LocalFastModel != "" {
			fast = info.LocalFastModel
		} else if info.LocalModelName != "" {
			fast = info.LocalModelName
		}
		heavy := info.LocalHeavyModel
		if heavy == "" {
			heavy = fast
		}
		msg := fmt.Sprintf("Routing mode: %s (fast=%s, heavy=%s)", mode, fast, heavy)
		if info.OpenAIBaseURL != "" {
			msg += fmt.Sprintf(" [openai base-url=%s]", info.OpenAIBaseURL)
		}
		return Result{
			Handled: true,
			Title:   "Local Routing",
			Output:  msg,
		}

	case "on", "local", "local-only", "offline":
		if h.Sess != nil {
			h.Sess.SetRoutingMode(RoutingModeLocalOnly)
		}
		if err := h.Opts.Config.SetRoutingMode(RoutingModeLocalOnly); err != nil {
			return Result{Handled: true, Title: "Local Routing", Err: err}
		}
		return Result{
			Handled: true,
			Title:   "Local Routing",
			Output:  "Routing mode switched to local-only (cloud calls disabled).",
		}

	case "off", "hybrid", "auto":
		if h.Sess != nil {
			h.Sess.SetRoutingMode(RoutingModeHybrid)
		}
		if err := h.Opts.Config.SetRoutingMode(RoutingModeHybrid); err != nil {
			return Result{Handled: true, Title: "Local Routing", Err: err}
		}
		return Result{
			Handled: true,
			Title:   "Local Routing",
			Output:  "Routing mode switched to hybrid.",
		}

	case "cloud", "cloud-only":
		if h.Sess != nil {
			h.Sess.SetRoutingMode(RoutingModeCloudOnly)
		}
		if err := h.Opts.Config.SetRoutingMode(RoutingModeCloudOnly); err != nil {
			return Result{Handled: true, Title: "Local Routing", Err: err}
		}
		return Result{
			Handled: true,
			Title:   "Local Routing",
			Output:  "Routing mode switched to cloud-only.",
		}

	default:
		return Result{
			Handled: true,
			Title:   "Local Routing",
			Output:  "Usage: /local [on|off|hybrid|local-only|cloud-only|status]",
		}
	}
}

// ApplyModel updates the session and persisted active model from a catalog entry.
func (h *Handler) ApplyModel(ctx context.Context, m ModelOption) Result {
	if h.Sess != nil {
		h.Sess.SetModel(m.ID, m.Provider)
	}
	if err := h.Opts.Config.SetActiveModel(m.ID); err != nil {
		return Result{Handled: true, Title: "Model", Err: err}
	}
	var extra string
	if m.Provider != "cloud" && h.Opts.EnsureLocalModel != nil {
		if h.Opts.EnsureLocalModel(ctx, m.ID) {
			extra = fmt.Sprintf("\nModel %s downloading in background — prompt anytime!", m.ID)
		}
	}
	badge := m.Badge
	if badge == "" {
		badge = strings.ToUpper(m.Provider)
	}
	name := m.Name
	if name == "" {
		name = m.ID
	}
	return Result{
		Handled:   true,
		Title:     "Model",
		Output:    fmt.Sprintf("Active model switched to: %s [%s] (%s)%s", m.ID, badge, name, extra),
		ActionArg: m.ID,
	}
}

func (h *Handler) handleModel(ctx context.Context, raw string) Result {
	target := strings.TrimSpace(raw)
	if target == "" {
		return Result{
			Handled: true,
			Title:   "Model",
			Output:  fmt.Sprintf("Active model: %s (use '/model <id>' to switch)", h.activeModelName(h.snapshot())),
			Action:  ActionPickModel,
		}
	}
	if h.Opts.ScanModels != nil {
		catalog, _ := h.Opts.ScanModels(ctx)
		for _, m := range catalog {
			if m.ID == target {
				return h.ApplyModel(ctx, m)
			}
		}
	}
	return Result{
		Handled: true,
		Title:   "Model",
		Err:     fmt.Errorf("Unknown model '%s'. Launch selector with '/model' or list with 'mg models list'.", target),
	}
}

func (h *Handler) handleCaveman(raw string) Result {
	arg := strings.ToLower(strings.TrimSpace(raw))
	if arg == "off" || arg == "disable" || arg == "stop" {
		if h.Sess != nil {
			h.Sess.SetCavemanLevel("off")
		}
		return Result{
			Handled: true,
			Title:   "Caveman",
			Output:  "Caveman mode disabled. Normal communication restored.",
		}
	}
	level := "full"
	if arg == "lite" || arg == "ultra" || arg == "max" {
		level = arg
	}
	if h.Sess != nil {
		h.Sess.SetCavemanLevel(level)
	}
	return Result{
		Handled: true,
		Title:   "Caveman",
		Output:  fmt.Sprintf("Caveman mode active (level: %s). Ultra-compressed responses.", level),
	}
}

func (h *Handler) handleImmediateRewind() Result {
	if h.Sess == nil {
		return Result{
			Handled: true,
			Title:   "Rewind",
			Err:     fmt.Errorf("no active engine session to rewind"),
		}
	}
	rewound, err := h.Sess.Rewind()
	if err != nil {
		return Result{
			Handled: true,
			Title:   "Rewind",
			Err:     fmt.Errorf("Rewind failed: %w", err),
		}
	}
	shortSHA := rewound.CommitSHA
	if len(shortSHA) > 8 {
		shortSHA = shortSHA[:8]
	}
	return Result{
		Handled: true,
		Title:   "System Directive",
		Output:  fmt.Sprintf("Rewound working tree to checkpoint %s (Turn %d, Commit: %s)", rewound.ID, rewound.Turn, shortSHA),
		Action:  ActionRefreshGit,
	}
}

func (h *Handler) handleSession(cmdWord, raw string) Result {
	name := strings.TrimSpace(raw)
	if name == "" {
		if cmdWord == "/sessions" {
			return Result{
				Handled: true,
				Title:   "Session",
				Output:  "Usage: /session <name>",
				Action:  ActionPickSession,
			}
		}
		current := h.snapshot().SessionName
		if current == "" {
			current = "default"
		}
		return Result{
			Handled: true,
			Title:   "Session",
			Output:  fmt.Sprintf("Active session: %s (Usage: /session <name>)", current),
		}
	}
	if h.Sess == nil {
		return Result{
			Handled: true,
			Title:   "Session",
			Err:     fmt.Errorf("no active session"),
		}
	}
	if err := h.Sess.SwitchSession(name); err != nil {
		return Result{
			Handled: true,
			Title:   "Session",
			Err:     err,
		}
	}
	return Result{
		Handled:   true,
		Title:     "Session",
		Output:    fmt.Sprintf("Switched to session %q", name),
		Action:    ActionReloadSession,
		ActionArg: name,
	}
}

// ContextUsage computes estimated token usage, model token ceiling, percentage,
// and total character/message counts for the session.
func (h *Handler) ContextUsage() (usedTokens, maxTokens, pct, charCount, msgCount int) {
	info := h.snapshot()
	msgCount = info.MessageCount
	charCount = info.MessageChars
	if charCount == 0 {
		charCount = len(info.ConversationText)
	}
	if charCount > 0 {
		usedTokens = (charCount + 3) / 4
	}
	modelID := h.activeModelName(info)
	maxTokens = pruner.MaxTokensForModel(modelID)
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
	return usedTokens, maxTokens, pct, charCount, msgCount
}

func (h *Handler) handleUsage() Result {
	usedTokens, maxTokens, pct, chars, msgCount := h.ContextUsage()
	activeModel := h.activeModelName(h.snapshot())
	output := strings.Join([]string{
		fmt.Sprintf(" • Active Model Tier: %s", activeModel),
		" • Remaining G1 Credits: Unlimited Developer Tier",
		fmt.Sprintf(" • Context: ~%d / %d tokens (%d%%) • ~%d characters • Messages: %d", usedTokens, maxTokens, pct, chars, msgCount),
	}, "\n")
	return Result{
		Handled: true,
		Title:   "Usage",
		Output:  output,
	}
}

func (h *Handler) handleSubagents(ctx context.Context, raw string) Result {
	arg := strings.TrimSpace(raw)
	switch {
	case arg == "" || arg == "list":
		list := h.Opts.Subagents.List()
		if len(list) == 0 {
			return Result{
				Handled: true,
				Title:   "Active Subagents",
				Output:  "No subagents currently active.",
			}
		}
		var lines []string
		for _, sa := range list {
			lines = append(lines, fmt.Sprintf(" • [%s] %s (%s) - %s [Workspace: %s]", sa.Status, sa.ID, sa.Role, sa.Prompt, sa.Workspace))
		}
		return Result{
			Handled: true,
			Title:   "Active Subagents",
			Output:  strings.Join(lines, "\n"),
		}

	case arg == "kill-all":
		count := h.Opts.Subagents.KillAll()
		return Result{
			Handled: true,
			Title:   "Subagents",
			Output:  fmt.Sprintf("Terminated %d active subagent(s).", count),
		}

	case strings.HasPrefix(arg, "spawn "):
		promptTask := strings.TrimSpace(strings.TrimPrefix(arg, "spawn "))
		if promptTask == "" {
			return Result{
				Handled: true,
				Title:   "Subagents",
				Output:  "Usage: /subagents [list|kill-all|spawn <task>]",
			}
		}
		info, err := h.Opts.Subagents.Spawn(ctx, promptTask)
		if err != nil {
			return Result{Handled: true, Title: "Subagents", Err: err}
		}
		return Result{
			Handled: true,
			Title:   "Subagents",
			Output:  fmt.Sprintf("Spawned Subagent [%s] for task: %s", info.ID, promptTask),
		}

	default:
		return Result{
			Handled: true,
			Title:   "Subagents",
			Output:  "Usage: /subagents [list|kill-all|spawn <task>]",
		}
	}
}

func (h *Handler) handleSchedule(raw string) Result {
	parts := strings.Fields(strings.TrimSpace(raw))
	if len(parts) < 3 || parts[0] != "timer" {
		return Result{
			Handled: true,
			Title:   "Schedule",
			Output:  "Usage: /schedule timer <duration e.g. 10s|5m> <message>",
		}
	}
	dur, err := time.ParseDuration(parts[1])
	if err != nil {
		return Result{
			Handled: true,
			Title:   "Schedule",
			Err:     fmt.Errorf("Invalid duration: %w", err),
		}
	}
	if h.Sess == nil {
		return Result{
			Handled: true,
			Title:   "Schedule",
			Err:     fmt.Errorf("scheduler is not initialized on this session"),
		}
	}
	msg := strings.Join(parts[2:], " ")
	id, err := h.Sess.ScheduleTimer(dur, msg)
	if err != nil {
		return Result{
			Handled: true,
			Title:   "Schedule",
			Err:     err,
		}
	}
	return Result{
		Handled: true,
		Title:   "Schedule",
		Output:  fmt.Sprintf("Scheduled timer #%d for %s: %s", id, dur, msg),
	}
}

func (h *Handler) handleSkills(raw string) Result {
	loaded := h.snapshot().LoadedSkills
	if len(loaded) == 0 {
		return Result{
			Handled: true,
			Title:   "Skills",
			Output:  "No skills loaded. Run 'microgravity skills add <name>' to create one.",
		}
	}
	query := strings.TrimSpace(raw)
	if query == "" || query == "pick" {
		var lines []string
		for _, s := range loaded {
			lines = append(lines, skills.FormatSkillSummary(s))
		}
		return Result{
			Handled: true,
			Title:   fmt.Sprintf("Loaded Skills (%d)", len(loaded)),
			Output:  strings.Join(lines, "\n"),
			Action:  ActionPickSkill,
		}
	}

	q := strings.ToLower(query)
	var matches []string
	for _, s := range loaded {
		if strings.Contains(strings.ToLower(s.Name), q) ||
			strings.Contains(strings.ToLower(s.Description), q) ||
			strings.Contains(strings.ToLower(s.Type), q) {
			matches = append(matches, skills.FormatSkillSummary(s))
		}
	}
	if len(matches) == 0 {
		return Result{
			Handled: true,
			Title:   "Skills",
			Output:  fmt.Sprintf("No skills matching %q", query),
		}
	}
	return Result{
		Handled: true,
		Title:   fmt.Sprintf("Skill Search: %q (%d)", query, len(matches)),
		Output:  strings.Join(matches, "\n"),
	}
}

func (h *Handler) handleMemory(raw string) Result {
	query := strings.TrimSpace(raw)
	if query == "" {
		return Result{
			Handled: true,
			Title:   "Memory",
			Output:  "Usage: /memory <search terms>",
		}
	}
	savedConvo := h.snapshot().ConversationText
	var matches []string
	qLower := strings.ToLower(query)
	for _, line := range strings.Split(savedConvo, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		if strings.Contains(strings.ToLower(line), qLower) {
			matches = append(matches, " • "+line)
		}
	}
	if len(matches) == 0 {
		return Result{
			Handled: true,
			Title:   "Memory",
			Output:  fmt.Sprintf("No persistent memory results for %q", query),
		}
	}
	total := len(matches)
	if len(matches) > 10 {
		matches = matches[:10]
	}
	return Result{
		Handled: true,
		Title:   fmt.Sprintf("Memory Context Search (%d)", total),
		Output:  strings.Join(matches, "\n"),
	}
}

func (h *Handler) handleLearn(raw string) Result {
	ruleText := strings.TrimSpace(raw)
	if ruleText == "" {
		return Result{
			Handled: true,
			Title:   "Learn",
			Output:  "Usage: /learn <directive or rule text>",
		}
	}
	rulesDir := filepath.Join(h.cwd(), ".microgravity")
	if err := os.MkdirAll(rulesDir, 0o755); err != nil {
		return Result{Handled: true, Title: "Learn", Err: err}
	}
	rulesPath := filepath.Join(rulesDir, "rules.md")
	f, err := os.OpenFile(rulesPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return Result{Handled: true, Title: "Learn", Err: err}
	}
	defer f.Close()
	if _, err := f.WriteString("\n- " + ruleText + "\n"); err != nil {
		return Result{Handled: true, Title: "Learn", Err: err}
	}
	return Result{
		Handled: true,
		Title:   "Learn",
		Output:  "Directive persisted to " + rulesPath,
	}
}

func (h *Handler) snapshot() SessionInfo {
	if h.Sess == nil {
		return SessionInfo{}
	}
	return h.Sess.Snapshot()
}

func (h *Handler) cwd() string {
	if dir := h.snapshot().Cwd; dir != "" {
		return dir
	}
	wd, _ := os.Getwd()
	return wd
}

func (h *Handler) activeModelName(info SessionInfo) string {
	if info.ActiveModel != "" {
		return info.ActiveModel
	}
	if info.CloudModelName != "" {
		return info.CloudModelName
	}
	if info.LocalModelName != "" {
		return info.LocalModelName
	}
	if m := h.Opts.Config.GetActiveModel(); m != "" {
		return m
	}
	return "default"
}

func splitCommand(trimmed string) (cmdWord, arg string) {
	idx := strings.IndexAny(trimmed, " \t\r\n")
	if idx == -1 {
		return strings.ToLower(trimmed), ""
	}
	return strings.ToLower(trimmed[:idx]), strings.TrimSpace(trimmed[idx+1:])
}

// CheckpointItem represents a discoverable git checkpoint for rewind UIs.
type CheckpointItem struct {
	ID        string
	Turn      int
	CommitSHA string
	ShortSHA  string
	Message   string
	When      string
	DiffStat  string
}

// LoadCheckpoints lists up to 15 session checkpoints from git refs and the
// active session's latest in-memory checkpoint.
func LoadCheckpoints(sess Session) []CheckpointItem {
	var items []CheckpointItem
	seenSHA := make(map[string]bool)

	var cwd string
	if sess != nil {
		cwd = sess.Snapshot().Cwd
	}

	if cwd != "" {
		cmd := exec.Command("git", "for-each-ref", "--sort=-creatordate",
			"--format=%(refname:short)|%(objectname:short)|%(objectname)|%(creatordate:relative)|%(subject)",
			"refs/microgravity/checkpoints/")
		cmd.Dir = cwd
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
				diffStat := gitDiffShortStat(cwd, fullSHA)
				items = append(items, CheckpointItem{
					ID:        fmt.Sprintf("cp-%d", turn),
					Turn:      turn,
					CommitSHA: fullSHA,
					ShortSHA:  shortSHA,
					Message:   subject,
					When:      when,
					DiffStat:  diffStat,
				})
				seenSHA[fullSHA] = true
				if len(items) >= 15 {
					break
				}
			}
		}
	}

	if sess != nil {
		if latest, ok := sess.LatestCheckpoint(); ok && !seenSHA[latest.CommitSHA] {
			shortSHA := latest.CommitSHA
			if len(shortSHA) > 8 {
				shortSHA = shortSHA[:8]
			}
			diffStat := ""
			if cwd != "" {
				diffStat = gitDiffShortStat(cwd, latest.CommitSHA)
			}
			items = append([]CheckpointItem{{
				ID:        latest.ID,
				Turn:      latest.Turn,
				CommitSHA: latest.CommitSHA,
				ShortSHA:  shortSHA,
				Message:   latest.Message,
				When:      latest.Timestamp.Format("15:04:05"),
				DiffStat:  diffStat,
			}}, items...)
		}
	}

	return items
}

// RestoreCheckpoint restores a selected CheckpointItem on sess.
func RestoreCheckpoint(sess Session, selected CheckpointItem) error {
	if sess == nil {
		return fmt.Errorf("checkpoint manager not initialized")
	}
	if latest, ok := sess.LatestCheckpoint(); ok && (latest.CommitSHA == selected.CommitSHA || latest.ID == selected.ID) {
		_, err := sess.Rewind()
		return err
	}
	return sess.RestoreCheckpoint(selected.CommitSHA)
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

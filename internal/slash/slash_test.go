package slash

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/CmfH009/microgravity/internal/config"
	"github.com/CmfH009/microgravity/internal/git"
	"github.com/CmfH009/microgravity/internal/skills"
	"github.com/CmfH009/microgravity/internal/subagent"
	"github.com/CmfH009/microgravity/internal/ui"
)

type fakeSession struct {
	info            SessionInfo
	explicitProv    string
	mode            string
	cavemanLevel    string
	cleared         bool
	ckptMgr         *git.CheckpointManager
	scheduledTimers []string
	nextTimerID     int
}

func newFakeSession(cwd string) *fakeSession {
	return &fakeSession{
		info: SessionInfo{
			SessionName:      "slash-test",
			Cwd:              cwd,
			ActiveModel:      "gemini-3.6-flash",
			CloudModelName:   "gemini-3.6-flash",
			LocalModelName:   "gemma:2b",
			LocalFastModel:   "gemma4:e2b",
			LocalHeavyModel:  "qwen2.5-coder:7b",
			RoutingMode:      RoutingModeHybrid,
			ConversationText: "You: Remember project codename Aurora\nAgent: Noted.",
			MessageChars:     52,
			MessageCount:     2,
			MCPCount:         1,
			LoadedSkills: []*skills.Skill{
				{Name: "codebase-design", Description: "Design deep modules", Type: "prompt", Body: "Deep modules body"},
				{Name: "golang-pro", Description: "Master Go patterns", Type: "prompt"},
			},
		},
		ckptMgr:     git.NewCheckpointManager(cwd, "slash-test"),
		nextTimerID: 1,
	}
}

func (s *fakeSession) Snapshot() SessionInfo { return s.info }
func (s *fakeSession) ClearHistory() {
	s.cleared = true
	s.info.ConversationText = ""
	s.info.MessageChars = 0
	s.info.MessageCount = 0
}
func (s *fakeSession) SetRoutingMode(mode string) { s.info.RoutingMode = mode }
func (s *fakeSession) SetModel(modelID, provider string) {
	s.explicitProv = provider
	s.info.ActiveModel = modelID
	if provider == "cloud" {
		s.info.CloudModelName = modelID
	} else {
		s.info.LocalModelName = modelID
	}
}
func (s *fakeSession) SetMode(mode string)          { s.mode = mode }
func (s *fakeSession) SetCavemanLevel(level string) { s.cavemanLevel = level }
func (s *fakeSession) SwitchSession(name string) error {
	s.info.SessionName = name
	return nil
}
func (s *fakeSession) Rewind() (*git.Checkpoint, error) {
	if s.ckptMgr == nil {
		return nil, fmt.Errorf("no checkpoint manager")
	}
	return s.ckptMgr.Rewind()
}
func (s *fakeSession) RestoreCheckpoint(commitSHA string) error {
	if s.ckptMgr == nil {
		return fmt.Errorf("no checkpoint manager")
	}
	return s.ckptMgr.RestoreCheckpoint(commitSHA)
}
func (s *fakeSession) LatestCheckpoint() (*git.Checkpoint, bool) {
	if s.ckptMgr == nil {
		return nil, false
	}
	return s.ckptMgr.Latest()
}
func (s *fakeSession) ScheduleTimer(dur time.Duration, msg string) (int, error) {
	id := s.nextTimerID
	s.nextTimerID++
	s.scheduledTimers = append(s.scheduledTimers, fmt.Sprintf("%s:%s", dur, msg))
	return id, nil
}

type fakeConfigStore struct {
	routingMode string
	theme       string
	uiTheme     string
	activeModel string
	mcpServers  []string
}

func newFakeConfigStore() *fakeConfigStore {
	return &fakeConfigStore{
		routingMode: RoutingModeHybrid,
		theme:       "dark",
		uiTheme:     "dark",
		activeModel: "gemini-3.6-flash",
	}
}

func (f *fakeConfigStore) GetRoutingMode() string { return f.routingMode }
func (f *fakeConfigStore) SetRoutingMode(mode string) error {
	f.routingMode = mode
	return nil
}
func (f *fakeConfigStore) GetTheme() string { return f.theme }
func (f *fakeConfigStore) SetTheme(theme string) error {
	f.theme = theme
	return nil
}
func (f *fakeConfigStore) ApplyUITheme(theme string) string {
	if theme == "auto" {
		f.uiTheme = "dark"
	} else {
		f.uiTheme = theme
	}
	return f.uiTheme
}
func (f *fakeConfigStore) ActiveUITheme() string  { return f.uiTheme }
func (f *fakeConfigStore) GetActiveModel() string { return f.activeModel }
func (f *fakeConfigStore) SetActiveModel(modelID string) error {
	f.activeModel = modelID
	return nil
}
func (f *fakeConfigStore) GetMCPServers() []string { return f.mcpServers }

type fakeSubagents struct {
	items   []*subagent.SubagentInfo
	killed  int
	spawned string
}

func (f *fakeSubagents) List() []*subagent.SubagentInfo { return f.items }
func (f *fakeSubagents) KillAll() int {
	n := len(f.items)
	f.items = nil
	f.killed += n
	return n
}
func (f *fakeSubagents) Spawn(ctx context.Context, promptTask string) (*subagent.SubagentInfo, error) {
	f.spawned = promptTask
	info := &subagent.SubagentInfo{
		ID:        "sub-test-1",
		Role:      "Subagent Worker",
		Prompt:    promptTask,
		Status:    "RUNNING",
		Workspace: subagent.WorkspaceInherit,
	}
	f.items = append(f.items, info)
	return info, nil
}

func TestHandler_NonSlashAndUnknownSlash(t *testing.T) {
	sess := newFakeSession(t.TempDir())
	h := NewHandler(sess, Options{Config: newFakeConfigStore()})

	if res := h.Execute(context.Background(), "hello world"); res.Handled {
		t.Fatalf("expected non-slash input to be unhandled, got %+v", res)
	}
	if res := h.Execute(context.Background(), "/nonexistent-command"); res.Handled {
		t.Fatalf("expected unknown slash command to be unhandled, got %+v", res)
	}
}

func TestHandler_ExitClearAndHelp(t *testing.T) {
	sess := newFakeSession(t.TempDir())
	h := NewHandler(sess, Options{Config: newFakeConfigStore()})

	exitRes := h.Execute(context.Background(), "/exit")
	if !exitRes.Handled || exitRes.Action != ActionExit {
		t.Fatalf("unexpected /exit result: %+v", exitRes)
	}
	quitRes := h.Execute(context.Background(), "/quit")
	if !quitRes.Handled || quitRes.Action != ActionExit {
		t.Fatalf("unexpected /quit result: %+v", quitRes)
	}

	clearRes := h.Execute(context.Background(), "/clear")
	if !clearRes.Handled || clearRes.Action != ActionClear || !sess.cleared || sess.info.MessageCount != 0 {
		t.Fatalf("unexpected /clear result: %+v", clearRes)
	}

	helpRes := h.Execute(context.Background(), "/help")
	if !helpRes.Handled || helpRes.Action != ActionHelp || !strings.Contains(helpRes.Output, "/theme") || !strings.Contains(helpRes.Output, "/local") {
		t.Fatalf("unexpected /help result: %+v", helpRes)
	}
	if len(Catalog()) < 25 {
		t.Fatalf("expected full shared command catalog, got %d entries", len(Catalog()))
	}
}

func TestHandler_ThemeValidationAndConfigPersistence(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("APPDATA", tmpHome)
	t.Cleanup(func() { ui.SetTheme("dark") })

	if !IsSupportedTheme("auto") || !IsSupportedTheme("tokyo-night") || IsSupportedTheme("bogus") {
		t.Fatal("IsSupportedTheme returned unexpected validation results")
	}

	sess := newFakeSession(t.TempDir())
	hCLI := NewHandler(sess, Options{SupportsOverlays: false})
	hTUI := NewHandler(sess, Options{SupportsOverlays: true})

	// /theme without args: CLI returns status, TUI requests theme picker overlay
	cliBlank := hCLI.Execute(context.Background(), "/theme")
	if !cliBlank.Handled || cliBlank.Action != ActionNone || !strings.Contains(cliBlank.Output, "Active theme:") {
		t.Fatalf("unexpected CLI /theme result: %+v", cliBlank)
	}
	tuiBlank := hTUI.Execute(context.Background(), "/theme")
	if !tuiBlank.Handled || tuiBlank.Action != ActionOpenThemePicker {
		t.Fatalf("expected ActionOpenThemePicker for TUI /theme, got %+v", tuiBlank)
	}

	// /theme catppuccin updates UI AND persists via config.SetTheme
	res := hTUI.Execute(context.Background(), "/theme catppuccin")
	if !res.Handled || res.Err != nil {
		t.Fatalf("/theme catppuccin failed: %+v", res)
	}
	if ui.ActiveThemeName() != "catppuccin" {
		t.Fatalf("ui.ActiveThemeName() = %q, want catppuccin", ui.ActiveThemeName())
	}
	if gotCfg := config.GetTheme(); gotCfg != "catppuccin" {
		t.Fatalf("config.GetTheme() = %q, want catppuccin", gotCfg)
	}

	// /theme auto persists "auto" and resolves active UI theme
	autoRes := hTUI.Execute(context.Background(), "/theme auto")
	if !autoRes.Handled || autoRes.Err != nil {
		t.Fatalf("/theme auto failed: %+v", autoRes)
	}
	if gotCfg := config.GetTheme(); gotCfg != "auto" {
		t.Fatalf("config.GetTheme() = %q, want auto", gotCfg)
	}

	// Invalid theme rejected without mutating persisted theme
	badRes := hTUI.Execute(context.Background(), "/theme neon-bogus")
	if !badRes.Handled || badRes.Err == nil || !strings.Contains(badRes.Err.Error(), "unknown theme") {
		t.Fatalf("expected unknown theme error, got %+v", badRes)
	}
	if gotCfg := config.GetTheme(); gotCfg != "auto" {
		t.Fatalf("persisted theme should remain auto after error, got %q", gotCfg)
	}
}

func TestHandler_LocalRoutingModes(t *testing.T) {
	sess := newFakeSession(t.TempDir())
	cfg := newFakeConfigStore()
	h := NewHandler(sess, Options{Config: cfg})

	onRes := h.Execute(context.Background(), "/local on")
	if !onRes.Handled || sess.info.RoutingMode != RoutingModeLocalOnly || cfg.GetRoutingMode() != RoutingModeLocalOnly {
		t.Fatalf("expected local-only mode, got res=%+v sess=%q cfg=%q", onRes, sess.info.RoutingMode, cfg.GetRoutingMode())
	}

	statusRes := h.Execute(context.Background(), "/local status")
	if !strings.Contains(statusRes.Output, "local-only") || !strings.Contains(statusRes.Output, "gemma4:e2b") {
		t.Fatalf("unexpected /local status output: %q", statusRes.Output)
	}

	offRes := h.Execute(context.Background(), "/local off")
	if sess.info.RoutingMode != RoutingModeHybrid || cfg.GetRoutingMode() != RoutingModeHybrid {
		t.Fatalf("expected hybrid mode after /local off, got sess=%q cfg=%q (%+v)", sess.info.RoutingMode, cfg.GetRoutingMode(), offRes)
	}

	cloudRes := h.Execute(context.Background(), "/local cloud-only")
	if sess.info.RoutingMode != RoutingModeCloudOnly || cfg.GetRoutingMode() != RoutingModeCloudOnly {
		t.Fatalf("expected cloud-only mode, got sess=%q cfg=%q (%+v)", sess.info.RoutingMode, cfg.GetRoutingMode(), cloudRes)
	}

	usageRes := h.Execute(context.Background(), "/local bogus")
	if !strings.Contains(usageRes.Output, "Usage: /local") {
		t.Fatalf("expected /local usage message, got %+v", usageRes)
	}
}

func TestHandler_ModelSwitching(t *testing.T) {
	sess := newFakeSession(t.TempDir())
	cfg := newFakeConfigStore()
	var pulledModel string

	h := NewHandler(sess, Options{
		Config: cfg,
		ScanModels: func(ctx context.Context) ([]ModelOption, error) {
			return []ModelOption{
				{ID: "gemini-3.1-pro", Name: "Gemini 3.1 Pro Cloud", Provider: "cloud", Badge: "PRO"},
				{ID: "qwen2.5-coder:7b", Name: "Qwen 2.5 Coder 7B Local", Provider: "ollama", Badge: "LOCAL"},
			}, nil
		},
		EnsureLocalModel: func(ctx context.Context, modelID string) bool {
			pulledModel = modelID
			return true
		},
	})

	// Empty /model triggers ActionPickModel
	pickRes := h.Execute(context.Background(), "/model")
	if !pickRes.Handled || pickRes.Action != ActionPickModel {
		t.Fatalf("expected ActionPickModel on bare /model, got %+v", pickRes)
	}

	// Switch to cloud model
	cloudRes := h.Execute(context.Background(), "/model gemini-3.1-pro")
	if cloudRes.Err != nil || sess.info.ActiveModel != "gemini-3.1-pro" || sess.explicitProv != "cloud" || cfg.GetActiveModel() != "gemini-3.1-pro" {
		t.Fatalf("unexpected cloud model switch: %+v", cloudRes)
	}

	// Switch to local model triggers EnsureLocalModel seam
	localRes := h.Execute(context.Background(), "/model qwen2.5-coder:7b")
	if localRes.Err != nil || sess.info.LocalModelName != "qwen2.5-coder:7b" || sess.explicitProv != "ollama" || pulledModel != "qwen2.5-coder:7b" {
		t.Fatalf("unexpected local model switch: %+v", localRes)
	}
	if !strings.Contains(localRes.Output, "downloading in background") {
		t.Fatalf("expected background download notice, got %q", localRes.Output)
	}

	// Unknown model returns error
	unknownRes := h.Execute(context.Background(), "/model unknown:model")
	if unknownRes.Err == nil || !strings.Contains(unknownRes.Err.Error(), "Unknown model") {
		t.Fatalf("expected unknown model error, got %+v", unknownRes)
	}
}

func TestHandler_CavemanPlanGoalGrillMe(t *testing.T) {
	sess := newFakeSession(t.TempDir())
	h := NewHandler(sess, Options{Config: newFakeConfigStore()})

	// /caveman
	res := h.Execute(context.Background(), "/caveman ultra")
	if sess.cavemanLevel != "ultra" || !strings.Contains(res.Output, "level: ultra") {
		t.Fatalf("unexpected /caveman ultra result: %+v (level=%q)", res, sess.cavemanLevel)
	}
	off := h.Execute(context.Background(), "/caveman off")
	if sess.cavemanLevel != "off" || !strings.Contains(off.Output, "disabled") {
		t.Fatalf("unexpected /caveman off result: %+v", off)
	}

	// /plan
	planUsage := h.Execute(context.Background(), "/plan")
	if planUsage.Prompt != "" || !strings.Contains(planUsage.Output, "Usage: /plan") {
		t.Fatalf("expected /plan usage when empty, got %+v", planUsage)
	}
	planRes := h.Execute(context.Background(), "/plan refactor router")
	if sess.mode != "plan" || !strings.HasPrefix(planRes.Prompt, "[PLAN MODE ACTIVATED]") || !strings.Contains(planRes.Prompt, "refactor router") {
		t.Fatalf("unexpected /plan result: %+v", planRes)
	}

	// /grill-me
	grillRes := h.Execute(context.Background(), "/grill-me build billing engine")
	if !strings.HasPrefix(grillRes.Prompt, "[INTERVIEW MODE ACTIVATED]") {
		t.Fatalf("unexpected /grill-me result: %+v", grillRes)
	}

	// /goal
	goalRes := h.Execute(context.Background(), "/goal achieve 100% pass rate")
	if !strings.HasPrefix(goalRes.Prompt, "[GOAL MODE ACTIVATED]") {
		t.Fatalf("unexpected /goal result: %+v", goalRes)
	}
}

func TestHandler_DiffRewindUndoAndCheckpoints(t *testing.T) {
	repoDir := t.TempDir()
	if err := exec.Command("git", "-C", repoDir, "init").Run(); err != nil {
		t.Skipf("git not available: %v", err)
	}
	_ = exec.Command("git", "-C", repoDir, "config", "user.name", "Slash Test").Run()
	_ = exec.Command("git", "-C", repoDir, "config", "user.email", "slash@test.local").Run()
	_ = exec.Command("git", "-C", repoDir, "config", "commit.gpgsign", "false").Run()
	_ = exec.Command("git", "-C", repoDir, "config", "core.autocrlf", "false").Run()

	file := filepath.Join(repoDir, "hello.txt")
	if err := os.WriteFile(file, []byte("v1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_ = exec.Command("git", "-C", repoDir, "add", ".").Run()
	_ = exec.Command("git", "-C", repoDir, "commit", "-m", "init").Run()

	sess := newFakeSession(repoDir)
	if _, err := sess.ckptMgr.CreateCheckpoint(1, "turn 1 checkpoint"); err != nil {
		t.Fatalf("CreateCheckpoint failed: %v", err)
	}
	if err := os.WriteFile(file, []byte("v2-dirty\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// TUI /rewind without args opens picker; LoadCheckpoints finds cp-1
	hTUI := NewHandler(sess, Options{Config: newFakeConfigStore(), SupportsOverlays: true})
	pickerRes := hTUI.Execute(context.Background(), "/rewind")
	if pickerRes.Action != ActionOpenRewindPicker {
		t.Fatalf("expected ActionOpenRewindPicker in TUI mode, got %+v", pickerRes)
	}
	cps := LoadCheckpoints(sess)
	if len(cps) == 0 || cps[0].ID != "cp-1" {
		t.Fatalf("expected cp-1 in LoadCheckpoints, got %+v", cps)
	}

	// /diff shows v2-dirty
	diffRes := hTUI.Execute(context.Background(), "/diff")
	if !strings.Contains(diffRes.Output, "v2-dirty") {
		t.Fatalf("expected git diff to contain v2-dirty, got %q", diffRes.Output)
	}

	// RestoreCheckpoint restores v1
	if err := RestoreCheckpoint(sess, cps[0]); err != nil {
		t.Fatalf("RestoreCheckpoint failed: %v", err)
	}
	data, _ := os.ReadFile(file)
	if strings.ReplaceAll(string(data), "\r\n", "\n") != "v1\n" {
		t.Fatalf("file after RestoreCheckpoint = %q, want v1", string(data))
	}

	// Create a second checkpoint and verify CLI /rewind + /undo immediate rewind
	if _, err := sess.ckptMgr.CreateCheckpoint(2, "turn 2 checkpoint"); err != nil {
		t.Fatalf("CreateCheckpoint(2) failed: %v", err)
	}
	if err := os.WriteFile(file, []byte("v3-dirty\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	undoRes := hTUI.Execute(context.Background(), "/undo")
	if undoRes.Err != nil || undoRes.Action != ActionRefreshGit || !strings.Contains(undoRes.Output, "cp-2") {
		t.Fatalf("unexpected /undo result: %+v", undoRes)
	}
	data, _ = os.ReadFile(file)
	if strings.ReplaceAll(string(data), "\r\n", "\n") != "v1\n" {
		t.Fatalf("file after /undo = %q, want v1", string(data))
	}
}

func TestHandler_SubagentsScheduleSkillsMemoryLearnAndMeta(t *testing.T) {
	cwd := t.TempDir()
	sess := newFakeSession(cwd)

	cfg := newFakeConfigStore()
	cfg.mcpServers = []string{"filesystem-tool", "orbit-mcp"}
	subs := &fakeSubagents{}

	h := NewHandler(sess, Options{
		Config:    cfg,
		Subagents: subs,
		AuditWorkspace: func(dir string) (string, error) {
			return "Audit Grade: A (98/100)", nil
		},
		LoadRules: func(dir string) (string, error) {
			return "- Rule 1: Test first", nil
		},
		RenderChangelog: func() string {
			return "v2.0.0 Changelog"
		},
	})

	// /subagents spawn, list, kill-all
	spawnRes := h.Execute(context.Background(), "/subagents spawn audit security")
	if spawnRes.Err != nil || !strings.Contains(spawnRes.Output, "sub-test-1") {
		t.Fatalf("unexpected /subagents spawn: %+v", spawnRes)
	}
	listRes := h.Execute(context.Background(), "/subagents list")
	if !strings.Contains(listRes.Output, "audit security") {
		t.Fatalf("unexpected /subagents list: %+v", listRes)
	}
	killRes := h.Execute(context.Background(), "/subagents kill-all")
	if !strings.Contains(killRes.Output, "Terminated 1") {
		t.Fatalf("unexpected /subagents kill-all: %+v", killRes)
	}

	// /schedule timer
	schedRes := h.Execute(context.Background(), "/schedule timer 50ms check build")
	if schedRes.Err != nil || !strings.Contains(schedRes.Output, "Scheduled timer #1") || len(sess.scheduledTimers) != 1 {
		t.Fatalf("unexpected /schedule timer result: %+v", schedRes)
	}

	// /skills search
	skillRes := h.Execute(context.Background(), "/skills codebase")
	if !strings.Contains(skillRes.Output, "codebase-design") {
		t.Fatalf("unexpected /skills search result: %+v", skillRes)
	}

	// /memory search
	memRes := h.Execute(context.Background(), "/memory Aurora")
	if !strings.Contains(memRes.Output, "Aurora") {
		t.Fatalf("unexpected /memory result: %+v", memRes)
	}

	// /learn persists directive to .microgravity/rules.md
	learnRes := h.Execute(context.Background(), "/learn Always run go test before handoff")
	if learnRes.Err != nil {
		t.Fatalf("/learn failed: %+v", learnRes)
	}
	rulesBytes, err := os.ReadFile(filepath.Join(cwd, ".microgravity", "rules.md"))
	if err != nil || !strings.Contains(string(rulesBytes), "Always run go test before handoff") {
		t.Fatalf("expected directive in rules.md, got %q (err=%v)", string(rulesBytes), err)
	}

	// /usage, /audit, /rules, /mcp, /changelog, /statusline, /swarm, /session, /teamwork, /dashboard, /fix, /ci, /paste
	if res := h.Execute(context.Background(), "/usage"); !strings.Contains(res.Output, "Context:") {
		t.Fatalf("unexpected /usage: %+v", res)
	}
	if res := h.Execute(context.Background(), "/audit"); !strings.Contains(res.Output, "Audit Grade: A") {
		t.Fatalf("unexpected /audit: %+v", res)
	}
	if res := h.Execute(context.Background(), "/rules"); !strings.Contains(res.Output, "Rule 1") {
		t.Fatalf("unexpected /rules: %+v", res)
	}
	if res := h.Execute(context.Background(), "/mcp"); !strings.Contains(res.Output, "filesystem-tool") {
		t.Fatalf("unexpected /mcp: %+v", res)
	}
	if res := h.Execute(context.Background(), "/changelog"); res.Output != "v2.0.0 Changelog" {
		t.Fatalf("unexpected /changelog: %+v", res)
	}
	if res := h.Execute(context.Background(), "/statusline"); !strings.Contains(res.Output, "Statusline:") {
		t.Fatalf("unexpected /statusline: %+v", res)
	}
	if res := h.Execute(context.Background(), "/swarm on"); res.Action != ActionToggleSwarm || res.ActionArg != "on" {
		t.Fatalf("unexpected /swarm on: %+v", res)
	}
	if res := h.Execute(context.Background(), "/session next-sess"); res.Action != ActionReloadSession || sess.info.SessionName != "next-sess" {
		t.Fatalf("unexpected /session switch: %+v", res)
	}
	if res := h.Execute(context.Background(), "/teamwork"); !strings.Contains(res.Output, "mg swarm") {
		t.Fatalf("unexpected /teamwork: %+v", res)
	}
	if res := h.Execute(context.Background(), "/paste"); res.Action != ActionPaste {
		t.Fatalf("unexpected /paste: %+v", res)
	}
}

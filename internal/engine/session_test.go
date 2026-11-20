package engine_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/purav/microgravity/internal/engine"
	"github.com/purav/microgravity/internal/events"
	"github.com/purav/microgravity/internal/mcp"
	"github.com/purav/microgravity/internal/rules"
	"github.com/purav/microgravity/internal/skills"
	"github.com/purav/microgravity/internal/tools"
)

func TestNewSession_Defaults(t *testing.T) {
	cfg := engine.SessionConfig{
		SessionName: "test-session",
	}

	s, err := engine.NewSession(cfg)
	if err != nil {
		t.Fatalf("NewSession failed: %v", err)
	}
	defer s.Close()

	if s.Config.SessionName != "test-session" {
		t.Errorf("expected session name 'test-session', got '%s'", s.Config.SessionName)
	}
	if s.GetState() != engine.StateIdle {
		t.Errorf("expected initial state StateIdle, got '%s'", s.GetState())
	}
}

func TestSession_BuildSystemPrompt(t *testing.T) {
	r := tools.NewRegistry()
	r.Register(mcp.Tool{
		Name:        "sample_tool",
		Description: "A sample test tool",
	}, func(ctx context.Context, args map[string]interface{}) (string, error) {
		return "sample output", nil
	})

	eb := events.NewEventBus()
	loadedRules := &rules.Rules{
		GlobalContent: "Always be helpful and test thoroughly.",
	}

	testSkills := []*skills.Skill{
		{
			Name:        "test-skill",
			Description: "A skill for testing",
			Type:        "instruction",
		},
	}

	cfg := engine.SessionConfig{
		SessionName:  "prompt-test",
		Registry:     r,
		EventBus:     eb,
		LoadedRules:  loadedRules,
		LoadedSkills: testSkills,
		Mode:         "plan",
		CavemanLevel: "lite",
	}

	s, err := engine.NewSession(cfg)
	if err != nil {
		t.Fatalf("NewSession failed: %v", err)
	}
	defer s.Close()

	prompt := s.BuildSystemPrompt()

	if !strings.Contains(prompt, "Always be helpful and test thoroughly.") {
		t.Errorf("expected prompt to contain global rules, got: %s", prompt)
	}
	if !strings.Contains(prompt, "CAVEMAN MODE ACTIVE") {
		t.Errorf("expected prompt to contain caveman mode, got: %s", prompt)
	}
	if !strings.Contains(prompt, "PLAN MODE ENFORCED") {
		t.Errorf("expected prompt to contain plan mode, got: %s", prompt)
	}
	if !strings.Contains(prompt, "test-skill: A skill for testing") {
		t.Errorf("expected prompt to contain skills, got: %s", prompt)
	}
	if !strings.Contains(prompt, "sample_tool: A sample test tool") {
		t.Errorf("expected prompt to contain sample_tool, got: %s", prompt)
	}
	// Verify that Letta Core Memory block is present by default
	if !strings.Contains(prompt, "<core_memory>") || !strings.Contains(prompt, "</core_memory>") {
		t.Errorf("expected default Letta core memory in system prompt, got: %s", prompt)
	}
}

func TestSession_BuildSystemPrompt_WithLettaMemory(t *testing.T) {
	customMem := "<core_memory>\n  <human>Custom Human Operator</human>\n  <persona>Autonomous Agent</persona>\n  <scratchpad>Immediate Task</scratchpad>\n</core_memory>"
	cfg := engine.SessionConfig{
		SessionName: "custom-letta-test",
		CoreMemory:  customMem,
	}
	s, err := engine.NewSession(cfg)
	if err != nil {
		t.Fatalf("NewSession failed: %v", err)
	}
	defer s.Close()

	prompt := s.BuildSystemPrompt()
	if !strings.Contains(prompt, "<core_memory>") || !strings.Contains(prompt, "</core_memory>") {
		t.Errorf("expected <core_memory> tags in system prompt, got: %s", prompt)
	}
	if !strings.Contains(prompt, "Custom Human Operator") {
		t.Errorf("expected custom human operator in system prompt, got: %s", prompt)
	}
}

func TestSession_StateTransitions(t *testing.T) {
	eb := events.NewEventBus()
	stateCh, subID := eb.Subscribe("session:state")
	defer eb.Unsubscribe("session:state", subID)

	s, err := engine.NewSession(engine.SessionConfig{SessionName: "state-test", EventBus: eb})
	if err != nil {
		t.Fatalf("NewSession failed: %v", err)
	}
	defer s.Close()

	s.SetState(engine.StateThinking)
	if s.GetState() != engine.StateThinking {
		t.Errorf("expected StateThinking, got %s", s.GetState())
	}

	select {
	case msg := <-stateCh:
		if msg != string(engine.StateThinking) {
			t.Errorf("expected 'thinking' on eventbus, got %s", msg)
		}
	default:
		t.Errorf("expected state change event on eventbus")
	}

	s.SetState(engine.StateExecutingTools)
	if s.GetState() != engine.StateExecutingTools {
		t.Errorf("expected StateExecutingTools, got %s", s.GetState())
	}

	select {
	case msg := <-stateCh:
		if msg != string(engine.StateExecutingTools) {
			t.Errorf("expected 'executing_tools' on eventbus, got %s", msg)
		}
	default:
		t.Errorf("expected state change event on eventbus")
	}
}

func TestSession_HistoryManagement(t *testing.T) {
	s, err := engine.NewSession(engine.SessionConfig{SessionName: "history-test"})
	if err != nil {
		t.Fatalf("NewSession failed: %v", err)
	}
	defer s.Close()

	s.Conversation.WriteString("User: Hello\nAgent: Hi\n")
	_ = s.SaveHistory()

	s2, _ := engine.NewSession(engine.SessionConfig{SessionName: "history-test"})
	_ = s2.LoadSavedHistory()

	if !strings.Contains(s2.Conversation.String(), "User: Hello") {
		t.Errorf("expected loaded history to contain 'User: Hello', got: %s", s2.Conversation.String())
	}

	s2.ClearHistory()
	if s2.Conversation.Len() != 0 {
		t.Errorf("expected conversation to be empty after ClearHistory, got len %d", s2.Conversation.Len())
	}
}

func TestSession_CheckpointAndRewind(t *testing.T) {
	tempDir := t.TempDir()
	_ = exec.Command("git", "-C", tempDir, "init").Run()
	_ = exec.Command("git", "-C", tempDir, "config", "user.name", "Test").Run()
	_ = exec.Command("git", "-C", tempDir, "config", "user.email", "test@test.com").Run()
	_ = os.WriteFile(filepath.Join(tempDir, "file.txt"), []byte("initial"), 0644)
	_ = exec.Command("git", "-C", tempDir, "add", ".").Run()
	_ = exec.Command("git", "-C", tempDir, "commit", "-m", "init").Run()

	s, err := engine.NewSession(engine.SessionConfig{SessionName: "ckpt-test", Cwd: tempDir})
	if err != nil {
		t.Fatalf("NewSession failed: %v", err)
	}
	defer s.Close()

	cp, err := s.CreateCheckpoint(1, "test-ckpt")
	if err != nil {
		t.Fatalf("CreateCheckpoint failed: %v", err)
	}
	if cp == nil || cp.CommitSHA == "" {
		t.Fatalf("expected valid checkpoint")
	}

	// Modify file
	_ = os.WriteFile(filepath.Join(tempDir, "file.txt"), []byte("modified"), 0644)

	// Rewind
	rewound, err := s.Rewind()
	if err != nil {
		t.Fatalf("Rewind failed: %v", err)
	}
	if rewound == nil {
		t.Fatalf("expected non-nil rewound checkpoint")
	}

	content, _ := os.ReadFile(filepath.Join(tempDir, "file.txt"))
	if string(content) != "initial" {
		t.Errorf("expected 'initial', got '%s'", string(content))
	}
}

func TestSession_AsyncEventsUnifiedRouting(t *testing.T) {
	eb := events.NewEventBus()
	s, err := engine.NewSession(engine.SessionConfig{SessionName: "events-test", EventBus: eb})
	if err != nil {
		t.Fatalf("NewSession failed: %v", err)
	}
	defer s.Close()

	if s.AsyncEvents == nil {
		t.Fatal("expected AsyncEvents channel to be initialized")
	}

	// 1. Subagent response
	eb.Publish("subagent_responses", "subagent worker completed research")

	// 2. Background task completed
	eb.Publish("task_completed", "background build task finished with code 0")

	// 3. Scheduler alert
	eb.Publish("scheduler_alerts", "timer expired: run health check")

	received := make(map[string]string)
	timeout := time.After(300 * time.Millisecond)

	for len(received) < 3 {
		select {
		case ev := <-s.AsyncEvents:
			received[ev.Topic] = ev.Payload
		case <-timeout:
			t.Fatalf("timed out waiting for AsyncEvents. Received %d/3: %+v", len(received), received)
		}
	}

	if received["subagent_responses"] != "subagent worker completed research" {
		t.Errorf("unexpected subagent payload: %q", received["subagent_responses"])
	}
	if received["task_completed"] != "background build task finished with code 0" {
		t.Errorf("unexpected task payload: %q", received["task_completed"])
	}
	if received["scheduler_alerts"] != "timer expired: run health check" {
		t.Errorf("unexpected scheduler payload: %q", received["scheduler_alerts"])
	}
}



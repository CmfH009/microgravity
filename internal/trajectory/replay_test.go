package trajectory

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeTestTranscript(t *testing.T, sessionID string, steps []Step) string {
	t.Helper()
	home := t.TempDir()
	brainDir := filepath.Join(home, ".gemini", "antigravity-cli", "brain", sessionID, ".system_generated", "logs")
	if err := os.MkdirAll(brainDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	logPath := filepath.Join(brainDir, "transcript_full.jsonl")
	f, err := os.Create(logPath)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	defer f.Close()
	for _, s := range steps {
		b, _ := json.Marshal(s)
		f.Write(append(b, '\n'))
	}
	return logPath
}

func TestNewSessionReplay(t *testing.T) {
	// Override os.UserHomeDir by using a temp dir via symlink is complex;
	// instead, test that NewSessionReplay returns an error for missing session.
	_, err := NewSessionReplay("nonexistent-session-xyz")
	if err == nil {
		t.Fatal("expected error for missing session")
	}
}

func TestFormatStep(t *testing.T) {
	step := Step{
		StepIndex: 1,
		Source:    "user",
		Type:      "message",
		Status:    "completed",
		CreatedAt: "2026-10-19T00:00:00Z",
		Content:   "Hello, agent!",
		ToolCalls: []ToolCallInfo{
			{ToolName: "list_dir", Arguments: map[string]interface{}{"path": "."}},
		},
		Thinking:        "The user wants me to list files.",
		TruncatedFields: []string{"content"},
	}

	out := FormatStep(step)
	if !strings.Contains(out, "[Step 1]") {
		t.Fatalf("expected step index in output, got %q", out)
	}
	if !strings.Contains(out, "user") {
		t.Fatalf("expected source in output, got %q", out)
	}
	if !strings.Contains(out, "message") {
		t.Fatalf("expected type in output, got %q", out)
	}
	if !strings.Contains(out, "completed") {
		t.Fatalf("expected status in output, got %q", out)
	}
	if !strings.Contains(out, "Hello, agent!") {
		t.Fatalf("expected content in output, got %q", out)
	}
	if !strings.Contains(out, "list_dir") {
		t.Fatalf("expected tool name in output, got %q", out)
	}
	if !strings.Contains(out, "Thinking") {
		t.Fatalf("expected thinking in output, got %q", out)
	}
	if !strings.Contains(out, "Truncated") {
		t.Fatalf("expected truncated fields in output, got %q", out)
	}
}

func TestFormatStep_Minimal(t *testing.T) {
	step := Step{
		StepIndex:   1,
		Source:      "agent",
		CreatedAt:   "2026-10-19T00:00:00Z",
		Content:     "I am done.",
	}
	out := FormatStep(step)
	if !strings.Contains(out, "I am done.") {
		t.Fatalf("expected content, got %q", out)
	}
}

func TestHeader(t *testing.T) {
	out := Header("sess-123", 5)
	if !strings.Contains(out, "sess-123") {
		t.Fatalf("expected session ID in header, got %q", out)
	}
	if !strings.Contains(out, "5") {
		t.Fatalf("expected step count in header, got %q", out)
	}
}

func TestSessionReplay_Accessors(t *testing.T) {
	rep := &SessionReplay{
		sessionID: "sess-1",
		steps:     []Step{{StepIndex: 1}, {StepIndex: 2}},
	}
	if rep.SessionID() != "sess-1" {
		t.Fatalf("expected sess-1, got %q", rep.SessionID())
	}
	if rep.NumSteps() != 2 {
		t.Fatalf("expected 2 steps, got %d", rep.NumSteps())
	}
	if len(rep.Steps()) != 2 {
		t.Fatalf("expected 2 steps, got %d", len(rep.Steps()))
	}
}

// Ensure os package is used (for the missing-session test).
var _ = os.Open

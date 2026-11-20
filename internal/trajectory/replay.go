// Package trajectory provides session replay over AGY JSONL transcripts.
//
// Replays read transcript_full.jsonl (untruncated) with fallback to
// transcript.jsonl (compact), and render each step for debugging and audit.

package trajectory

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// NewSessionReplay loads a session transcript from the brain directory.
// Falls back to transcript.jsonl if transcript_full.jsonl is unavailable.
func NewSessionReplay(sessionID string) (*SessionReplay, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		home = "."
	}
	logDir := filepath.Join(home, ".gemini", "antigravity-cli", "brain", sessionID, ".system_generated", "logs")

	fullPath := filepath.Join(logDir, "transcript_full.jsonl")
	compactPath := filepath.Join(logDir, "transcript.jsonl")

	logPath := fullPath
	if _, err := os.Stat(logPath); err != nil {
		logPath = compactPath
	}

	f, err := os.Open(logPath)
	if err != nil {
		return nil, fmt.Errorf("open transcript for session %q: %w", sessionID, err)
	}
	defer f.Close()

	var steps []Step
	scanner := bufio.NewScanner(f)
	scanBuf := make([]byte, 1024*1024)
	scanner.Buffer(scanBuf, 10*1024*1024) // 10MB max for large transcripts
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var step Step
		if err := json.Unmarshal([]byte(line), &step); err == nil {
			steps = append(steps, step)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read transcript: %w", err)
	}

	return &SessionReplay{
		sessionID: sessionID,
		brainDir:  filepath.Join(home, ".gemini", "antigravity-cli", "brain", sessionID),
		steps:     steps,
	}, nil
}

// SessionReplay holds a parsed session transcript.
type SessionReplay struct {
	sessionID string
	brainDir  string
	steps     []Step
}

// SessionID returns the session identifier.
func (r *SessionReplay) SessionID() string { return r.sessionID }

// Steps returns all parsed steps in order.
func (r *SessionReplay) Steps() []Step {
	return r.steps
}

// NumSteps returns the number of steps in the transcript.
func (r *SessionReplay) NumSteps() int {
	return len(r.steps)
}

// FormatStep renders a single step for human-readable replay output.
func FormatStep(step Step) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("[Step %d] %s", step.StepIndex, step.Source))
	if step.Type != "" {
		sb.WriteString(fmt.Sprintf(" / %s", step.Type))
	}
	if step.Status != "" {
		sb.WriteString(fmt.Sprintf(" [%s]", step.Status))
	}
	sb.WriteString(fmt.Sprintf(" — %s\n", step.CreatedAt))

	if step.Content != "" {
		sb.WriteString(step.Content)
		if !strings.HasSuffix(step.Content, "\n") {
			sb.WriteString("\n")
		}
	}

	if len(step.ToolCalls) > 0 {
		sb.WriteString("  Tool calls:\n")
		for _, tc := range step.ToolCalls {
			sb.WriteString(fmt.Sprintf("    - %s", tc.ToolName))
			if len(tc.Arguments) > 0 {
				sb.WriteString(fmt.Sprintf("(%v)", tc.Arguments))
			}
			sb.WriteString("\n")
		}
	}

	if step.Thinking != "" {
		sb.WriteString(fmt.Sprintf("  Thinking: %s\n", step.Thinking))
	}

	if len(step.TruncatedFields) > 0 {
		sb.WriteString(fmt.Sprintf("  Truncated: %v\n", step.TruncatedFields))
	}

	return strings.TrimRight(sb.String(), "\n")
}

// ListSessions returns all available session IDs under the brain directory.
func ListSessions() ([]string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		home = "."
	}
	brainRoot := filepath.Join(home, ".gemini", "antigravity-cli", "brain")

	entries, err := os.ReadDir(brainRoot)
	if err != nil {
		// No brain directory yet — no sessions.
		return nil, nil
	}

	var sessions []string
	for _, entry := range entries {
		if entry.IsDir() {
			sessions = append(sessions, entry.Name())
		}
	}
	return sessions, nil
}

// Header returns a title banner for replay output.
func Header(sessionID string, numSteps int) string {
	return fmt.Sprintf("=== Replaying session %s (%d step(s)) ===", sessionID, numSteps)
}

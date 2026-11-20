package trajectory

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type ToolCallInfo struct {
	ToolName  string                 `json:"tool_name,omitempty"`
	Name      string                 `json:"name,omitempty"`
	Arguments map[string]interface{} `json:"arguments,omitempty"`
}

type Step struct {
	StepIndex       int            `json:"step_index"`
	Source          string         `json:"source"`
	Type            string         `json:"type"`
	Status          string         `json:"status"`
	CreatedAt       string         `json:"created_at"`
	Content         string         `json:"content"`
	Thinking        string         `json:"thinking,omitempty"`
	ToolCalls       []ToolCallInfo `json:"tool_calls,omitempty"`
	TruncatedFields []string       `json:"truncated_fields,omitempty"`
}

type Logger struct {
	mu          sync.Mutex
	sessionID   string
	fullLogPath string
	compactPath string
	stepCounter int
	brainDir    string
}

func NewLogger(sessionID string) (*Logger, error) {
	if sessionID == "" {
		sessionID = fmt.Sprintf("session-%d", time.Now().Unix())
	}

	home, err := os.UserHomeDir()
	if err != nil {
		home = "."
	}

	brainDir := filepath.Join(home, ".gemini", "antigravity-cli", "brain", sessionID)
	baseDir := filepath.Join(brainDir, ".system_generated", "logs")
	if err := os.MkdirAll(baseDir, 0755); err != nil {
		return nil, err
	}

	return &Logger{
		sessionID:   sessionID,
		brainDir:    brainDir,
		fullLogPath: filepath.Join(baseDir, "transcript_full.jsonl"),
		compactPath: filepath.Join(baseDir, "transcript.jsonl"),
	}, nil
}

func (l *Logger) GetBrainDir() string {
	return l.brainDir
}

func (l *Logger) LogStep(source, stepType, status, content, thinking string, toolCalls []ToolCallInfo) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.stepCounter++
	nowIso := time.Now().UTC().Format(time.RFC3339)

	fullStep := Step{
		StepIndex: l.stepCounter,
		Source:    source,
		Type:      stepType,
		Status:    status,
		CreatedAt: nowIso,
		Content:   content,
		Thinking:  thinking,
		ToolCalls: toolCalls,
	}

	fullBytes, err := json.Marshal(fullStep)
	if err != nil {
		return err
	}

	if err := appendLine(l.fullLogPath, fullBytes); err != nil {
		return err
	}

	var truncated []string
	compactContent := content
	if len(compactContent) > 300 {
		compactContent = compactContent[:300] + "... [truncated]"
		truncated = append(truncated, "content")
	}

	compactThinking := thinking
	if len(compactThinking) > 200 {
		compactThinking = compactThinking[:200] + "... [truncated]"
		truncated = append(truncated, "thinking")
	}

	compactStep := Step{
		StepIndex:       l.stepCounter,
		Source:          source,
		Type:            stepType,
		Status:          status,
		CreatedAt:       nowIso,
		Content:         compactContent,
		Thinking:        compactThinking,
		ToolCalls:       toolCalls,
		TruncatedFields: truncated,
	}

	compactBytes, err := json.Marshal(compactStep)
	if err != nil {
		return err
	}

	return appendLine(l.compactPath, compactBytes)
}

func appendLine(path string, data []byte) error {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()

	if _, err := f.Write(append(data, '\n')); err != nil {
		return err
	}
	return nil
}

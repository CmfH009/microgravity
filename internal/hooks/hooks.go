package hooks

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"
)

// EventType defines lifecycle hook trigger points.
type EventType string

const (
	EventPreToolCall  EventType = "pre_tool_call"
	EventPostToolCall EventType = "post_tool_call"
	EventPreTurn      EventType = "pre_turn"
	EventPostTurn     EventType = "post_turn"
)

// Hook represents an individual lifecycle hook handler.
type Hook struct {
	Name           string    `json:"name,omitempty"`
	Event          EventType `json:"event"`
	Command        string    `json:"command,omitempty"`
	Args           []string  `json:"args,omitempty"`
	URL            string    `json:"url,omitempty"`
	TimeoutSeconds int       `json:"timeout_seconds,omitempty"`
}

// Config represents a hooks.json configuration file.
type Config struct {
	Hooks []Hook `json:"hooks"`
}

// Payload is the data sent to the hook executor.
type Payload struct {
	Event     EventType              `json:"event"`
	Timestamp time.Time              `json:"timestamp"`
	ToolName  string                 `json:"tool_name,omitempty"`
	ToolArgs  map[string]interface{} `json:"tool_args,omitempty"`
	Result    string                 `json:"result,omitempty"`
	Error     string                 `json:"error,omitempty"`
	TurnIndex int                    `json:"turn_index,omitempty"`
	Prompt    string                 `json:"prompt,omitempty"`
}

// Manager orchestrates hook discovery and execution.
type Manager struct {
	mu    sync.RWMutex
	hooks []Hook
}

func NewManager() *Manager {
	return &Manager{
		hooks: make([]Hook, 0),
	}
}

// RegisterHook adds a runtime hook.
func (m *Manager) RegisterHook(h Hook) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.hooks = append(m.hooks, h)
}

// LoadConfig loads hooks from a JSON file.
func (m *Manager) LoadConfig(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.hooks = append(m.hooks, cfg.Hooks...)
	return nil
}

// DiscoverWorkspaceHooks scans workspace root for hooks.json.
func (m *Manager) DiscoverWorkspaceHooks(workspaceDir string) {
	candidates := []string{
		filepath.Join(workspaceDir, "hooks.json"),
		filepath.Join(workspaceDir, ".agents", "hooks.json"),
		filepath.Join(workspaceDir, ".agent", "hooks.json"),
	}
	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			_ = m.LoadConfig(c)
		}
	}
}

// Execute triggers all registered hooks for the specified event.
func (m *Manager) Execute(ctx context.Context, payload Payload) error {
	m.mu.RLock()
	activeHooks := make([]Hook, 0)
	for _, h := range m.hooks {
		if h.Event == payload.Event {
			activeHooks = append(activeHooks, h)
		}
	}
	m.mu.RUnlock()

	payload.Timestamp = time.Now()
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	for _, h := range activeHooks {
		timeout := 10 * time.Second
		if h.TimeoutSeconds > 0 {
			timeout = time.Duration(h.TimeoutSeconds) * time.Second
		}
		hookCtx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()

		if h.URL != "" {
			req, err := http.NewRequestWithContext(hookCtx, "POST", h.URL, bytes.NewReader(payloadBytes))
			if err != nil {
				continue
			}
			req.Header.Set("Content-Type", "application/json")
			resp, err := http.DefaultClient.Do(req)
			if err == nil && resp != nil {
				resp.Body.Close()
			}
		} else if h.Command != "" {
			cmd := exec.CommandContext(hookCtx, h.Command, h.Args...)
			cmd.Stdin = bytes.NewReader(payloadBytes)
			cmd.Env = append(os.Environ(), fmt.Sprintf("AGY_HOOK_EVENT=%s", payload.Event))
			_ = cmd.Run()
		}
	}

	return nil
}

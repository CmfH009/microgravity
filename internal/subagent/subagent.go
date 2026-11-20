package subagent

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/purav/microgravity/internal/events"
)

type SubagentProfile string

const (
	ProfileResearcher SubagentProfile = "researcher"
	ProfileQAAuditor  SubagentProfile = "test-engineer"
	ProfileSecurity   SubagentProfile = "security-auditor"
	ProfileGeneral    SubagentProfile = "general"
)

type WorkspaceMode string

const (
	WorkspaceInherit WorkspaceMode = "inherit"
	WorkspaceBranch  WorkspaceMode = "branch"
	WorkspaceShare   WorkspaceMode = "share"
)

type SubagentState string

const (
	StateRunning           SubagentState = "running"
	StateIdle              SubagentState = "idle"
	StateWaitingForInput   SubagentState = "waiting_for_input"
	StateWaitingForDeps    SubagentState = "waiting_for_dependents"
	StateWaitingForMessage SubagentState = "waiting_for_message"
	StateCanceling         SubagentState = "canceling"
	StateErrored           SubagentState = "errored"
	StateUnspecified       SubagentState = "unspecified"
	StateCompleted         SubagentState = "completed"
	StateKilled            SubagentState = "killed"
)

type SubagentDefinition struct {
	Name                string `json:"name"`
	Description         string `json:"description"`
	SystemPrompt        string `json:"system_prompt"`
	EnableWriteTools    bool   `json:"enable_write_tools"`
	EnableSubagentTools bool   `json:"enable_subagent_tools"`
	EnableMCPTools      bool   `json:"enable_mcp_tools"`
}

type SubagentInvocation struct {
	TypeName  string        `json:"type_name"`
	Role      string        `json:"role"`
	Prompt    string        `json:"prompt"`
	Model     string        `json:"model"`
	Workspace WorkspaceMode `json:"workspace"`
}

type SubagentInfo struct {
	mu             sync.RWMutex
	ID             string          `json:"id"`
	ConversationID string          `json:"conversationId"`
	Role           string          `json:"role"`
	TypeName       string          `json:"type"`
	Profile        SubagentProfile `json:"profile"`
	Prompt         string          `json:"prompt"`
	Model          string          `json:"model"`
	Workspace      WorkspaceMode   `json:"workspace"`
	TranscriptURI  string          `json:"transcript"`
	State          SubagentState   `json:"state"`
	StateDetail    string          `json:"stateDetail,omitempty"`
	Status         string          `json:"status"` // RUNNING, COMPLETED, KILLED, FAILED (backward compat)
	Result         string          `json:"result,omitempty"`
	StartTime      time.Time       `json:"start_time"`
	EndTime        time.Time       `json:"end_time,omitempty"`
	cancel         context.CancelFunc
}

// Snapshot returns a thread-safe copy of SubagentInfo without internal mutex/cancel.
func (s *SubagentInfo) Snapshot() *SubagentInfo {
	if s == nil {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return &SubagentInfo{
		ID:             s.ID,
		ConversationID: s.ConversationID,
		Role:           s.Role,
		TypeName:       s.TypeName,
		Profile:        s.Profile,
		Prompt:         s.Prompt,
		Model:          s.Model,
		Workspace:      s.Workspace,
		TranscriptURI:  s.TranscriptURI,
		State:          s.State,
		StateDetail:    s.StateDetail,
		Status:         s.Status,
		Result:         s.Result,
		StartTime:      s.StartTime,
		EndTime:        s.EndTime,
	}
}

// GetStatus returns the status string thread-safely.
func (s *SubagentInfo) GetStatus() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.Status
}

// GetState returns the SubagentState thread-safely.
func (s *SubagentInfo) GetState() SubagentState {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.State
}

// GetResult returns the result string thread-safely.
func (s *SubagentInfo) GetResult() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.Result
}

type Message struct {
	SenderID   string    `json:"sender_id"`
	ReceiverID string    `json:"receiver_id"`
	Content    string    `json:"content"`
	Timestamp  time.Time `json:"timestamp"`
}

type SwarmPipelineStep struct {
	ID        string          `json:"id"`
	Role      string          `json:"role"`
	Profile   SubagentProfile `json:"profile"`
	Prompt    string          `json:"prompt"`
	DependsOn []string        `json:"depends_on,omitempty"`
}

type Manager struct {
	mu          sync.RWMutex
	subagents   map[string]*SubagentInfo
	definitions map[string]SubagentDefinition
	messages    map[string][]Message
	eventBus      *events.EventBus
	brainDir      string
	defaultRunner func(ctx context.Context, sub *SubagentInfo, workDir string) (string, error)
	wtCreator     func(cwd, name string) (string, error)
	wtRemover     func(cwd, name string) error
}

var globalManager *Manager
var once sync.Once

func GetManager() *Manager {
	once.Do(func() {
		home, _ := os.UserHomeDir()
		brain := filepath.Join(home, ".gemini", "antigravity-cli", "brain")
		globalManager = NewManager(nil, brain)
	})
	return globalManager
}

func NewManager(eb *events.EventBus, brainDir string) *Manager {
	if brainDir == "" {
		home, _ := os.UserHomeDir()
		brainDir = filepath.Join(home, ".gemini", "antigravity-cli", "brain")
	}
	m := &Manager{
		subagents:   make(map[string]*SubagentInfo),
		definitions: make(map[string]SubagentDefinition),
		messages:    make(map[string][]Message),
		eventBus:    eb,
		brainDir:    brainDir,
	}

	// Register standard AGY builtin definitions
	m.definitions["self"] = SubagentDefinition{
		Name:                "self",
		Description:         "Subagent that inherits the parent agent's full configuration including tools, system prompt, and model.",
		EnableWriteTools:    true,
		EnableSubagentTools: true,
		EnableMCPTools:      true,
	}
	m.definitions["research"] = SubagentDefinition{
		Name:                "research",
		Description:         "Research subagent with read-only tools for exploring the codebase, searching the web, and reading files.",
		EnableWriteTools:    false,
		EnableSubagentTools: false,
		EnableMCPTools:      true,
	}
	m.definitions["code-reviewer"] = SubagentDefinition{
		Name:                "code-reviewer",
		Description:         "Senior code reviewer that evaluates changes across correctness, readability, architecture, security, and performance.",
		EnableWriteTools:    false,
		EnableSubagentTools: false,
		EnableMCPTools:      true,
	}
	m.definitions["security-auditor"] = SubagentDefinition{
		Name:                "security-auditor",
		Description:         "Security engineer focused on vulnerability detection, threat modeling, and secure coding practices.",
		EnableWriteTools:    false,
		EnableSubagentTools: false,
		EnableMCPTools:      true,
	}
	m.definitions["test-engineer"] = SubagentDefinition{
		Name:                "test-engineer",
		Description:         "QA engineer specialized in test strategy, test writing, and coverage analysis.",
		EnableWriteTools:    true,
		EnableSubagentTools: false,
		EnableMCPTools:      true,
	}
	m.definitions["web-performance-auditor"] = SubagentDefinition{
		Name:                "web-performance-auditor",
		Description:         "Web performance engineer focused on Core Web Vitals, loading, rendering, and network optimization.",
		EnableWriteTools:    false,
		EnableSubagentTools: false,
		EnableMCPTools:      true,
	}

	return m
}

func (m *Manager) SetEventBus(eb *events.EventBus) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.eventBus = eb
}

func (m *Manager) SetDefaultRunner(runner func(ctx context.Context, sub *SubagentInfo, workDir string) (string, error)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.defaultRunner = runner
}

func (m *Manager) SetWorktreeProvider(creator func(cwd, name string) (string, error), remover func(cwd, name string) error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.wtCreator = creator
	m.wtRemover = remover
}

func (m *Manager) DefineSubagent(def SubagentDefinition) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.definitions[def.Name] = def
}

func (m *Manager) GetDefinition(name string) (SubagentDefinition, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	d, ok := m.definitions[name]
	return d, ok
}

func (m *Manager) ListDefinitions() []SubagentDefinition {
	m.mu.RLock()
	defer m.mu.RUnlock()
	defs := make([]SubagentDefinition, 0, len(m.definitions))
	for _, d := range m.definitions {
		defs = append(defs, d)
	}
	return defs
}

func (m *Manager) Spawn(ctx context.Context, id, role string, profile SubagentProfile, prompt, modelOverride string, workspace WorkspaceMode, runner func(ctx context.Context, sub *SubagentInfo) (string, error)) (*SubagentInfo, error) {
	m.mu.Lock()

	subCtx, cancel := context.WithCancel(ctx)

	if modelOverride == "" || modelOverride == "inherit" {
		switch profile {
		case ProfileResearcher:
			modelOverride = "flash_lite"
		case ProfileQAAuditor, ProfileSecurity:
			modelOverride = "pro"
		default:
			modelOverride = "flash"
		}
	}

	if workspace == "" {
		workspace = WorkspaceInherit
	}

	convID := fmt.Sprintf("subagent-%s", id)
	transcriptPath := filepath.Join(m.brainDir, convID, ".system_generated", "logs", "transcript.jsonl")

	typeName := string(profile)
	if typeName == "" {
		typeName = "general"
	}

	info := &SubagentInfo{
		ID:             id,
		ConversationID: convID,
		Role:           role,
		TypeName:       typeName,
		Profile:        profile,
		Prompt:         prompt,
		Model:          modelOverride,
		Workspace:      workspace,
		TranscriptURI:  "file:///" + filepath.ToSlash(transcriptPath),
		State:          StateRunning,
		StateDetail:    fmt.Sprintf("Executing task: %s", prompt),
		Status:         "RUNNING",
		StartTime:      time.Now(),
		cancel:         cancel,
	}
	m.subagents[id] = info
	eb := m.eventBus
	m.mu.Unlock()

	go func() {
		defer cancel()

		var workDir string
		cwd, _ := os.Getwd()
		workDir = cwd
		var wtCreated string

		if (workspace == WorkspaceBranch || workspace == WorkspaceShare) && m.wtCreator != nil {
			wtName := fmt.Sprintf("subagent-%s", id)
			wtPath, err := m.wtCreator(cwd, wtName)
			if err == nil && wtPath != "" {
				workDir = wtPath
				wtCreated = wtName
			}
		}

		var res string
		var err error
		if runner != nil {
			res, err = runner(subCtx, info)
		} else if m.defaultRunner != nil {
			res, err = m.defaultRunner(subCtx, info, workDir)
		} else {
			time.Sleep(50 * time.Millisecond)
			res = fmt.Sprintf("Subagent [%s] finished task (workspace: %s).", role, workDir)
		}

		if wtCreated != "" && err == nil && m.wtRemover != nil {
			_ = m.wtRemover(cwd, wtCreated)
		}

		m.mu.Lock()
		info.mu.Lock()
		info.EndTime = time.Now()
		if err != nil {
			if subCtx.Err() == context.Canceled {
				info.Status = "KILLED"
				info.State = StateKilled
				info.StateDetail = "Canceled by user"
			} else {
				info.Status = "FAILED"
				info.State = StateErrored
				info.StateDetail = err.Error()
				info.Result = fmt.Sprintf("Error: %v", err)
			}
		} else {
			info.Status = "COMPLETED"
			info.State = StateCompleted
			info.StateDetail = "Completed successfully"
			info.Result = res
		}
		status := info.Status
		resultMsg := info.Result
		info.mu.Unlock()
		m.mu.Unlock()

		if eb != nil {
			eb.Publish("subagent_responses", fmt.Sprintf("Subagent [%s] (%s) %s: %s", id, role, status, resultMsg))
		}
	}()

	return info.Snapshot(), nil
}

func (m *Manager) List() []*SubagentInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make([]*SubagentInfo, 0, len(m.subagents))
	for _, info := range m.subagents {
		result = append(result, info.Snapshot())
	}
	return result
}

func (m *Manager) Get(id string) (*SubagentInfo, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	info, exists := m.subagents[id]
	if !exists {
		for _, sa := range m.subagents {
			if sa.ConversationID == id {
				info = sa
				exists = true
				break
			}
		}
	}
	if !exists {
		return nil, false
	}
	return info.Snapshot(), true
}

func (m *Manager) Kill(id string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	info, exists := m.subagents[id]
	if !exists {
		// check by ConversationID as well
		for _, sa := range m.subagents {
			if sa.ConversationID == id {
				info = sa
				exists = true
				break
			}
		}
		if !exists {
			return false
		}
	}
	if info.cancel != nil {
		info.cancel()
	}
	info.mu.Lock()
	info.Status = "KILLED"
	info.State = StateKilled
	info.StateDetail = "Canceled by manager"
	info.EndTime = time.Now()
	info.mu.Unlock()
	return true
}

func (m *Manager) KillAll() int {
	m.mu.Lock()
	defer m.mu.Unlock()

	count := 0
	for _, info := range m.subagents {
		info.mu.Lock()
		isRunning := info.Status == "RUNNING" || info.State == StateRunning
		if isRunning {
			if info.cancel != nil {
				info.cancel()
			}
			info.Status = "KILLED"
			info.State = StateKilled
			info.StateDetail = "Terminated during kill_all"
			info.EndTime = time.Now()
			count++
		}
		info.mu.Unlock()
	}
	return count
}

func (m *Manager) SendMessage(senderID, receiverID, content string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.subagents[receiverID]; !exists && receiverID != "*" {
		// Check by ConversationID
		found := false
		for k, sa := range m.subagents {
			if sa.ConversationID == receiverID {
				receiverID = k
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("receiver subagent [%s] not found", receiverID)
		}
	}

	msg := Message{
		SenderID:   senderID,
		ReceiverID: receiverID,
		Content:    content,
		Timestamp:  time.Now(),
	}

	if receiverID == "*" {
		for targetID := range m.subagents {
			if targetID != senderID {
				m.messages[targetID] = append(m.messages[targetID], msg)
			}
		}
	} else {
		m.messages[receiverID] = append(m.messages[receiverID], msg)
	}

	if m.eventBus != nil {
		m.eventBus.Publish("subagent_messages", fmt.Sprintf("[%s -> %s]: %s", senderID, receiverID, content))
	}
	return nil
}

func (m *Manager) GetMessages(subagentID string) []Message {
	m.mu.Lock()
	defer m.mu.Unlock()

	msgs := m.messages[subagentID]
	m.messages[subagentID] = nil // consume messages
	return msgs
}

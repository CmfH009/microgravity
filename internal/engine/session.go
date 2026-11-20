package engine

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/purav/microgravity/internal/cloud"
	"github.com/purav/microgravity/internal/config"
	"github.com/purav/microgravity/internal/events"
	"github.com/purav/microgravity/internal/executor"
	"github.com/purav/microgravity/internal/git"
	"github.com/purav/microgravity/internal/history"
	"github.com/purav/microgravity/internal/mcp"
	"github.com/purav/microgravity/internal/memory"
	"github.com/purav/microgravity/internal/ollama"
	"github.com/purav/microgravity/internal/plan"
	"github.com/purav/microgravity/internal/router"
	"github.com/purav/microgravity/internal/rules"
	"github.com/purav/microgravity/internal/scheduler"
	"github.com/purav/microgravity/internal/skills"
	"github.com/purav/microgravity/internal/tools"
	"github.com/purav/microgravity/internal/trajectory"
	"github.com/purav/microgravity/internal/verifier"
)

// DefaultCoreMemory is the resilient fallback XML block for Letta core memory when
// the Python sidecar is slow, times out, or unavailable under heavy parallel load.
const DefaultCoreMemory = `<core_memory>
  <human>
    Primary developer and workspace operator.
  </human>
  <persona>
    Microgravity autonomous engineering agent equipped with Gemma inference, Letta hierarchical memory, and Browser-Use tools.
  </persona>
  <scratchpad>
    Ready for active tasks.
  </scratchpad>
</core_memory>`

var (
	lettaCompiledCacheMu sync.RWMutex
	lettaCompiledCache   string
)

// ResetLettaMemoryCache clears the cached compiled Letta core memory.
func ResetLettaMemoryCache() {
	lettaCompiledCacheMu.Lock()
	lettaCompiledCache = ""
	lettaCompiledCacheMu.Unlock()
}

type ExecutionState string

const (
	StateIdle           ExecutionState = "idle"
	StateThinking       ExecutionState = "thinking"
	StateExecutingTools ExecutionState = "executing_tools"
	StateVerifying      ExecutionState = "verifying"
	StateComplete       ExecutionState = "complete"
	StateError          ExecutionState = "error"
)

// SessionConfig provides the initial configuration for an agent runtime session.
type SessionConfig struct {
	SessionName      string
	ActiveModel      string
	LocalModelName   string
	CloudModelName   string
	OllamaBaseURL    string
	Cwd              string
	Effort           string
	Mode             string
	AutoApprove      bool
	DisableSlash     bool
	CavemanLevel     string
	Registry         *tools.Registry
	MCPClients       []*mcp.Client
	MCPRouter        map[string]*mcp.Client
	EventBus         *events.EventBus
	Scheduler        *scheduler.Scheduler
	NotifyChan       chan string
	LoadedSkills     []*skills.Skill
	LoadedRules      *rules.Rules
	Executor         *executor.Executor
	ExplicitProvider string
	CoreMemory       string
	MemoryStore      *memory.MemoryStore
}

// AsyncEvent represents an asynchronous alert from subagents, background tasks, or schedulers.
type AsyncEvent struct {
	Topic   string
	Payload string
}

// Session coordinates the multi-turn agent loop, context assembly, and tool dispatching.
type Session struct {
	mu            sync.RWMutex
	Config        SessionConfig
	State         ExecutionState
	Conversation  strings.Builder
	Messages      []Message
	ActivePlan    *plan.Plan
	TrajLogger    *trajectory.Logger
	OllamaClient  *ollama.Client
	CloudClient   *cloud.Client
	Router        *router.Router
	SubCh         <-chan string
	SubID         int
	AsyncEvents   chan AsyncEvent
	subIDs        map[string]int
	CheckpointMgr *git.CheckpointManager
	Verifier      *verifier.Verifier
}

// NewSession constructs a fully initialized agent execution session.
func NewSession(cfg SessionConfig) (*Session, error) {
	if cfg.SessionName == "" {
		cfg.SessionName = "default"
	}
	if cfg.Cwd == "" {
		cfg.Cwd, _ = os.Getwd()
	}
	if cfg.LocalModelName == "" {
		cfg.LocalModelName = "gemma:2b"
	}
	if cfg.CloudModelName == "" {
		cfg.CloudModelName = "gemini-3.6-flash"
	}
	if cfg.ActiveModel == "" {
		cfg.ActiveModel = config.GetActiveModel()
		if cfg.ActiveModel == "" {
			cfg.ActiveModel = cfg.CloudModelName
		}
	}
	if cfg.Registry == nil {
		cfg.Registry = tools.NewRegistry()
	}
	if cfg.MCPRouter == nil {
		cfg.MCPRouter = make(map[string]*mcp.Client)
	}
	if cfg.Executor == nil {
		cfg.Executor = executor.New(cfg.Registry, cfg.MCPRouter)
	}
	if cfg.OllamaBaseURL == "" {
		cfg.OllamaBaseURL = "http://localhost:11434"
	}

	trajLogger, _ := trajectory.NewLogger(cfg.SessionName)

	s := &Session{
		Config:        cfg,
		State:         StateIdle,
		TrajLogger:    trajLogger,
		OllamaClient:  ollama.NewClient(cfg.OllamaBaseURL),
		CloudClient:   cloud.NewClient(),
		Router:        router.NewRouter(),
		CheckpointMgr: git.NewCheckpointManager(cfg.Cwd, cfg.SessionName),
		Verifier:      verifier.New(),
	}

	s.AsyncEvents = make(chan AsyncEvent, 100)
	if cfg.EventBus != nil {
		s.subIDs = make(map[string]int)
		for _, topic := range []string{"subagent_responses", "task_completed", "scheduler_alerts"} {
			t := topic
			ch, id := cfg.EventBus.Subscribe(t)
			s.subIDs[t] = id
			go func(c <-chan string, top string) {
				for msg := range c {
					s.AsyncEvents <- AsyncEvent{Topic: top, Payload: msg}
				}
			}(ch, t)
		}
		s.SubCh, s.SubID = cfg.EventBus.Subscribe("subagent_responses")
	}

	return s, nil
}

// CreateCheckpoint captures a shadow git snapshot of the current working tree.
func (s *Session) CreateCheckpoint(turn int, message string) (*git.Checkpoint, error) {
	if s.CheckpointMgr == nil {
		return nil, nil
	}
	return s.CheckpointMgr.CreateCheckpoint(turn, message)
}

// Rewind restores the working tree to the most recent checkpoint.
func (s *Session) Rewind() (*git.Checkpoint, error) {
	if s.CheckpointMgr == nil {
		return nil, fmt.Errorf("checkpoint manager not initialized")
	}
	return s.CheckpointMgr.Rewind()
}

// Close cleans up event bus subscriptions and resources.
func (s *Session) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.Config.EventBus != nil {
		if s.SubCh != nil {
			s.Config.EventBus.Unsubscribe("subagent_responses", s.SubID)
			s.SubCh = nil
		}
		for topic, id := range s.subIDs {
			s.Config.EventBus.Unsubscribe(topic, id)
		}
		s.subIDs = nil
	}
}

// GetState returns the current execution state thread-safely.
func (s *Session) GetState() ExecutionState {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.State
}

// SetState updates the execution state thread-safely and publishes state transitions.
func (s *Session) SetState(st ExecutionState) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.State = st
	if s.Config.EventBus != nil {
		s.Config.EventBus.Publish("session:state", string(st))
	}
}

// AddMessage appends a message to the structured message list and synchronizes the conversation buffer.
func (s *Session) AddMessage(msg Message) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.Messages) == 0 && msg.Role == RoleAssistant {
		s.Conversation.WriteString("\nAgent: ")
	} else if len(s.Messages) > 0 && s.Messages[len(s.Messages)-1].Role == RoleAssistant && msg.Role == RoleAssistant {
		s.Conversation.WriteString("\nAgent: ")
	}
	s.Messages = append(s.Messages, msg)
	s.Conversation.WriteString(msg.FormatText())
}

// GetMessages returns a safe copy of the structured message history.
// If Messages is empty but Conversation has content, it parses messages from the text.
func (s *Session) GetMessages() []Message {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if len(s.Messages) == 0 && s.Conversation.Len() > 0 {
		return ParseMessagesFromText(s.Conversation.String())
	}
	res := make([]Message, len(s.Messages))
	copy(res, s.Messages)
	return res
}

// SetMessages overwrites the structured messages and synchronizes the conversation buffer.
func (s *Session) SetMessages(msgs []Message) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Messages = make([]Message, len(msgs))
	copy(s.Messages, msgs)
	s.Conversation.Reset()
	s.Conversation.WriteString(FormatMessages(s.Messages))
}

// SyncConversation updates Conversation to mirror s.Messages.
func (s *Session) SyncConversation() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Conversation.Reset()
	s.Conversation.WriteString(FormatMessages(s.Messages))
}

func (s *Session) getAvailableToolsLocked() []mcp.Tool {
	var availableTools []mcp.Tool
	if s.Config.Registry != nil {
		availableTools = append(availableTools, s.Config.Registry.GetNativeTools()...)
	}
	for _, client := range s.Config.MCPClients {
		if client != nil {
			tList, _ := client.ListTools()
			availableTools = append(availableTools, tList...)
		}
	}
	return availableTools
}

// GetAvailableTools returns all registered native and MCP tools.
func (s *Session) GetAvailableTools() []mcp.Tool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.getAvailableToolsLocked()
}

// LoadSavedHistory loads persistent history into the session.
func (s *Session) LoadSavedHistory() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	saved, err := history.LoadHistory()
	if err == nil && saved != "" {
		s.Conversation.Reset()
		s.Conversation.WriteString(saved)
		s.Messages = ParseMessagesFromText(saved)
	}
	return err
}

// SaveHistory persists the current conversation to history.
func (s *Session) SaveHistory() error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return history.SaveHistory(s.Conversation.String())
}

// ClearHistory resets the conversation buffer and clears disk history.
func (s *Session) ClearHistory() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Conversation.Reset()
	s.Messages = nil
	history.ClearHistory()
}

// BuildSystemPrompt compiles directives, rules, tools, and skills into the active system instruction.
func (s *Session) BuildSystemPrompt() string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Current System Date and Time: %s\n", time.Now().Format("Monday, January 2, 2006 15:04:05 MST")))

	if s.Config.LoadedRules != nil {
		if formatted := s.Config.LoadedRules.FormatForSystemPrompt(); formatted != "" {
			sb.WriteString(formatted + "\n")
		}
	}

	if s.Config.CavemanLevel != "" && s.Config.CavemanLevel != "off" {
		sb.WriteString(fmt.Sprintf("\n[CAVEMAN MODE ACTIVE - Level: %s]: Respond terse like smart caveman. Substance stay. Fluff die. Drop pleasantries, filler, hedging, and articles. Technical code blocks, commands, and diffs 100%% verbatim exact. No artificial caricature grunts. Maximum token density.\n", s.Config.CavemanLevel))
	}

	if s.Config.Mode == "plan" {
		sb.WriteString("\n[PLAN MODE ENFORCED]: You must create a detailed Markdown implementation plan with tasks and checkboxes before modifying code.\n")
	}

	// Inject Letta Core Memory blocks (<core_memory> containing <human>, <persona>, <scratchpad>)
	var coreMem string
	if s.Config.CoreMemory != "" {
		coreMem = s.Config.CoreMemory
	} else {
		lettaCompiledCacheMu.RLock()
		cached := lettaCompiledCache
		lettaCompiledCacheMu.RUnlock()

		if cached != "" {
			coreMem = cached
		} else {
			lettaCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			coreMem = tools.GetCompiledLettaMemory(lettaCtx)
			cancel()
			if coreMem == "" {
				coreMem = DefaultCoreMemory
			}
			lettaCompiledCacheMu.Lock()
			lettaCompiledCache = coreMem
			lettaCompiledCacheMu.Unlock()
		}
	}
	if coreMem != "" {
		sb.WriteString("\n" + strings.TrimSpace(coreMem) + "\n")
	}

	availableTools := s.getAvailableToolsLocked()

	var promptSkills []string
	for i, sk := range s.Config.LoadedSkills {
		if sk.Type != "tool" {
			promptSkills = append(promptSkills, fmt.Sprintf("- %s: %s", sk.Name, sk.Description))
		}
		if i >= 50 {
			promptSkills = append(promptSkills, fmt.Sprintf("... and %d more skills (omitted to save context)", len(s.Config.LoadedSkills)-50))
			break
		}
	}

	if len(availableTools) > 0 || len(promptSkills) > 0 {
		sb.WriteString("System: You are an autonomous AI agent.\n")
		if len(promptSkills) > 0 {
			sb.WriteString("You have access to the following skills and domain knowledge:\n" + strings.Join(promptSkills, "\n") + "\n")
		}
		if len(availableTools) > 0 {
			sb.WriteString("You have access to the following tools:\n")
			for _, t := range availableTools {
				sb.WriteString(fmt.Sprintf("- %s: %s\n", t.Name, t.Description))
			}
			sb.WriteString("To call one or more tools, you MUST output a JSON block formatted like:\n```json\n{\n  \"tool_call\": \"tool_name\",\n  \"arguments\": {\"arg1\": \"value\"}\n}\n```\n")
			sb.WriteString("Or an array for multiple tool calls in a single turn:\n```json\n[\n  {\"tool_call\": \"tool1\", \"arguments\": {}},\n  {\"tool_call\": \"tool2\", \"arguments\": {}}\n]\n```\n")
			sb.WriteString("CRITICAL: If the user asks about current events, dates, time, weather, or web facts, YOU MUST CALL A TOOL. DO NOT GUESS OR GENERATE PLACEHOLDER LINKS.\n")
		}
	}

	return sb.String()
}

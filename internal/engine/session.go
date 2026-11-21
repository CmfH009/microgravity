package engine

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/CmfH009/microgravity/internal/cloud"
	"github.com/CmfH009/microgravity/internal/config"
	"github.com/CmfH009/microgravity/internal/events"
	"github.com/CmfH009/microgravity/internal/executor"
	"github.com/CmfH009/microgravity/internal/git"
	"github.com/CmfH009/microgravity/internal/history"
	"github.com/CmfH009/microgravity/internal/mcp"
	"github.com/CmfH009/microgravity/internal/memory"
	"github.com/CmfH009/microgravity/internal/ollama"
	"github.com/CmfH009/microgravity/internal/openai"
	"github.com/CmfH009/microgravity/internal/plan"
	"github.com/CmfH009/microgravity/internal/router"
	"github.com/CmfH009/microgravity/internal/rules"
	"github.com/CmfH009/microgravity/internal/scheduler"
	"github.com/CmfH009/microgravity/internal/skills"
	"github.com/CmfH009/microgravity/internal/tools"
	"github.com/CmfH009/microgravity/internal/trajectory"
	"github.com/CmfH009/microgravity/internal/verifier"
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
	MaxTurns         int
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
	RoutingMode      string
	LocalFastModel   string
	LocalHeavyModel  string
	OpenAIBaseURL    string
	OpenAIAPIKey     string
	OpenAIModel      string
	CoreMemory       string
	MemoryStore      *memory.MemoryStore
	Router           *router.Router
	Providers        router.Providers
}

// AsyncEvent represents an asynchronous alert from subagents, background tasks, or schedulers.
type AsyncEvent struct {
	Topic   string
	Payload string
}

// Session coordinates the multi-turn agent loop, context assembly, and tool dispatching.
type Session struct {
	mu            sync.RWMutex
	ckptMu        sync.Mutex
	Config        SessionConfig
	State         ExecutionState
	Conversation  strings.Builder
	Messages      []Message
	ActivePlan    *plan.Plan
	TrajLogger    *trajectory.Logger
	OllamaClient  *ollama.Client
	OpenAIClient  *openai.Client
	CloudClient   *cloud.Client
	Router        *router.Router
	SubCh         <-chan string
	SubID         int
	AsyncEvents   chan AsyncEvent
	subIDs        map[string]int
	closeCh       chan struct{}
	closed        bool
	subWg         sync.WaitGroup
	CheckpointMgr *git.CheckpointManager
	Verifier      *verifier.Verifier
}

// NewSession constructs a fully initialized agent execution session.
func NewSession(cfg SessionConfig) (*Session, error) {
	if cfg.SessionName == "" {
		cfg.SessionName = "default"
	}
	if cfg.Cwd == "" {
		cwd, err := os.Getwd()
		if err != nil {
			cwd = "."
		}
		cfg.Cwd = cwd
	}
	if cfg.LocalModelName == "" {
		if cfg.LocalFastModel != "" {
			cfg.LocalModelName = cfg.LocalFastModel
		} else {
			cfg.LocalModelName = "gemma:2b"
		}
	}
	if cfg.RoutingMode == "" {
		if envMode, ok := router.RoutingModeFromEnv(); ok {
			cfg.RoutingMode = string(envMode)
		} else {
			cfg.RoutingMode = string(router.ModeHybrid)
		}
	} else {
		cfg.RoutingMode = string(router.NormalizeRoutingMode(cfg.RoutingMode))
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

	ollamaClient := ollama.NewClient(cfg.OllamaBaseURL)
	openAIClient := openai.NewClient(cfg.OpenAIBaseURL, cfg.OpenAIAPIKey)
	cloudClient := cloud.NewClient()

	providers := cfg.Providers
	if providers.Ollama == nil {
		providers.Ollama = ollamaClient
	}
	if providers.OpenAI == nil {
		providers.OpenAI = openAIClient
	}
	if providers.Cloud == nil {
		providers.Cloud = cloudClient
	}

	routerInst := cfg.Router
	if routerInst == nil {
		routerInst = router.NewWithProviders(providers)
	} else if routerInst.GetProviders() == (router.Providers{}) {
		routerInst.SetProviders(providers)
	}

	s := &Session{
		Config:        cfg,
		State:         StateIdle,
		TrajLogger:    trajLogger,
		OllamaClient:  ollamaClient,
		OpenAIClient:  openAIClient,
		CloudClient:   cloudClient,
		Router:        routerInst,
		closeCh:       make(chan struct{}),
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
			s.subWg.Add(1)
			go func(c <-chan string, top string) {
				defer s.subWg.Done()
				for {
					select {
					case msg, ok := <-c:
						if !ok {
							return
						}
						select {
						case s.AsyncEvents <- AsyncEvent{Topic: top, Payload: msg}:
						case <-s.closeCh:
							return
						}
					case <-s.closeCh:
						return
					}
				}
			}(ch, t)
		}
		s.SubCh, s.SubID = cfg.EventBus.Subscribe("subagent_responses")
	}

	return s, nil
}

// CreateCheckpoint captures a shadow git snapshot of the current working tree.
func (s *Session) CreateCheckpoint(turn int, message string) (*git.Checkpoint, error) {
	if s == nil || s.CheckpointMgr == nil {
		return nil, nil
	}
	s.ckptMu.Lock()
	defer s.ckptMu.Unlock()
	return s.CheckpointMgr.CreateCheckpoint(turn, message)
}

// Rewind restores the working tree to the most recent checkpoint.
func (s *Session) Rewind() (*git.Checkpoint, error) {
	if s == nil || s.CheckpointMgr == nil {
		return nil, fmt.Errorf("checkpoint manager not initialized")
	}
	s.ckptMu.Lock()
	defer s.ckptMu.Unlock()
	return s.CheckpointMgr.Rewind()
}

// Close cleans up event bus subscriptions and resources idempotently.
func (s *Session) Close() {
	if s == nil {
		return
	}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.closed = true
	if s.closeCh != nil {
		close(s.closeCh)
	}
	eb := s.Config.EventBus
	subCh := s.SubCh
	subID := s.SubID
	subIDs := s.subIDs
	s.SubCh = nil
	s.subIDs = nil
	s.mu.Unlock()

	if eb != nil {
		if subCh != nil {
			eb.Unsubscribe("subagent_responses", subID)
		}
		for topic, id := range subIDs {
			eb.Unsubscribe(topic, id)
		}
	}
	s.subWg.Wait()
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

// SetRoutingMode updates the active routing mode thread-safely.
func (s *Session) SetRoutingMode(mode string) {
	if s == nil {
		return
	}
	norm := string(router.NormalizeRoutingMode(mode))
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Config.RoutingMode = norm
	if norm == string(router.ModeLocalOnly) && s.Config.ExplicitProvider == "cloud" {
		s.Config.ExplicitProvider = ""
	}
}

// GetRoutingMode returns the effective routing mode thread-safely.
func (s *Session) GetRoutingMode() router.RoutingMode {
	if s == nil {
		return router.ModeHybrid
	}
	s.mu.RLock()
	raw := s.Config.RoutingMode
	s.mu.RUnlock()
	if strings.TrimSpace(raw) == "" {
		if envMode, ok := router.RoutingModeFromEnv(); ok {
			return envMode
		}
		return router.ModeHybrid
	}
	return router.NormalizeRoutingMode(raw)
}

func (s *Session) routePolicyLocked() router.RoutePolicy {
	var mode router.RoutingMode
	if strings.TrimSpace(s.Config.RoutingMode) != "" {
		mode = router.NormalizeRoutingMode(s.Config.RoutingMode)
	} else if envMode, ok := router.RoutingModeFromEnv(); ok {
		mode = envMode
	} else {
		mode = router.ModeHybrid
	}
	localModel := s.Config.LocalModelName
	if localModel == "" {
		localModel = router.DefaultLocalModel
	}
	cloudModel := s.Config.CloudModelName
	if cloudModel == "" {
		cloudModel = router.DefaultCloudModel
	}
	activeModel := s.Config.ActiveModel
	if activeModel == "" {
		activeModel = cloudModel
	}
	return router.RoutePolicy{
		RoutingMode:      mode,
		ExplicitProvider: s.Config.ExplicitProvider,
		ActiveModel:      activeModel,
		LocalModel:       localModel,
		LocalFastModel:   s.Config.LocalFastModel,
		LocalHeavyModel:  s.Config.LocalHeavyModel,
		CloudModel:       cloudModel,
		OpenAIBaseURL:    s.Config.OpenAIBaseURL,
		OpenAIModel:      s.Config.OpenAIModel,
	}
}

// RoutePolicy returns a thread-safe snapshot of the session's active routing policy.
func (s *Session) RoutePolicy() router.RoutePolicy {
	if s == nil {
		return router.RoutePolicy{RoutingMode: router.ModeHybrid}
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.routePolicyLocked()
}

// IsLocalRoute reports whether the session currently routes primarily to a local inference backend.
func (s *Session) IsLocalRoute() bool {
	if s == nil {
		return false
	}
	p := s.RoutePolicy()
	return router.IsLocalRoute(p.ExplicitProvider, string(p.RoutingMode), p.OpenAIBaseURL, p.ActiveModel, p.LocalModel)
}

func cloneMessage(m Message) Message {
	out := m
	if len(m.Images) > 0 {
		out.Images = append([]string(nil), m.Images...)
	}
	if len(m.ToolCalls) > 0 {
		out.ToolCalls = make([]ToolCall, len(m.ToolCalls))
		for i, tc := range m.ToolCalls {
			out.ToolCalls[i] = tc
			if tc.Arguments != nil {
				args := make(map[string]interface{}, len(tc.Arguments))
				for k, v := range tc.Arguments {
					args[k] = v
				}
				out.ToolCalls[i].Arguments = args
			}
		}
	}
	if m.Metadata != nil {
		meta := make(map[string]string, len(m.Metadata))
		for k, v := range m.Metadata {
			meta[k] = v
		}
		out.Metadata = meta
	}
	return out
}

// AddMessage appends a message to the structured message list and synchronizes the conversation buffer.
func (s *Session) AddMessage(msg Message) {
	cloned := cloneMessage(msg)
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.Messages) == 0 && cloned.Role == RoleAssistant {
		s.Conversation.WriteString("\nAgent: ")
	} else if len(s.Messages) > 0 && s.Messages[len(s.Messages)-1].Role == RoleAssistant && cloned.Role == RoleAssistant {
		s.Conversation.WriteString("\nAgent: ")
	}
	s.Messages = append(s.Messages, cloned)
	s.Conversation.WriteString(cloned.FormatText())
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
	for i, m := range s.Messages {
		res[i] = cloneMessage(m)
	}
	return res
}

// SetMessages overwrites the structured messages and synchronizes the conversation buffer.
func (s *Session) SetMessages(msgs []Message) {
	cloned := make([]Message, len(msgs))
	for i, m := range msgs {
		cloned[i] = cloneMessage(m)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Messages = cloned
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
			if tList, err := client.ListTools(); err == nil {
				availableTools = append(availableTools, tList...)
			}
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

func resolveLettaCoreMemory(configured string) string {
	if configured != "" {
		return configured
	}
	lettaCompiledCacheMu.RLock()
	cached := lettaCompiledCache
	lettaCompiledCacheMu.RUnlock()
	if cached != "" {
		return cached
	}

	lettaCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	coreMem := tools.GetCompiledLettaMemory(lettaCtx)
	cancel()
	if coreMem == "" {
		coreMem = DefaultCoreMemory
	}
	lettaCompiledCacheMu.Lock()
	lettaCompiledCache = coreMem
	lettaCompiledCacheMu.Unlock()
	return coreMem
}

// BuildSystemPrompt compiles directives, rules, tools, and skills into the active system instruction.
func (s *Session) BuildSystemPrompt() string {
	s.mu.RLock()
	configuredCoreMem := s.Config.CoreMemory
	s.mu.RUnlock()

	// Resolve Letta memory without holding s.mu across potential 5s subprocess calls.
	coreMem := resolveLettaCoreMemory(configuredCoreMem)

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

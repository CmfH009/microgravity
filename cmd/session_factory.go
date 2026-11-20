package cmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/purav/microgravity/internal/cache"
	"github.com/purav/microgravity/internal/config"
	"github.com/purav/microgravity/internal/engine"
	"github.com/purav/microgravity/internal/events"
	"github.com/purav/microgravity/internal/executor"
	"github.com/purav/microgravity/internal/git"
	"github.com/purav/microgravity/internal/mcp"
	"github.com/purav/microgravity/internal/memory"
	"github.com/purav/microgravity/internal/policy"
	"github.com/purav/microgravity/internal/rules"
	"github.com/purav/microgravity/internal/sandbox"
	"github.com/purav/microgravity/internal/scheduler"
	"github.com/purav/microgravity/internal/skills"
	"github.com/purav/microgravity/internal/subagent"
	"github.com/purav/microgravity/internal/tools"
	"github.com/purav/microgravity/internal/web"
)

// SessionOptions encapsulates parameters to construct an engine.Session.
type SessionOptions struct {
	SessionName      string
	ActiveModel      string
	LocalModelName   string
	CloudModelName   string
	OllamaBaseURL    string
	Mode             string
	Effort           string
	AutoApprove      bool
	DisableSlash     bool
	CavemanLevel     string
	ExplicitProvider string
	MCPServers       []string
	NotifyChan       chan string
	AddDirs          []string
	Sandbox          bool
	Cwd              string
	MaxParallel      int
	DryRun           bool
}

// BuildEngineSession instantiates a fully equipped engine.Session with native tools,
// MCP servers, git, sandbox, and skills registered.
func BuildEngineSession(ctx context.Context, opts SessionOptions) (*engine.Session, func(), error) {
	cwd := opts.Cwd
	if cwd == "" {
		cwd, _ = os.Getwd()
	}

	mcpClients := make([]*mcp.Client, 0)
	mcpRouter := make(map[string]*mcp.Client)
	mcpNamedClients := make(map[string]*mcp.Client)

	allMCPServers := append([]string{}, opts.MCPServers...)
	allMCPServers = append(allMCPServers, config.GetMCPServers()...)

	if orbitCmd := config.DetectOrbitMCPServer(); orbitCmd != "" {
		hasOrbit := false
		for _, s := range allMCPServers {
			if strings.Contains(s, "orbit_mcp_server.py") {
				hasOrbit = true
				break
			}
		}
		if !hasOrbit {
			allMCPServers = append(allMCPServers, orbitCmd)
		}
	}

	for _, serverCmd := range allMCPServers {
		if strings.TrimSpace(serverCmd) == "" {
			continue
		}
		parts := strings.Split(serverCmd, " ")
		client := mcp.NewClient(parts[0], parts[1:])
		if err := client.Start(ctx); err != nil {
			continue
		}
		if err := client.Initialize(); err != nil {
			client.Stop()
			continue
		}
		tList, _ := client.ListTools()
		for _, t := range tList {
			mcpRouter[t.Name] = client
		}
		mcpClients = append(mcpClients, client)
		mcpNamedClients[serverCmd] = client
	}

	notifyChan := opts.NotifyChan
	if notifyChan == nil {
		notifyChan = make(chan string, 16)
		go func() {
			for msg := range notifyChan {
				_ = msg
			}
		}()
	}

	sched := scheduler.New()
	eb := events.NewEventBus()
	sched.SetEventBus(eb)

	// Connect git worktree isolation and child session execution to subagent manager
	subMgr := subagent.GetManager()
	subMgr.SetEventBus(eb)
	subMgr.SetWorktreeProvider(
		func(repoDir, name string) (string, error) {
			return git.CreateWorktree(repoDir, name)
		},
		func(repoDir, name string) error {
			return git.RemoveWorktree(repoDir, name)
		},
	)

	subMgr.SetDefaultRunner(func(ctx context.Context, sub *subagent.SubagentInfo, workDir string) (string, error) {
		childModel := sub.Model
		if childModel == "" || childModel == "inherit" {
			childModel = opts.ActiveModel
		}

		childOpts := SessionOptions{
			SessionName:      fmt.Sprintf("subagent-%s", sub.ID),
			ActiveModel:      childModel,
			LocalModelName:   opts.LocalModelName,
			CloudModelName:   opts.CloudModelName,
			OllamaBaseURL:    opts.OllamaBaseURL,
			Mode:             "agent",
			AutoApprove:      true,
			DisableSlash:     true,
			ExplicitProvider: opts.ExplicitProvider,
			AddDirs:          []string{workDir},
			Sandbox:          opts.Sandbox,
			Cwd:              workDir,
		}

		childSess, cleanup, err := BuildEngineSession(ctx, childOpts)
		if err != nil {
			return "", fmt.Errorf("failed to build child session: %w", err)
		}
		defer cleanup()

		childSess.Config.Cwd = workDir

		// Subagents execute the prompt through the full turn/tool execution loop
		res, err := childSess.RunPrompt(ctx, sub.Prompt, nil, io.Discard)
		if err != nil {
			return "", err
		}
		return res, nil
	})

	roots := append([]string{cwd, os.TempDir()}, opts.AddDirs...)
	pol := policy.New(roots, !opts.Sandbox, opts.Sandbox)

	// Apply fine-grained tool rules from environment (MG_POLICY_DENY_TOOLS,
	// MG_POLICY_ALLOW_TOOLS, MG_POLICY_DENY_PATHS).
	denyTools, allowTools, denyPaths := policy.ToolRulesFromEnv()
	pol.WithToolRules(denyTools, allowTools, denyPaths)

	registry := tools.NewRegistry()
	// Attach TTL result cache for tool memoization (MG_CACHE_TTL, MG_CACHE_ENABLED).
	if cache.Enabled() {
		registry.SetCache(cache.NewTTLCache(cache.DefaultTTL()))
	}
	// Attach policy for tool-level enforcement.
	registry.SetPolicy(pol)
	// Attach dry-run mode for previewing tool calls without executing.
	registry.SetDryRun(opts.DryRun)
	tools.RegisterBuiltinsWithPolicy(registry, eb, sched, notifyChan, nil, pol)
	tools.RegisterMCPResourceTools(registry, mcpNamedClients)
	git.RegisterGitTools(registry)
	web.RegisterWebTools(registry)
	sandbox.RegisterSandboxTools(registry)

	// Memory store for cross-session semantic memory
	home, _ := os.UserHomeDir()
	memStore, err := memory.NewMemoryStore(filepath.Join(home, ".microgravity", "memory.db"))
	if err != nil {
		// Non-fatal: memory store is optional
		fmt.Fprintf(os.Stderr, "[memory] warning: failed to open memory store: %v\n", err)
	}

	loadedSkills, _ := skills.LoadAll(config.GetSkillsDir())
	skills.RegisterTools(registry, loadedSkills)

	loadedRules, _ := rules.LoadDefaultRules(cwd)

	exec := executor.New(registry, mcpRouter)
	if opts.MaxParallel > 0 {
		exec.SetMaxParallel(opts.MaxParallel)
	}

	cfg := engine.SessionConfig{
		SessionName:      opts.SessionName,
		ActiveModel:      opts.ActiveModel,
		LocalModelName:   opts.LocalModelName,
		CloudModelName:   opts.CloudModelName,
		OllamaBaseURL:    opts.OllamaBaseURL,
		Cwd:              cwd,
		Effort:           opts.Effort,
		Mode:             opts.Mode,
		AutoApprove:      opts.AutoApprove,
		DisableSlash:     opts.DisableSlash,
		CavemanLevel:     opts.CavemanLevel,
		Registry:         registry,
		MCPClients:       mcpClients,
		MCPRouter:        mcpRouter,
		EventBus:         eb,
		Scheduler:        sched,
		NotifyChan:       notifyChan,
		LoadedSkills:     loadedSkills,
		LoadedRules:      loadedRules,
		Executor:         exec,
		ExplicitProvider: opts.ExplicitProvider,
		MemoryStore:      memStore,
	}

	sess, err := engine.NewSession(cfg)
	if err != nil {
		sched.Stop()
		for _, c := range mcpClients {
			c.Stop()
		}
		return nil, nil, err
	}

	cleanup := func() {
		sess.Close()
		sched.Stop()
		if memStore != nil {
			_ = memStore.Close()
		}
		for _, c := range mcpClients {
			c.Stop()
		}
	}

	return sess, cleanup, nil
}

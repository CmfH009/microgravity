package router

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"

	"github.com/CmfH009/microgravity/internal/cmdparse"
	"github.com/CmfH009/microgravity/internal/config"
	"github.com/CmfH009/microgravity/internal/mcp"
	"github.com/shirou/gopsutil/v3/mem"
)

type Complexity string

const (
	ComplexitySimple  Complexity = "simple"
	ComplexityComplex Complexity = "complex"
)

// RoutingMode controls how prompts are distributed across local and cloud providers.
type RoutingMode string

const (
	ModeLocalOnly RoutingMode = "local-only"
	ModeHybrid    RoutingMode = "hybrid"
	ModeCloudOnly RoutingMode = "cloud-only"
)

// Canonical provider identifiers and default model tiers.
const (
	ProviderOllama = "ollama"
	ProviderOpenAI = "openai"
	ProviderCloud  = "cloud"

	DefaultLocalModel = "gemma:2b"
	DefaultCloudModel = "gemini-3.6-flash"
)

// LocalTier identifies which local model tier handles a local-routed prompt.
type LocalTier string

const (
	TierLocalFast  LocalTier = "fast"
	TierLocalHeavy LocalTier = "heavy"
)

// GenerateResult holds the accumulated text and parsed tool calls returned by an inference Provider.
type GenerateResult struct {
	Text      string
	ToolCalls []cmdparse.ToolCall
}

// Provider is the port at the inference seam satisfied by ollama.Client,
// openai.Client, cloud.Client, and in-memory test adapters.
type Provider interface {
	GenerateWithTools(ctx context.Context, model, prompt string, images []string, tools []mcp.Tool, out io.Writer) (*GenerateResult, error)
}

// ProviderFunc adapts a function into a Provider.
type ProviderFunc func(ctx context.Context, model, prompt string, images []string, tools []mcp.Tool, out io.Writer) (*GenerateResult, error)

// GenerateWithTools implements Provider.
func (f ProviderFunc) GenerateWithTools(ctx context.Context, model, prompt string, images []string, tools []mcp.Tool, out io.Writer) (*GenerateResult, error) {
	return f(ctx, model, prompt, images, tools, out)
}

// Providers bundles the concrete or fake adapters for each backend.
type Providers struct {
	Ollama Provider
	OpenAI Provider
	Cloud  Provider
}

// RouteDecision describes the resolved provider, tier, model, and classification rationale.
type RouteDecision struct {
	Provider   string
	Tier       LocalTier
	Model      string
	Complexity Complexity
	Reason     string
}

// RoutePolicy captures the routing configuration for a turn.
type RoutePolicy struct {
	RoutingMode      RoutingMode
	ExplicitProvider string
	ActiveModel      string
	LocalModel       string
	LocalFastModel   string
	LocalHeavyModel  string
	CloudModel       string
	OpenAIBaseURL    string
	OpenAIModel      string
}

// DispatchStep represents a single provider+model attempt in an ordered failover plan.
type DispatchStep struct {
	Provider string
	Model    string
}

// RoutePlan contains the primary routing decision, the model to size context token
// budgets against, and the ordered failover steps to execute.
type RoutePlan struct {
	Decision    RouteDecision
	BudgetModel string
	Steps       []DispatchStep
}

// GenerateRequest encapsulates a single turn generation call across the router's failover chain.
type GenerateRequest struct {
	Prompt     string
	Images     []string
	Tools      []mcp.Tool
	Out        io.Writer
	OnFailover func(failed DispatchStep, next DispatchStep, err error)
}

// ConfigInput supplies CLI flags, environment, and persisted config for unified routing resolution.
type ConfigInput struct {
	Config           *config.Config
	LocalFlag        bool
	RoutingMode      string
	ModelFlag        string
	ActiveModel      string
	LocalModel       string
	CloudModel       string
	LocalFastModel   string
	LocalHeavyModel  string
	OpenAIBaseURL    string
	OpenAIAPIKey     string
	OpenAIModel      string
	ExplicitProvider string
}

// ResolvedConfig is the canonical, conflict-free routing configuration produced by ResolveConfig.
type ResolvedConfig struct {
	RoutingMode      RoutingMode
	ExplicitProvider string
	ActiveModel      string
	LocalModel       string
	LocalFastModel   string
	LocalHeavyModel  string
	CloudModel       string
	OpenAIBaseURL    string
	OpenAIAPIKey     string
	OpenAIModel      string
}

// SkipCloudAuth reports whether cloud authentication can be skipped under this configuration.
func (c ResolvedConfig) SkipCloudAuth() bool {
	if c.RoutingMode == ModeLocalOnly {
		return true
	}
	if strings.TrimSpace(c.OpenAIBaseURL) != "" {
		return true
	}
	if c.ExplicitProvider == ProviderOllama || c.ExplicitProvider == ProviderOpenAI {
		return true
	}
	if c.ActiveModel != "" && !IsCloudModel(c.ActiveModel) {
		return true
	}
	return false
}

// DisplayModel returns the primary human-facing model label for status banners.
func (c ResolvedConfig) DisplayModel() string {
	if strings.TrimSpace(c.OpenAIBaseURL) != "" && strings.TrimSpace(c.OpenAIModel) != "" {
		return c.OpenAIModel
	}
	if c.ExplicitProvider == ProviderOllama && strings.TrimSpace(c.LocalModel) != "" {
		return c.LocalModel
	}
	if c.RoutingMode == ModeLocalOnly {
		if strings.TrimSpace(c.LocalFastModel) != "" {
			return c.LocalFastModel
		}
		if strings.TrimSpace(c.LocalModel) != "" {
			return c.LocalModel
		}
	}
	if strings.TrimSpace(c.CloudModel) != "" {
		return c.CloudModel
	}
	return DefaultCloudModel
}

// RoutePolicy converts the resolved configuration into a per-turn RoutePolicy.
func (c ResolvedConfig) RoutePolicy() RoutePolicy {
	return RoutePolicy{
		RoutingMode:      c.RoutingMode,
		ExplicitProvider: c.ExplicitProvider,
		ActiveModel:      c.ActiveModel,
		LocalModel:       c.LocalModel,
		LocalFastModel:   c.LocalFastModel,
		LocalHeavyModel:  c.LocalHeavyModel,
		CloudModel:       c.CloudModel,
		OpenAIBaseURL:    c.OpenAIBaseURL,
		OpenAIModel:      c.OpenAIModel,
	}
}

// IsLocalRoute reports whether the resolved configuration routes primarily to a local backend.
func (c ResolvedConfig) IsLocalRoute() bool {
	return IsLocalRoute(c.ExplicitProvider, string(c.RoutingMode), c.OpenAIBaseURL, c.ActiveModel, c.LocalModel)
}

// IsCloudModel reports whether modelID identifies a cloud Gemini or cloud Gemma-2 model.
func IsCloudModel(modelID string) bool {
	lower := strings.ToLower(strings.TrimSpace(modelID))
	return strings.HasPrefix(lower, "gemini-") ||
		strings.HasPrefix(lower, "gemma-2-") ||
		lower == "flash" ||
		lower == "pro"
}

// IsLocalRoute reports whether the given provider/mode/model combination targets a local backend.
func IsLocalRoute(explicitProvider, routingMode, openAIBaseURL, activeModel, localModel string) bool {
	if strings.TrimSpace(openAIBaseURL) != "" {
		return true
	}
	if strings.TrimSpace(routingMode) != "" {
		switch NormalizeRoutingMode(routingMode) {
		case ModeLocalOnly:
			return true
		case ModeCloudOnly:
			return false
		}
	}
	switch strings.ToLower(strings.TrimSpace(explicitProvider)) {
	case ProviderOllama, ProviderOpenAI:
		return true
	case ProviderCloud:
		return false
	}
	active := strings.TrimSpace(activeModel)
	local := strings.TrimSpace(localModel)
	if active != "" && local != "" && strings.EqualFold(active, local) {
		return true
	}
	if active != "" {
		return !IsCloudModel(active)
	}
	return false
}

// ResolveRoutingMode resolves the effective RoutingMode from CLI flags, MG_ROUTING_MODE, and persisted config.
func ResolveRoutingMode(cfg *config.Config, localFlag bool, routingModeFlag string) RoutingMode {
	if localFlag {
		return ModeLocalOnly
	}
	if strings.TrimSpace(routingModeFlag) != "" {
		return NormalizeRoutingMode(routingModeFlag)
	}
	if envMode, ok := RoutingModeFromEnv(); ok {
		return envMode
	}
	if cfg != nil && strings.TrimSpace(cfg.RoutingMode) != "" {
		return NormalizeRoutingMode(cfg.RoutingMode)
	}
	return ModeHybrid
}

// ResolveOpenAIBaseURL resolves the OpenAI-compatible endpoint URL from flag, MG_OPENAI_BASE_URL, or config.
func ResolveOpenAIBaseURL(cfg *config.Config, baseURLFlag string) string {
	if strings.TrimSpace(baseURLFlag) != "" {
		return strings.TrimSpace(baseURLFlag)
	}
	if envURL := strings.TrimSpace(os.Getenv("MG_OPENAI_BASE_URL")); envURL != "" {
		return envURL
	}
	if cfg != nil && strings.TrimSpace(cfg.OpenAIBaseURL) != "" {
		return strings.TrimSpace(cfg.OpenAIBaseURL)
	}
	return ""
}

// ResolveOpenAIAPIKey resolves the OpenAI-compatible API key from explicit value, MG_OPENAI_API_KEY, or config.
func ResolveOpenAIAPIKey(cfg *config.Config, explicitKey string) string {
	if strings.TrimSpace(explicitKey) != "" {
		return strings.TrimSpace(explicitKey)
	}
	if envKey := strings.TrimSpace(os.Getenv("MG_OPENAI_API_KEY")); envKey != "" {
		return envKey
	}
	if cfg != nil && strings.TrimSpace(cfg.OpenAIAPIKey) != "" {
		return strings.TrimSpace(cfg.OpenAIAPIKey)
	}
	return ""
}

// ResolveLocalTierModels resolves the local fast and heavy tier models from flags, env, config, and fallback.
func ResolveLocalTierModels(cfg *config.Config, fastFlag, heavyFlag, fallbackLocal string) (string, string) {
	fast := strings.TrimSpace(fastFlag)
	if fast == "" {
		fast = strings.TrimSpace(os.Getenv("MG_LOCAL_FAST_MODEL"))
	}
	if fast == "" && cfg != nil {
		fast = strings.TrimSpace(cfg.LocalFastModel)
	}
	if fast == "" {
		fast = strings.TrimSpace(fallbackLocal)
	}

	heavy := strings.TrimSpace(heavyFlag)
	if heavy == "" {
		heavy = strings.TrimSpace(os.Getenv("MG_LOCAL_HEAVY_MODEL"))
	}
	if heavy == "" && cfg != nil {
		heavy = strings.TrimSpace(cfg.LocalHeavyModel)
	}
	return fast, heavy
}

// ResolveConfig consolidates CLI flags, environment variables, and persisted config into a single ResolvedConfig.
func ResolveConfig(in ConfigInput) ResolvedConfig {
	routingMode := ResolveRoutingMode(in.Config, in.LocalFlag, in.RoutingMode)
	openAIBaseURL := ResolveOpenAIBaseURL(in.Config, in.OpenAIBaseURL)
	openAIAPIKey := ResolveOpenAIAPIKey(in.Config, in.OpenAIAPIKey)

	modelFlag := strings.TrimSpace(in.ModelFlag)
	localModel := strings.TrimSpace(in.LocalModel)
	cloudModel := strings.TrimSpace(in.CloudModel)

	// Guard against callers accidentally passing a local model ID in CloudModel.
	if cloudModel != "" && !IsCloudModel(cloudModel) {
		if localModel == "" || localModel == DefaultLocalModel {
			localModel = cloudModel
		}
		cloudModel = DefaultCloudModel
	}
	if cloudModel == "" {
		cloudModel = DefaultCloudModel
	}

	activeModel := strings.TrimSpace(in.ActiveModel)
	if modelFlag != "" {
		activeModel = modelFlag
	} else if activeModel == "" && in.Config != nil && strings.TrimSpace(in.Config.ActiveModel) != "" {
		activeModel = strings.TrimSpace(in.Config.ActiveModel)
	}

	if activeModel != "" {
		if IsCloudModel(activeModel) {
			cloudModel = activeModel
		} else {
			localModel = activeModel
		}
	}

	localFast, localHeavy := ResolveLocalTierModels(in.Config, in.LocalFastModel, in.LocalHeavyModel, localModel)
	if localFast != "" && (localModel == "" || localModel == DefaultLocalModel) {
		localModel = localFast
	}
	if localModel == "" {
		localModel = DefaultLocalModel
	}
	if localFast == "" {
		localFast = localModel
	}
	if activeModel == "" {
		activeModel = cloudModel
	}

	openAIModel := strings.TrimSpace(in.OpenAIModel)
	if openAIModel == "" && in.Config != nil {
		openAIModel = strings.TrimSpace(in.Config.OpenAIModel)
	}
	if openAIBaseURL != "" && modelFlag != "" {
		openAIModel = modelFlag
	}

	explicit := strings.ToLower(strings.TrimSpace(in.ExplicitProvider))
	if openAIBaseURL != "" {
		explicit = ProviderOpenAI
	} else if routingMode == ModeLocalOnly {
		if explicit == ProviderCloud {
			explicit = ""
		}
	} else if explicit == "" {
		if modelFlag != "" {
			if IsCloudModel(modelFlag) {
				explicit = ProviderCloud
			} else {
				explicit = ProviderOllama
			}
		} else if activeModel != "" && !IsCloudModel(activeModel) {
			explicit = ProviderOllama
		}
	}

	return ResolvedConfig{
		RoutingMode:      routingMode,
		ExplicitProvider: explicit,
		ActiveModel:      activeModel,
		LocalModel:       localModel,
		LocalFastModel:   localFast,
		LocalHeavyModel:  localHeavy,
		CloudModel:       cloudModel,
		OpenAIBaseURL:    openAIBaseURL,
		OpenAIAPIKey:     openAIAPIKey,
		OpenAIModel:      openAIModel,
	}
}

// Router classifies prompts, plans token-budget & failover routes, and dispatches across inference providers.
type Router struct {
	mu             sync.RWMutex
	providers      Providers
	memUsedPercent func() (float64, error)
}

// NewRouter creates a Router with default system memory probing.
func NewRouter() *Router {
	return &Router{
		memUsedPercent: defaultMemUsedPercent,
	}
}

// NewWithProviders creates a Router wired with the given inference Provider adapters.
func NewWithProviders(p Providers) *Router {
	return &Router{
		providers:      p,
		memUsedPercent: defaultMemUsedPercent,
	}
}

// SetProviders updates the Provider adapters used by the Router thread-safely.
func (r *Router) SetProviders(p Providers) {
	if r == nil {
		return
	}
	r.mu.Lock()
	r.providers = p
	r.mu.Unlock()
}

// GetProviders returns the currently configured Provider adapters thread-safely.
func (r *Router) GetProviders() Providers {
	if r == nil {
		return Providers{}
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.providers
}

func defaultMemUsedPercent() (float64, error) {
	v, err := mem.VirtualMemory()
	if err != nil {
		return 0, err
	}
	return v.UsedPercent, nil
}

func (r *Router) memUsage() (float64, error) {
	if r != nil && r.memUsedPercent != nil {
		return r.memUsedPercent()
	}
	return defaultMemUsedPercent()
}

// NormalizeRoutingMode canonicalizes a raw routing mode string into a valid RoutingMode,
// defaulting to ModeHybrid for empty or unrecognized inputs.
func NormalizeRoutingMode(raw string) RoutingMode {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "local-only", "local", "offline":
		return ModeLocalOnly
	case "cloud-only", "cloud":
		return ModeCloudOnly
	case "hybrid", "auto", "":
		return ModeHybrid
	default:
		return ModeHybrid
	}
}

// RoutingModeFromEnv reads MG_ROUTING_MODE from the environment if set.
func RoutingModeFromEnv() (RoutingMode, bool) {
	raw := strings.TrimSpace(os.Getenv("MG_ROUTING_MODE"))
	if raw == "" {
		return "", false
	}
	return NormalizeRoutingMode(raw), true
}

var complexKeywords = []string{
	"refactor", "design", "optimize", "architect", "architecture",
	"explain", "debug", "benchmark", "implement", "analyze",
	"search", "web", "today", "date", "time", "weather", "latest", "news", "current", "who", "day", "year",
}

func classifyPromptHeuristics(prompt string) (Complexity, string) {
	clean := strings.ToLower(strings.TrimSpace(prompt))
	if clean == "" {
		return ComplexitySimple, "empty prompt"
	}
	if len(clean) > 150 {
		return ComplexityComplex, "prompt length > 150 chars"
	}
	for _, kw := range complexKeywords {
		if strings.Contains(clean, kw) {
			return ComplexityComplex, fmt.Sprintf("complex intent keyword %q", kw)
		}
	}
	return ComplexitySimple, "simple prompt"
}

// Classify classifies a prompt to decide if it requires a local or cloud model.
func (r *Router) Classify(prompt string) Complexity {
	clean := strings.ToLower(strings.TrimSpace(prompt))
	if clean == "" {
		return ComplexitySimple
	}

	// Check System RAM first. If RAM is over 85%, route to Cloud to avoid crashing local Ollama.
	if usedPct, err := r.memUsage(); err == nil && usedPct > 85.0 {
		fmt.Printf("\n⚠️ High memory usage detected (%.1f%%). Routing to Cloud Model.\n", usedPct)
		return ComplexityComplex
	}

	complexity, _ := classifyPromptHeuristics(clean)
	return complexity
}

// SelectRoute chooses the provider, tier, and concrete model for a prompt under mode.
// When mode is empty, MG_ROUTING_MODE is consulted before defaulting to ModeHybrid.
func (r *Router) SelectRoute(prompt string, mode RoutingMode, localFast, localHeavy, cloudModel string) RouteDecision {
	resolvedMode := resolveModeOrEnv(mode)

	effectiveFast := strings.TrimSpace(localFast)
	effectiveHeavy := strings.TrimSpace(localHeavy)
	if effectiveFast == "" {
		effectiveFast = effectiveHeavy
	}
	if effectiveFast == "" {
		effectiveFast = DefaultLocalModel
	}
	if effectiveHeavy == "" {
		effectiveHeavy = effectiveFast
	}

	effectiveCloud := strings.TrimSpace(cloudModel)
	if effectiveCloud == "" {
		effectiveCloud = DefaultCloudModel
	}

	promptComplexity, promptReason := classifyPromptHeuristics(prompt)

	switch resolvedMode {
	case ModeLocalOnly:
		// Under ModeLocalOnly, NEVER route to cloud even for >150 chars, coding keywords, or >85% RAM.
		if promptComplexity == ComplexityComplex {
			return RouteDecision{
				Provider:   ProviderOllama,
				Tier:       TierLocalHeavy,
				Model:      effectiveHeavy,
				Complexity: ComplexityComplex,
				Reason:     "local-only mode: " + promptReason,
			}
		}
		return RouteDecision{
			Provider:   ProviderOllama,
			Tier:       TierLocalFast,
			Model:      effectiveFast,
			Complexity: ComplexitySimple,
			Reason:     "local-only mode: " + promptReason,
		}

	case ModeCloudOnly:
		return RouteDecision{
			Provider:   ProviderCloud,
			Model:      effectiveCloud,
			Complexity: promptComplexity,
			Reason:     "cloud-only mode: " + promptReason,
		}

	default: // ModeHybrid
		if usedPct, err := r.memUsage(); err == nil && usedPct > 85.0 {
			return RouteDecision{
				Provider:   ProviderCloud,
				Model:      effectiveCloud,
				Complexity: ComplexityComplex,
				Reason:     fmt.Sprintf("hybrid mode: high memory usage (%.1f%%)", usedPct),
			}
		}
		if promptComplexity == ComplexityComplex {
			return RouteDecision{
				Provider:   ProviderCloud,
				Model:      effectiveCloud,
				Complexity: ComplexityComplex,
				Reason:     "hybrid mode: " + promptReason,
			}
		}
		return RouteDecision{
			Provider:   ProviderOllama,
			Tier:       TierLocalFast,
			Model:      effectiveFast,
			Complexity: ComplexitySimple,
			Reason:     "hybrid mode: " + promptReason,
		}
	}
}

// SelectRoute chooses the provider, tier, and concrete model for a prompt using a default Router.
func SelectRoute(prompt string, mode RoutingMode, localFast, localHeavy, cloudModel string) RouteDecision {
	return NewRouter().SelectRoute(prompt, mode, localFast, localHeavy, cloudModel)
}

func resolveModeOrEnv(mode RoutingMode) RoutingMode {
	if strings.TrimSpace(string(mode)) == "" {
		if envMode, ok := RoutingModeFromEnv(); ok {
			return envMode
		}
		return ModeHybrid
	}
	return NormalizeRoutingMode(string(mode))
}

func selectOpenAIModel(policy RoutePolicy, decision RouteDecision) string {
	if m := strings.TrimSpace(policy.OpenAIModel); m != "" {
		return m
	}
	if decision.Complexity == ComplexityComplex && strings.TrimSpace(policy.LocalHeavyModel) != "" {
		return strings.TrimSpace(policy.LocalHeavyModel)
	}
	if strings.TrimSpace(policy.LocalFastModel) != "" {
		return strings.TrimSpace(policy.LocalFastModel)
	}
	if m := strings.TrimSpace(policy.ActiveModel); m != "" && !IsCloudModel(m) {
		return m
	}
	if decision.Model != "" && !IsCloudModel(decision.Model) {
		return decision.Model
	}
	if strings.TrimSpace(policy.LocalModel) != "" {
		return strings.TrimSpace(policy.LocalModel)
	}
	return DefaultLocalModel
}

// Plan computes the full RoutePlan for a prompt and policy: primary RouteDecision,
// token-budget model, and ordered failover Steps.
func (r *Router) Plan(prompt string, policy RoutePolicy) RoutePlan {
	mode := resolveModeOrEnv(policy.RoutingMode)

	localModel := strings.TrimSpace(policy.LocalModel)
	if localModel == "" {
		if strings.TrimSpace(policy.LocalFastModel) != "" {
			localModel = strings.TrimSpace(policy.LocalFastModel)
		} else {
			localModel = DefaultLocalModel
		}
	}
	effectiveFast := strings.TrimSpace(policy.LocalFastModel)
	if effectiveFast == "" {
		effectiveFast = localModel
	}
	localHeavy := strings.TrimSpace(policy.LocalHeavyModel)

	cloudModel := strings.TrimSpace(policy.CloudModel)
	if cloudModel == "" {
		cloudModel = DefaultCloudModel
	}
	activeModel := strings.TrimSpace(policy.ActiveModel)
	if activeModel == "" {
		activeModel = cloudModel
	}

	decision := r.SelectRoute(prompt, mode, effectiveFast, localHeavy, activeModel)

	// Local target model for Ollama steps (fast vs heavy based on prompt complexity).
	localTarget := effectiveFast
	if decision.Complexity == ComplexityComplex && localHeavy != "" {
		localTarget = localHeavy
	}
	if localTarget == "" {
		localTarget = localModel
	}

	explicit := strings.ToLower(strings.TrimSpace(policy.ExplicitProvider))

	// 1. OpenAI-compatible local backend never calls Cloud APIs.
	if explicit == ProviderOpenAI || strings.TrimSpace(policy.OpenAIBaseURL) != "" {
		target := selectOpenAIModel(policy, decision)
		decision.Provider = ProviderOpenAI
		decision.Model = target
		return RoutePlan{
			Decision:    decision,
			BudgetModel: target,
			Steps:       []DispatchStep{{Provider: ProviderOpenAI, Model: target}},
		}
	}

	// 2. Strict local-only mode never touches Cloud APIs.
	if mode == ModeLocalOnly {
		return RoutePlan{
			Decision:    decision,
			BudgetModel: localTarget,
			Steps:       []DispatchStep{{Provider: ProviderOllama, Model: localTarget}},
		}
	}

	// 3. Explicit provider overrides.
	switch explicit {
	case ProviderOllama:
		target := localModel
		if strings.TrimSpace(policy.LocalFastModel) != "" || strings.TrimSpace(policy.LocalHeavyModel) != "" {
			target = localTarget
		}
		decision.Provider = ProviderOllama
		decision.Model = target
		return RoutePlan{
			Decision:    decision,
			BudgetModel: target,
			Steps:       []DispatchStep{{Provider: ProviderOllama, Model: target}},
		}

	case ProviderCloud:
		steps := []DispatchStep{{Provider: ProviderCloud, Model: activeModel}}
		if mode != ModeCloudOnly {
			steps = append(steps, DispatchStep{Provider: ProviderOllama, Model: localModel})
		}
		decision.Provider = ProviderCloud
		decision.Model = activeModel
		return RoutePlan{
			Decision:    decision,
			BudgetModel: activeModel,
			Steps:       steps,
		}
	}

	// 4. Cloud-only mode never touches local Ollama.
	if mode == ModeCloudOnly {
		return RoutePlan{
			Decision:    decision,
			BudgetModel: activeModel,
			Steps:       []DispatchStep{{Provider: ProviderCloud, Model: activeModel}},
		}
	}

	// 5. Local ActiveModel in hybrid mode routes directly to Ollama without calling Cloud.
	if activeModel != "" && !IsCloudModel(activeModel) {
		decision.Provider = ProviderOllama
		decision.Model = activeModel
		return RoutePlan{
			Decision:    decision,
			BudgetModel: activeModel,
			Steps:       []DispatchStep{{Provider: ProviderOllama, Model: activeModel}},
		}
	}

	// 6. Automatic Hybrid Routing with single-hop failover.
	if decision.Complexity == ComplexitySimple {
		return RoutePlan{
			Decision:    decision,
			BudgetModel: localTarget,
			Steps: []DispatchStep{
				{Provider: ProviderOllama, Model: localTarget},
				{Provider: ProviderCloud, Model: activeModel},
			},
		}
	}

	return RoutePlan{
		Decision:    decision,
		BudgetModel: activeModel,
		Steps: []DispatchStep{
			{Provider: ProviderCloud, Model: activeModel},
			{Provider: ProviderOllama, Model: localTarget},
		},
	}
}

func (r *Router) providerFor(name string) Provider {
	provs := r.GetProviders()
	switch name {
	case ProviderOllama:
		return provs.Ollama
	case ProviderOpenAI:
		return provs.OpenAI
	case ProviderCloud:
		return provs.Cloud
	default:
		return nil
	}
}

// Generate executes a RoutePlan across the configured Provider adapters with failover.
// Context cancellation aborts immediately without failing over to fallback providers.
func (r *Router) Generate(ctx context.Context, plan RoutePlan, req GenerateRequest) (*GenerateResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(plan.Steps) == 0 {
		return nil, fmt.Errorf("route plan contains no dispatch steps")
	}

	type stepFailure struct {
		step DispatchStep
		err  error
	}
	var failures []stepFailure

	for i, step := range plan.Steps {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		prov := r.providerFor(step.Provider)
		if prov == nil {
			err := fmt.Errorf("%s client not initialized", step.Provider)
			failures = append(failures, stepFailure{step: step, err: err})
			if i+1 < len(plan.Steps) && req.OnFailover != nil {
				req.OnFailover(step, plan.Steps[i+1], err)
			}
			continue
		}

		res, err := prov.GenerateWithTools(ctx, step.Model, req.Prompt, req.Images, req.Tools, req.Out)
		if err == nil && res == nil {
			err = fmt.Errorf("%s returned nil result", step.Provider)
		}
		if err == nil {
			return &GenerateResult{
				Text:      strings.TrimSpace(res.Text),
				ToolCalls: res.ToolCalls,
			}, nil
		}

		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		failures = append(failures, stepFailure{step: step, err: err})
		if i+1 < len(plan.Steps) && req.OnFailover != nil {
			req.OnFailover(step, plan.Steps[i+1], err)
		}
	}

	if len(failures) == 1 {
		return nil, failures[0].err
	}
	if len(failures) >= 2 {
		first, second := failures[0], failures[1]
		if first.step.Provider == ProviderCloud && second.step.Provider == ProviderOllama {
			return nil, fmt.Errorf("cloud failed (%w); ollama fallback failed (%w)", first.err, second.err)
		}
		if first.step.Provider == ProviderOllama && second.step.Provider == ProviderCloud {
			return nil, fmt.Errorf("ollama failed (%w); cloud failed (%w)", first.err, second.err)
		}
		return nil, fmt.Errorf("%s failed (%w); %s fallback failed (%w)", first.step.Provider, first.err, second.step.Provider, second.err)
	}
	return nil, fmt.Errorf("all inference providers failed")
}

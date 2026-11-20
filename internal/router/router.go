package router

import (
	"fmt"
	"os"
	"strings"

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

// LocalTier identifies which local model tier handles a local-routed prompt.
type LocalTier string

const (
	TierLocalFast  LocalTier = "fast"
	TierLocalHeavy LocalTier = "heavy"
)

// RouteDecision describes the resolved provider, tier, model, and classification rationale.
type RouteDecision struct {
	Provider   string
	Tier       LocalTier
	Model      string
	Complexity Complexity
	Reason     string
}

// Router classifies prompts and selects local/cloud execution routes.
type Router struct {
	memUsedPercent func() (float64, error)
}

func NewRouter() *Router {
	return &Router{
		memUsedPercent: defaultMemUsedPercent,
	}
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
	resolvedMode := mode
	if strings.TrimSpace(string(resolvedMode)) == "" {
		if envMode, ok := RoutingModeFromEnv(); ok {
			resolvedMode = envMode
		} else {
			resolvedMode = ModeHybrid
		}
	} else {
		resolvedMode = NormalizeRoutingMode(string(resolvedMode))
	}

	effectiveFast := strings.TrimSpace(localFast)
	effectiveHeavy := strings.TrimSpace(localHeavy)
	if effectiveFast == "" {
		effectiveFast = effectiveHeavy
	}
	if effectiveFast == "" {
		effectiveFast = "gemma:2b"
	}
	if effectiveHeavy == "" {
		effectiveHeavy = effectiveFast
	}

	effectiveCloud := strings.TrimSpace(cloudModel)
	if effectiveCloud == "" {
		effectiveCloud = "gemini-3.6-flash"
	}

	promptComplexity, promptReason := classifyPromptHeuristics(prompt)

	switch resolvedMode {
	case ModeLocalOnly:
		// Under ModeLocalOnly, NEVER route to cloud even for >150 chars, coding keywords, or >85% RAM.
		if promptComplexity == ComplexityComplex {
			return RouteDecision{
				Provider:   "ollama",
				Tier:       TierLocalHeavy,
				Model:      effectiveHeavy,
				Complexity: ComplexityComplex,
				Reason:     "local-only mode: " + promptReason,
			}
		}
		return RouteDecision{
			Provider:   "ollama",
			Tier:       TierLocalFast,
			Model:      effectiveFast,
			Complexity: ComplexitySimple,
			Reason:     "local-only mode: " + promptReason,
		}

	case ModeCloudOnly:
		return RouteDecision{
			Provider:   "cloud",
			Model:      effectiveCloud,
			Complexity: promptComplexity,
			Reason:     "cloud-only mode: " + promptReason,
		}

	default: // ModeHybrid
		if usedPct, err := r.memUsage(); err == nil && usedPct > 85.0 {
			return RouteDecision{
				Provider:   "cloud",
				Model:      effectiveCloud,
				Complexity: ComplexityComplex,
				Reason:     fmt.Sprintf("hybrid mode: high memory usage (%.1f%%)", usedPct),
			}
		}
		if promptComplexity == ComplexityComplex {
			return RouteDecision{
				Provider:   "cloud",
				Model:      effectiveCloud,
				Complexity: ComplexityComplex,
				Reason:     "hybrid mode: " + promptReason,
			}
		}
		return RouteDecision{
			Provider:   "ollama",
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

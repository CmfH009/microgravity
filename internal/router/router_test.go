package router

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/CmfH009/microgravity/internal/config"
	"github.com/CmfH009/microgravity/internal/mcp"
)

func newTestRouter(ramPct float64) *Router {
	return &Router{
		memUsedPercent: func() (float64, error) {
			return ramPct, nil
		},
	}
}

func newTestRouterWithProviders(ramPct float64, p Providers) *Router {
	return &Router{
		providers: p,
		memUsedPercent: func() (float64, error) {
			return ramPct, nil
		},
	}
}

func TestRouter_Classify(t *testing.T) {
	tests := []struct {
		name     string
		prompt   string
		expected Complexity
	}{
		{"Simple prompt", "what is 2 + 2?", ComplexitySimple},
		{"Short hello", "hi", ComplexitySimple},
		{"Keyword refactor", "please refactor this code block", ComplexityComplex},
		{"Keyword design", "design a software architecture for a chat server", ComplexityComplex},
		{"Long prompt", "can you tell me a very long story about how the world works, explaining all details, and also providing some code snippet representing standard math equations?", ComplexityComplex},
	}

	r := newTestRouter(40)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := r.Classify(tt.prompt)
			if got != tt.expected {
				t.Errorf("Classify(%q) = %v; expected %v", tt.prompt, got, tt.expected)
			}
		})
	}

	t.Run("High memory >85% escalates simple prompt", func(t *testing.T) {
		if got := newTestRouter(90).Classify("hi"); got != ComplexityComplex {
			t.Errorf("Classify(\"hi\") at 90%% RAM = %v; expected %v", got, ComplexityComplex)
		}
	})
}

func TestNormalizeRoutingMode(t *testing.T) {
	tests := []struct {
		raw  string
		want RoutingMode
	}{
		{"local-only", ModeLocalOnly},
		{"LOCAL-ONLY", ModeLocalOnly},
		{" local ", ModeLocalOnly},
		{"offline", ModeLocalOnly},
		{"hybrid", ModeHybrid},
		{"auto", ModeHybrid},
		{"", ModeHybrid},
		{"cloud-only", ModeCloudOnly},
		{"CLOUD", ModeCloudOnly},
		{"unknown-mode", ModeHybrid},
	}

	for _, tc := range tests {
		if got := NormalizeRoutingMode(tc.raw); got != tc.want {
			t.Errorf("NormalizeRoutingMode(%q) = %q, want %q", tc.raw, got, tc.want)
		}
	}
}

func TestIsCloudModel(t *testing.T) {
	tests := []struct {
		id   string
		want bool
	}{
		{"gemini-3.6-flash", true},
		{"GEMINI-3.1-PRO", true},
		{"gemma-2-27b-it", true},
		{"flash", true},
		{"pro", true},
		{"gemma:2b", false},
		{"gemma4:e2b", false},
		{"orbit-thinker:latest", false},
		{"qwen2.5-coder:7b", false},
		{"", false},
	}
	for _, tc := range tests {
		if got := IsCloudModel(tc.id); got != tc.want {
			t.Errorf("IsCloudModel(%q) = %v, want %v", tc.id, got, tc.want)
		}
	}
}

func TestResolveConfig_UnifiedPrecedenceAndBugFixes(t *testing.T) {
	clearEnv := func(t *testing.T) {
		t.Helper()
		t.Setenv("MG_ROUTING_MODE", "")
		t.Setenv("MG_OPENAI_BASE_URL", "")
		t.Setenv("MG_OPENAI_API_KEY", "")
		t.Setenv("MG_LOCAL_FAST_MODEL", "")
		t.Setenv("MG_LOCAL_HEAVY_MODEL", "")
	}

	t.Run("default hybrid config leaves ExplicitProvider empty so hybrid works in print/chat/tui", func(t *testing.T) {
		clearEnv(t)
		res := ResolveConfig(ConfigInput{Config: &config.Config{}})
		if res.RoutingMode != ModeHybrid {
			t.Fatalf("RoutingMode = %q, want hybrid", res.RoutingMode)
		}
		if res.ExplicitProvider != "" {
			t.Fatalf("ExplicitProvider = %q, want empty for default hybrid", res.ExplicitProvider)
		}
		if res.CloudModel != DefaultCloudModel || res.LocalModel != DefaultLocalModel {
			t.Fatalf("unexpected defaults: %+v", res)
		}
		if res.SkipCloudAuth() {
			t.Fatal("default hybrid must NOT skip cloud auth")
		}
	})

	t.Run("explicit --model cloud flag sets ExplicitProvider=cloud", func(t *testing.T) {
		clearEnv(t)
		res := ResolveConfig(ConfigInput{
			Config:    &config.Config{},
			ModelFlag: "gemini-3.1-pro",
		})
		if res.ExplicitProvider != ProviderCloud || res.CloudModel != "gemini-3.1-pro" || res.ActiveModel != "gemini-3.1-pro" {
			t.Fatalf("unexpected resolved config: %+v", res)
		}
	})

	t.Run("local ActiveModel in config NEVER overwrites CloudModel", func(t *testing.T) {
		clearEnv(t)
		res := ResolveConfig(ConfigInput{
			Config: &config.Config{ActiveModel: "orbit-thinker:latest"},
		})
		if res.ExplicitProvider != ProviderOllama {
			t.Fatalf("ExplicitProvider = %q, want ollama", res.ExplicitProvider)
		}
		if res.LocalModel != "orbit-thinker:latest" {
			t.Fatalf("LocalModel = %q, want orbit-thinker:latest", res.LocalModel)
		}
		if res.CloudModel != DefaultCloudModel {
			t.Fatalf("CloudModel polluted with local model: got %q, want %q", res.CloudModel, DefaultCloudModel)
		}
		if !res.SkipCloudAuth() {
			t.Fatal("local ActiveModel must skip cloud auth")
		}
	})

	t.Run("local-only + base-url flags honored with flag > env > config precedence", func(t *testing.T) {
		clearEnv(t)
		t.Setenv("MG_ROUTING_MODE", "hybrid")
		t.Setenv("MG_OPENAI_BASE_URL", "http://env:8080/v1")
		t.Setenv("MG_LOCAL_FAST_MODEL", "env-fast")
		t.Setenv("MG_LOCAL_HEAVY_MODEL", "env-heavy")

		res := ResolveConfig(ConfigInput{
			Config:          &config.Config{RoutingMode: "cloud-only", OpenAIBaseURL: "http://cfg:9999/v1"},
			RoutingMode:     "local-only",
			OpenAIBaseURL:   "http://flag:1234/v1",
			ModelFlag:       "llama-3.2-3b",
			LocalFastModel:  "flag-fast",
			LocalHeavyModel: "",
		})
		if res.RoutingMode != ModeLocalOnly {
			t.Fatalf("RoutingMode = %q, want local-only", res.RoutingMode)
		}
		if res.OpenAIBaseURL != "http://flag:1234/v1" || res.ExplicitProvider != ProviderOpenAI || res.OpenAIModel != "llama-3.2-3b" {
			t.Fatalf("unexpected openai resolution: %+v", res)
		}
		if res.LocalFastModel != "flag-fast" || res.LocalHeavyModel != "env-heavy" {
			t.Fatalf("unexpected tier resolution: fast=%q heavy=%q", res.LocalFastModel, res.LocalHeavyModel)
		}
		if !res.SkipCloudAuth() {
			t.Fatal("expected SkipCloudAuth=true")
		}
	})
}

func TestSelectRoute_TableDriven(t *testing.T) {
	longPrompt := strings.Repeat("a", 160)

	tests := []struct {
		name         string
		ramPct       float64
		prompt       string
		mode         RoutingMode
		localFast    string
		localHeavy   string
		cloudModel   string
		wantProvider string
		wantTier     LocalTier
		wantModel    string
		wantComplex  Complexity
	}{
		{
			name:         "local-only simple prompt uses TierLocalFast",
			ramPct:       40,
			prompt:       "hi",
			mode:         ModeLocalOnly,
			localFast:    "gemma4:e2b",
			localHeavy:   "qwen2.5-coder:7b",
			cloudModel:   "gemini-3.6-flash",
			wantProvider: "ollama",
			wantTier:     TierLocalFast,
			wantModel:    "gemma4:e2b",
			wantComplex:  ComplexitySimple,
		},
		{
			name:         "local-only coding keyword never hits cloud and picks TierLocalHeavy",
			ramPct:       40,
			prompt:       "refactor and debug this parser",
			mode:         ModeLocalOnly,
			localFast:    "gemma4:e2b",
			localHeavy:   "qwen2.5-coder:7b",
			cloudModel:   "gemini-3.6-flash",
			wantProvider: "ollama",
			wantTier:     TierLocalHeavy,
			wantModel:    "qwen2.5-coder:7b",
			wantComplex:  ComplexityComplex,
		},
		{
			name:         "local-only >150 chars never hits cloud and picks TierLocalHeavy",
			ramPct:       40,
			prompt:       longPrompt,
			mode:         ModeLocalOnly,
			localFast:    "gemma4:e2b",
			localHeavy:   "qwen2.5-coder:14b",
			cloudModel:   "gemini-3.6-flash",
			wantProvider: "ollama",
			wantTier:     TierLocalHeavy,
			wantModel:    "qwen2.5-coder:14b",
			wantComplex:  ComplexityComplex,
		},
		{
			name:         "local-only >85% RAM never hits cloud (simple prompt)",
			ramPct:       92.5,
			prompt:       "hi",
			mode:         ModeLocalOnly,
			localFast:    "gemma4:e2b",
			localHeavy:   "qwen2.5-coder:7b",
			cloudModel:   "gemini-3.6-flash",
			wantProvider: "ollama",
			wantTier:     TierLocalFast,
			wantModel:    "gemma4:e2b",
			wantComplex:  ComplexitySimple,
		},
		{
			name:         "local-only >85% RAM never hits cloud (complex prompt)",
			ramPct:       95.0,
			prompt:       "implement distributed consensus",
			mode:         ModeLocalOnly,
			localFast:    "gemma4:e2b",
			localHeavy:   "qwen2.5-coder:7b",
			cloudModel:   "gemini-3.6-flash",
			wantProvider: "ollama",
			wantTier:     TierLocalHeavy,
			wantModel:    "qwen2.5-coder:7b",
			wantComplex:  ComplexityComplex,
		},
		{
			name:         "local-only falls back to localFast when localHeavy is empty",
			ramPct:       40,
			prompt:       "implement unit tests",
			mode:         ModeLocalOnly,
			localFast:    "gemma4:e2b",
			localHeavy:   "",
			cloudModel:   "gemini-3.6-flash",
			wantProvider: "ollama",
			wantTier:     TierLocalHeavy,
			wantModel:    "gemma4:e2b",
			wantComplex:  ComplexityComplex,
		},
		{
			name:         "hybrid simple prompt uses localFast",
			ramPct:       40,
			prompt:       "what is 2 + 2?",
			mode:         ModeHybrid,
			localFast:    "gemma4:e2b",
			localHeavy:   "qwen2.5-coder:7b",
			cloudModel:   "gemini-3.6-flash",
			wantProvider: "ollama",
			wantTier:     TierLocalFast,
			wantModel:    "gemma4:e2b",
			wantComplex:  ComplexitySimple,
		},
		{
			name:         "hybrid complex keyword routes to cloud",
			ramPct:       40,
			prompt:       "design a distributed cache",
			mode:         ModeHybrid,
			localFast:    "gemma4:e2b",
			localHeavy:   "qwen2.5-coder:7b",
			cloudModel:   "gemini-3.6-flash",
			wantProvider: "cloud",
			wantTier:     "",
			wantModel:    "gemini-3.6-flash",
			wantComplex:  ComplexityComplex,
		},
		{
			name:         "hybrid >85% RAM routes simple prompt to cloud",
			ramPct:       90.0,
			prompt:       "hi",
			mode:         ModeHybrid,
			localFast:    "gemma4:e2b",
			localHeavy:   "qwen2.5-coder:7b",
			cloudModel:   "gemini-3.6-flash",
			wantProvider: "cloud",
			wantTier:     "",
			wantModel:    "gemini-3.6-flash",
			wantComplex:  ComplexityComplex,
		},
		{
			name:         "cloud-only routes even simple prompt to cloud",
			ramPct:       20,
			prompt:       "hi",
			mode:         ModeCloudOnly,
			localFast:    "gemma4:e2b",
			localHeavy:   "qwen2.5-coder:7b",
			cloudModel:   "gemini-3.8-flash",
			wantProvider: "cloud",
			wantTier:     "",
			wantModel:    "gemini-3.8-flash",
			wantComplex:  ComplexitySimple,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("MG_ROUTING_MODE", "")
			r := newTestRouter(tc.ramPct)
			got := r.SelectRoute(tc.prompt, tc.mode, tc.localFast, tc.localHeavy, tc.cloudModel)
			if got.Provider != tc.wantProvider || got.Tier != tc.wantTier || got.Model != tc.wantModel || got.Complexity != tc.wantComplex {
				t.Fatalf("SelectRoute() = %+v; want provider=%q tier=%q model=%q complexity=%q",
					got, tc.wantProvider, tc.wantTier, tc.wantModel, tc.wantComplex)
			}
			if got.Reason == "" {
				t.Errorf("expected non-empty Reason in RouteDecision")
			}
		})
	}
}

func TestSelectRoute_EnvVarOverrideAndPackageHelper(t *testing.T) {
	t.Setenv("MG_ROUTING_MODE", "local-only")
	if mode, ok := RoutingModeFromEnv(); !ok || mode != ModeLocalOnly {
		t.Fatalf("expected RoutingModeFromEnv() = (local-only, true), got (%q, %v)", mode, ok)
	}

	// Empty mode parameter falls back to MG_ROUTING_MODE env var.
	dec := SelectRoute("architect a microservice", "", "gemma4:e2b", "qwen2.5-coder:7b", "gemini-3.6-flash")
	if dec.Provider != "ollama" || dec.Tier != TierLocalHeavy || dec.Model != "qwen2.5-coder:7b" {
		t.Fatalf("expected MG_ROUTING_MODE=local-only to route to local heavy, got %+v", dec)
	}
}

func TestRouter_PlanAndGenerate_FailoverAndIsolation(t *testing.T) {
	t.Setenv("MG_ROUTING_MODE", "")
	errCloudQuota := errors.New("cloud quota exhausted")
	errLocalMissing := errors.New("ollama model missing")

	t.Run("hybrid complex fails over cloud -> ollama and triggers OnFailover", func(t *testing.T) {
		var cloudHits, ollamaHits, failoverCount int32
		r := newTestRouterWithProviders(40, Providers{
			Cloud: ProviderFunc(func(ctx context.Context, model, prompt string, images []string, tools []mcp.Tool, out io.Writer) (*GenerateResult, error) {
				atomic.AddInt32(&cloudHits, 1)
				return nil, errCloudQuota
			}),
			Ollama: ProviderFunc(func(ctx context.Context, model, prompt string, images []string, tools []mcp.Tool, out io.Writer) (*GenerateResult, error) {
				atomic.AddInt32(&ollamaHits, 1)
				return &GenerateResult{Text: "fallback:" + model}, nil
			}),
		})

		plan := r.Plan("design distributed consensus", RoutePolicy{
			RoutingMode:     ModeHybrid,
			ActiveModel:     "gemini-3.8-flash",
			LocalFastModel:  "gemma4:e2b",
			LocalHeavyModel: "qwen2.5-coder:7b",
		})
		if plan.BudgetModel != "gemini-3.8-flash" || len(plan.Steps) != 2 || plan.Steps[0].Provider != ProviderCloud || plan.Steps[1].Provider != ProviderOllama {
			t.Fatalf("unexpected plan: %+v", plan)
		}

		res, err := r.Generate(context.Background(), plan, GenerateRequest{
			Prompt: "design distributed consensus",
			Out:    io.Discard,
			OnFailover: func(failed, next DispatchStep, err error) {
				atomic.AddInt32(&failoverCount, 1)
			},
		})
		if err != nil {
			t.Fatalf("expected failover success, got %v", err)
		}
		if res.Text != "fallback:qwen2.5-coder:7b" {
			t.Fatalf("got %q, want fallback:qwen2.5-coder:7b", res.Text)
		}
		if cloudHits != 1 || ollamaHits != 1 || failoverCount != 1 {
			t.Fatalf("hits=(cloud:%d ollama:%d failover:%d), want (1,1,1)", cloudHits, ollamaHits, failoverCount)
		}
	})

	t.Run("both providers fail wraps both errors", func(t *testing.T) {
		r := newTestRouterWithProviders(40, Providers{
			Cloud: ProviderFunc(func(ctx context.Context, model, prompt string, images []string, tools []mcp.Tool, out io.Writer) (*GenerateResult, error) {
				return nil, errCloudQuota
			}),
			Ollama: ProviderFunc(func(ctx context.Context, model, prompt string, images []string, tools []mcp.Tool, out io.Writer) (*GenerateResult, error) {
				return nil, errLocalMissing
			}),
		})
		plan := r.Plan("hi", RoutePolicy{
			ExplicitProvider: ProviderCloud,
			ActiveModel:      "gemini-3.8-flash",
			LocalModel:       "gemma4:e2b",
		})
		_, err := r.Generate(context.Background(), plan, GenerateRequest{Prompt: "hi", Out: io.Discard})
		if !errors.Is(err, errCloudQuota) || !errors.Is(err, errLocalMissing) {
			t.Fatalf("expected wrapped primary+fallback errors, got: %v", err)
		}
	})

	t.Run("context cancellation aborts without failover", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		var ollamaHits int32
		r := newTestRouterWithProviders(40, Providers{
			Cloud: ProviderFunc(func(ctx context.Context, model, prompt string, images []string, tools []mcp.Tool, out io.Writer) (*GenerateResult, error) {
				cancel()
				return nil, ctx.Err()
			}),
			Ollama: ProviderFunc(func(ctx context.Context, model, prompt string, images []string, tools []mcp.Tool, out io.Writer) (*GenerateResult, error) {
				atomic.AddInt32(&ollamaHits, 1)
				return &GenerateResult{Text: "should-not-run"}, nil
			}),
		})
		plan := r.Plan("hi", RoutePolicy{ExplicitProvider: ProviderCloud, ActiveModel: "gemini-3.8-flash"})
		_, err := r.Generate(ctx, plan, GenerateRequest{Prompt: "hi", Out: io.Discard})
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("expected context.Canceled, got %v", err)
		}
		if ollamaHits != 0 {
			t.Fatalf("expected 0 fallback calls after cancel, got %d", ollamaHits)
		}
	})

	t.Run("local-only never touches cloud even if ExplicitProvider=cloud", func(t *testing.T) {
		var cloudHits int32
		r := newTestRouterWithProviders(95, Providers{
			Cloud: ProviderFunc(func(ctx context.Context, model, prompt string, images []string, tools []mcp.Tool, out io.Writer) (*GenerateResult, error) {
				atomic.AddInt32(&cloudHits, 1)
				return &GenerateResult{Text: "cloud"}, nil
			}),
			Ollama: ProviderFunc(func(ctx context.Context, model, prompt string, images []string, tools []mcp.Tool, out io.Writer) (*GenerateResult, error) {
				return nil, errLocalMissing
			}),
		})
		plan := r.Plan("refactor engine", RoutePolicy{
			RoutingMode:      ModeLocalOnly,
			ExplicitProvider: ProviderCloud,
			LocalFastModel:   "gemma4:e2b",
			LocalHeavyModel:  "qwen2.5-coder:7b",
		})
		if plan.BudgetModel != "qwen2.5-coder:7b" || len(plan.Steps) != 1 || plan.Steps[0].Provider != ProviderOllama {
			t.Fatalf("unexpected local-only plan: %+v", plan)
		}
		_, err := r.Generate(context.Background(), plan, GenerateRequest{Prompt: "refactor engine", Out: io.Discard})
		if !errors.Is(err, errLocalMissing) {
			t.Fatalf("expected errLocalMissing, got %v", err)
		}
		if cloudHits != 0 {
			t.Fatalf("expected 0 cloud hits in local-only, got %d", cloudHits)
		}
	})
}

package router

import (
	"strings"
	"testing"
)

func newTestRouter(ramPct float64) *Router {
	return &Router{
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

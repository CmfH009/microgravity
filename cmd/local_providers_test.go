package cmd

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/c-bata/go-prompt"
	"github.com/CmfH009/microgravity/internal/config"
	"github.com/CmfH009/microgravity/internal/engine"
	"github.com/CmfH009/microgravity/internal/ui"
)

func isolateCmdTestEnv(t *testing.T) string {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("USERPROFILE", tmp)
	t.Setenv("APPDATA", tmp)
	t.Setenv("MG_BUDGET_FILE", filepath.Join(tmp, "budget.json"))
	t.Setenv("MG_BUDGET_DISABLE", "1")
	t.Setenv("GEMINI_API_KEY", "")
	t.Setenv("GOOGLE_API_KEY", "")
	t.Setenv("MG_ROUTING_MODE", "")
	t.Setenv("MG_OPENAI_BASE_URL", "")
	t.Setenv("MG_OPENAI_API_KEY", "")
	t.Setenv("MG_LOCAL_FAST_MODEL", "")
	t.Setenv("MG_LOCAL_HEAVY_MODEL", "")
	return tmp
}

func TestRootFlags_LocalAndOpenAIRegistered(t *testing.T) {
	expectedFlags := []string{
		"local",
		"routing-mode",
		"base-url",
		"local-fast-model",
		"local-heavy-model",
	}

	for _, name := range expectedFlags {
		if rootCmd.Flag(name) == nil || rootCmd.PersistentFlags().Lookup(name) == nil {
			t.Errorf("expected persistent flag --%s on rootCmd", name)
		}
		if chatCmd.Flag(name) == nil {
			t.Errorf("expected persistent flag --%s inherited on chatCmd", name)
		}
	}
}

func TestShouldSkipCloudAuth_TableDriven(t *testing.T) {
	tests := []struct {
		name            string
		cfg             *config.Config
		localFlag       bool
		routingModeFlag string
		baseURLFlag     string
		envRoutingMode  string
		envBaseURL      string
		wantSkip        bool
	}{
		{
			name:     "default hybrid with no flags or env requires auth check",
			cfg:      &config.Config{},
			wantSkip: false,
		},
		{
			name:      "--local flag skips auth",
			cfg:       &config.Config{},
			localFlag: true,
			wantSkip:  true,
		},
		{
			name:            "--routing-mode local-only skips auth",
			cfg:             &config.Config{},
			routingModeFlag: "local-only",
			wantSkip:        true,
		},
		{
			name:           "MG_ROUTING_MODE=local-only skips auth",
			cfg:            &config.Config{},
			envRoutingMode: "local-only",
			wantSkip:       true,
		},
		{
			name:     "persisted config routing_mode=local-only skips auth",
			cfg:      &config.Config{RoutingMode: "local-only"},
			wantSkip: true,
		},
		{
			name:        "--base-url skips auth",
			cfg:         &config.Config{},
			baseURLFlag: "http://127.0.0.1:1234/v1",
			wantSkip:    true,
		},
		{
			name:       "MG_OPENAI_BASE_URL skips auth",
			cfg:        &config.Config{},
			envBaseURL: "http://127.0.0.1:8080/v1",
			wantSkip:   true,
		},
		{
			name:     "persisted config openai_base_url skips auth",
			cfg:      &config.Config{OpenAIBaseURL: "http://127.0.0.1:8000/v1"},
			wantSkip: true,
		},
		{
			name:            "--routing-mode hybrid overrides MG_ROUTING_MODE=local-only",
			cfg:             &config.Config{},
			routingModeFlag: "hybrid",
			envRoutingMode:  "local-only",
			wantSkip:        false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			isolateCmdTestEnv(t)
			t.Setenv("MG_ROUTING_MODE", tc.envRoutingMode)
			t.Setenv("MG_OPENAI_BASE_URL", tc.envBaseURL)

			got := shouldSkipCloudAuth(tc.cfg, tc.localFlag, tc.routingModeFlag, tc.baseURLFlag)
			if got != tc.wantSkip {
				t.Fatalf("shouldSkipCloudAuth() = %v, want %v", got, tc.wantSkip)
			}
		})
	}
}

func TestBuildEngineSession_LocalAndOpenAIWiring(t *testing.T) {
	tmp := isolateCmdTestEnv(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/chat/completions" {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = w.Write([]byte(`data: {"choices":[{"index":0,"delta":{"content":"session-factory-local-ok"}}]}` + "\n\n"))
			_, _ = w.Write([]byte("data: [DONE]\n\n"))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	t.Setenv("MG_ROUTING_MODE", "local-only")
	t.Setenv("MG_OPENAI_BASE_URL", srv.URL+"/v1")

	sess, cleanup, err := BuildEngineSession(context.Background(), SessionOptions{
		SessionName:     "factory-local-test",
		Cwd:             tmp,
		LocalFastModel:  "gemma4:e2b",
		LocalHeavyModel: "qwen2.5-coder:7b",
		OpenAIModel:     "qwen2.5-coder:7b",
	})
	if err != nil {
		t.Fatalf("BuildEngineSession failed: %v", err)
	}
	defer cleanup()

	if sess.Config.RoutingMode != "local-only" {
		t.Fatalf("expected RoutingMode=local-only, got %q", sess.Config.RoutingMode)
	}
	if sess.Config.OpenAIBaseURL != srv.URL+"/v1" {
		t.Fatalf("expected OpenAIBaseURL=%q, got %q", srv.URL+"/v1", sess.Config.OpenAIBaseURL)
	}
	if sess.Config.LocalFastModel != "gemma4:e2b" || sess.Config.LocalHeavyModel != "qwen2.5-coder:7b" {
		t.Fatalf("unexpected local tier models: fast=%q heavy=%q", sess.Config.LocalFastModel, sess.Config.LocalHeavyModel)
	}
	if sess.OpenAIClient == nil || sess.OpenAIClient.BaseURL != srv.URL+"/v1" {
		t.Fatalf("expected initialized OpenAIClient targeting %q, got %+v", srv.URL+"/v1", sess.OpenAIClient)
	}

	sess.Config.CoreMemory = "<core_memory></core_memory>"

	var out bytes.Buffer
	resp, err := sess.RunPrompt(context.Background(), "hello", nil, &out)
	if err != nil {
		t.Fatalf("RunPrompt via OpenAIClient failed: %v", err)
	}
	if resp != "session-factory-local-ok" {
		t.Fatalf("expected 'session-factory-local-ok', got %q", resp)
	}
	if !strings.Contains(out.String(), "session-factory-local-ok") {
		t.Fatalf("expected streamed output to contain 'session-factory-local-ok', got %q", out.String())
	}
}

func TestCompleterAndSlashLocalAndTheme(t *testing.T) {
	isolateCmdTestEnv(t)
	t.Cleanup(func() { ui.SetTheme("dark") })

	docFor := func(text string) prompt.Document {
		buf := prompt.NewBuffer()
		buf.InsertText(text, false, true)
		return *buf.Document()
	}

	// Verify /local and /theme appear in completer suggestions.
	localSug := completer(docFor("/lo"))
	if len(localSug) == 0 || localSug[0].Text != "/local" {
		t.Fatalf("expected /local completion for '/lo', got %+v", localSug)
	}
	themeSug := completer(docFor("/th"))
	if len(themeSug) == 0 || themeSug[0].Text != "/theme" {
		t.Fatalf("expected /theme completion for '/th', got %+v", themeSug)
	}

	sess, err := engine.NewSession(engine.SessionConfig{
		SessionName:     "slash-test",
		LocalFastModel:  "gemma4:e2b",
		LocalHeavyModel: "qwen2.5-coder:7b",
		CoreMemory:      "<core_memory></core_memory>",
	})
	if err != nil {
		t.Fatalf("NewSession failed: %v", err)
	}
	defer sess.Close()

	// /local on -> local-only
	out := handleLocalSlashCommand(sess, "on")
	if !strings.Contains(out, "local-only") {
		t.Fatalf("expected local-only confirmation, got %q", out)
	}
	if sess.GetRoutingMode() != "local-only" || config.GetRoutingMode() != "local-only" {
		t.Fatalf("expected session & persisted config mode=local-only, got session=%q config=%q", sess.GetRoutingMode(), config.GetRoutingMode())
	}

	// /local status
	statusOut := handleLocalSlashCommand(sess, "status")
	if !strings.Contains(statusOut, "local-only") || !strings.Contains(statusOut, "gemma4:e2b") {
		t.Fatalf("unexpected /local status output: %q", statusOut)
	}

	// /local off -> hybrid
	offOut := handleLocalSlashCommand(sess, "off")
	if !strings.Contains(offOut, "hybrid") || sess.GetRoutingMode() != "hybrid" {
		t.Fatalf("expected hybrid mode after '/local off', got %q / %q", offOut, sess.GetRoutingMode())
	}

	// /theme light
	themeOut := handleThemeSlashCommand("light")
	if !strings.Contains(themeOut, "light") || ui.CurrentTheme.Name != "light" || config.GetTheme() != "light" {
		t.Fatalf("expected theme=light, got out=%q current=%q config=%q", themeOut, ui.CurrentTheme.Name, config.GetTheme())
	}

	// /theme tokyo-night persists cleanly even before theme palette expansion merges
	tnOut := handleThemeSlashCommand("tokyo-night")
	if !strings.Contains(tnOut, "tokyo-night") || config.GetTheme() != "tokyo-night" {
		t.Fatalf("expected tokyo-night persisted, got out=%q config=%q", tnOut, config.GetTheme())
	}

	// /theme bogus reports error
	badTheme := handleThemeSlashCommand("nonexistent-theme")
	if !strings.Contains(badTheme, "unknown theme") {
		t.Fatalf("expected unknown theme error, got %q", badTheme)
	}
}

func TestShouldSkipCloudAuth_LocalActiveModel(t *testing.T) {
	origRootModel := rootModel
	t.Cleanup(func() { rootModel = origRootModel })
	rootModel = ""

	cases := []struct {
		name   string
		active string
		want   bool
	}{
		{"local orbit-thinker skips cloud auth", "orbit-thinker:latest", true},
		{"local qwen skips cloud auth", "qwen2.5-coder:7b", true},
		{"cloud gemini requires cloud auth", "gemini-3.6-flash", false},
		{"cloud gemma-2 requires cloud auth", "gemma-2-27b-it", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &config.Config{ActiveModel: tc.active}
			got := shouldSkipCloudAuth(cfg, false, "", "")
			if got != tc.want {
				t.Fatalf("shouldSkipCloudAuth(ActiveModel=%q) = %v, want %v", tc.active, got, tc.want)
			}
		})
	}
}

func TestUnifiedRoutingResolution_NoTuiOrPrintDrift(t *testing.T) {
	tmp := isolateCmdTestEnv(t)

	// 1. Local active model persisted in config must NOT overwrite CloudModelName and must route to ollama.
	sess, cleanup, err := BuildEngineSession(context.Background(), SessionOptions{
		SessionName:    "tui-local-model-check",
		Cwd:            tmp,
		ActiveModel:    "orbit-thinker:latest",
		LocalModelName: "gemma:2b",
		CloudModelName: "gemini-3.6-flash",
	})
	if err != nil {
		t.Fatalf("BuildEngineSession failed: %v", err)
	}
	defer cleanup()

	if sess.Config.CloudModelName != "gemini-3.6-flash" {
		t.Fatalf("local ActiveModel polluted CloudModelName: got %q, want gemini-3.6-flash", sess.Config.CloudModelName)
	}
	if sess.Config.LocalModelName != "orbit-thinker:latest" {
		t.Fatalf("expected LocalModelName=orbit-thinker:latest, got %q", sess.Config.LocalModelName)
	}
	if sess.Config.ExplicitProvider != "ollama" {
		t.Fatalf("expected ExplicitProvider=ollama for local ActiveModel, got %q", sess.Config.ExplicitProvider)
	}

	// 2. Default cloud model in hybrid mode (without explicit --model flag) must NOT force ExplicitProvider="cloud".
	sessHybrid, cleanupHybrid, err := BuildEngineSession(context.Background(), SessionOptions{
		SessionName:    "hybrid-default-check",
		Cwd:            tmp,
		CloudModelName: "gemini-3.6-flash",
	})
	if err != nil {
		t.Fatalf("BuildEngineSession hybrid failed: %v", err)
	}
	defer cleanupHybrid()

	if sessHybrid.Config.ExplicitProvider != "" {
		t.Fatalf("default hybrid session must leave ExplicitProvider empty, got %q", sessHybrid.Config.ExplicitProvider)
	}
	if sessHybrid.Config.RoutingMode != "hybrid" {
		t.Fatalf("expected RoutingMode=hybrid, got %q", sessHybrid.Config.RoutingMode)
	}
}

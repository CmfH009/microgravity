package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestConfigActiveModel(t *testing.T) {
	tempHome := t.TempDir()
	os.Setenv("USERPROFILE", tempHome)
	os.Setenv("HOME", tempHome)

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	if cfg.ActiveModel != "" {
		t.Errorf("Expected empty initial ActiveModel, got '%s'", cfg.ActiveModel)
	}

	err = SetActiveModel("gemini-3.1-pro")
	if err != nil {
		t.Fatalf("SetActiveModel failed: %v", err)
	}

	active := GetActiveModel()
	if active != "gemini-3.1-pro" {
		t.Errorf("Expected active model 'gemini-3.1-pro', got '%s'", active)
	}

	// Verify persistence file exists
	cfgPath, _ := GetConfigPath()
	if _, err := os.Stat(cfgPath); os.IsNotExist(err) {
		t.Errorf("Config file was not created at %s", cfgPath)
	}
}

func TestConfigMCPServers(t *testing.T) {
	tempHome := t.TempDir()
	os.Setenv("USERPROFILE", tempHome)
	os.Setenv("HOME", tempHome)

	entry := MCPServerEntry{
		Name:    "filesystem",
		Command: "npx",
		Args:    []string{"-y", "@modelcontextprotocol/server-filesystem", "/work"},
		Type:    "stdio",
		Enabled: true,
	}

	if err := AddMCPServer(entry); err != nil {
		t.Fatalf("failed to add MCP server: %v", err)
	}

	list := GetMCPServersDetailed()
	if len(list) != 1 || list[0].Name != "filesystem" {
		t.Fatalf("expected 1 server named filesystem, got %+v", list)
	}

	if err := DisableMCPServer("filesystem"); err != nil {
		t.Fatalf("failed to disable server: %v", err)
	}
	list = GetMCPServersDetailed()
	if list[0].Enabled {
		t.Errorf("expected server to be disabled")
	}

	if err := EnableMCPServer("filesystem"); err != nil {
		t.Fatalf("failed to enable server: %v", err)
	}
	list = GetMCPServersDetailed()
	if !list[0].Enabled {
		t.Errorf("expected server to be enabled")
	}

	if err := RemoveMCPServer("filesystem"); err != nil {
		t.Fatalf("failed to remove server: %v", err)
	}
	list = GetMCPServersDetailed()
	if len(list) != 0 {
		t.Errorf("expected 0 servers after removal, got %d", len(list))
	}
}

func TestVertexConfig(t *testing.T) {
	tempHome := t.TempDir()
	os.Setenv("USERPROFILE", tempHome)
	os.Setenv("HOME", tempHome)

	// Defaults
	authType, proj, loc, key := GetVertexConfig()
	if authType != "studio" || loc != "us-central1" || proj != "" || key != "" {
		t.Errorf("unexpected default vertex config: %s, %s, %s, %s", authType, proj, loc, key)
	}

	// Set Express Mode
	err := SetVertexConfig("vertex-express", "", "europe-west1", "my-vertex-key")
	if err != nil {
		t.Fatalf("SetVertexConfig failed: %v", err)
	}

	authType, proj, loc, key = GetVertexConfig()
	if authType != "vertex-express" || loc != "europe-west1" || key != "my-vertex-key" {
		t.Errorf("unexpected express config: %s, %s, %s", authType, loc, key)
	}

	// Set Standard Mode
	err = SetVertexConfig("vertex-standard", "my-gcp-project", "us-central1", "")
	if err != nil {
		t.Fatalf("SetVertexConfig standard failed: %v", err)
	}

	authType, proj, loc, _ = GetVertexConfig()
	if authType != "vertex-standard" || proj != "my-gcp-project" || loc != "us-central1" {
		t.Errorf("unexpected standard config: %s, %s, %s", authType, proj, loc)
	}
}

func TestConfigNoHardcodedMachinePaths(t *testing.T) {
	data, err := os.ReadFile("config.go")
	if err != nil {
		t.Fatalf("failed to read config.go: %v", err)
	}
	src := string(data)
	for _, forbidden := range []string{`A:\`, `A:/`, `C:\AgyHut`, `C:/AgyHut`} {
		if strings.Contains(src, forbidden) {
			t.Errorf("config.go contains forbidden hardcoded machine path %q", forbidden)
		}
	}
}

func TestDetectOrbitMCPServer(t *testing.T) {
	writeDummyScript := func(t *testing.T, p string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
			t.Fatalf("mkdir %s: %v", p, err)
		}
		if err := os.WriteFile(p, []byte("# dummy orbit mcp\n"), 0644); err != nil {
			t.Fatalf("write %s: %v", p, err)
		}
	}

	clearEnv := func(t *testing.T, home string) {
		t.Helper()
		t.Setenv("MG_ORBIT_MCP_SCRIPT", "")
		t.Setenv("MG_HOME", "")
		t.Setenv("AGY_HOME", "")
		t.Setenv("HOME", home)
		t.Setenv("USERPROFILE", home)
	}

	t.Run("none present returns empty", func(t *testing.T) {
		clearEnv(t, t.TempDir())
		if got := DetectOrbitMCPServer(); got != "" {
			t.Fatalf("expected empty command when no script exists, got %q", got)
		}
	})

	t.Run("MG_ORBIT_MCP_SCRIPT takes precedence", func(t *testing.T) {
		root := t.TempDir()
		clearEnv(t, root)
		script := filepath.Join(root, "custom", "orbit_mcp_server.py")
		writeDummyScript(t, script)
		t.Setenv("MG_ORBIT_MCP_SCRIPT", script)

		if got := DetectOrbitMCPServer(); got != "python "+script {
			t.Fatalf("expected %q, got %q", "python "+script, got)
		}
	})

	t.Run("MG_HOME scripts directory", func(t *testing.T) {
		root := t.TempDir()
		clearEnv(t, root)
		mgHome := filepath.Join(root, "mg")
		script := filepath.Join(mgHome, "scripts", "orbit_mcp_server.py")
		writeDummyScript(t, script)
		t.Setenv("MG_HOME", mgHome)

		if got := DetectOrbitMCPServer(); got != "python "+script {
			t.Fatalf("expected %q, got %q", "python "+script, got)
		}
	})

	t.Run("AGY_HOME system scripts directory", func(t *testing.T) {
		root := t.TempDir()
		clearEnv(t, root)
		agyHome := filepath.Join(root, "agy")
		script := filepath.Join(agyHome, "system", "scripts", "orbit_mcp_server.py")
		writeDummyScript(t, script)
		t.Setenv("AGY_HOME", agyHome)

		if got := DetectOrbitMCPServer(); got != "python "+script {
			t.Fatalf("expected %q, got %q", "python "+script, got)
		}
	})

	t.Run("home .microgravity scripts fallback", func(t *testing.T) {
		home := t.TempDir()
		clearEnv(t, home)
		script := filepath.Join(home, ".microgravity", "scripts", "orbit_mcp_server.py")
		writeDummyScript(t, script)

		if got := DetectOrbitMCPServer(); got != "python "+script {
			t.Fatalf("expected %q, got %q", "python "+script, got)
		}
	})

	t.Run("home .gemini antigravity-cli scripts fallback", func(t *testing.T) {
		home := t.TempDir()
		clearEnv(t, home)
		script := filepath.Join(home, ".gemini", "antigravity-cli", "scripts", "orbit_mcp_server.py")
		writeDummyScript(t, script)

		if got := DetectOrbitMCPServer(); got != "python "+script {
			t.Fatalf("expected %q, got %q", "python "+script, got)
		}
	})
}

func TestConfigLocalFirstAndThemeRoundTrip(t *testing.T) {
	home := t.TempDir()
	t.Setenv("USERPROFILE", home)
	t.Setenv("HOME", home)

	if got := GetRoutingMode(); got != "hybrid" {
		t.Fatalf("expected default routing mode 'hybrid', got %q", got)
	}
	if got := GetTheme(); got != "dark" {
		t.Fatalf("expected default theme 'dark', got %q", got)
	}

	if err := SetRoutingMode("local-only"); err != nil {
		t.Fatalf("SetRoutingMode failed: %v", err)
	}
	if got := GetRoutingMode(); got != "local-only" {
		t.Fatalf("expected 'local-only', got %q", got)
	}

	if err := SetRoutingMode("invalid-mode"); err == nil {
		t.Fatalf("expected error for invalid routing mode")
	}

	if err := SetTheme("tokyo-night"); err != nil {
		t.Fatalf("SetTheme failed: %v", err)
	}
	if got := GetTheme(); got != "tokyo-night" {
		t.Fatalf("expected 'tokyo-night', got %q", got)
	}
	if err := SetTheme("  "); err == nil {
		t.Fatalf("expected error for empty theme")
	}

	// Full struct round-trip persistence
	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}
	cfg.RoutingMode = "local-only"
	cfg.LocalFastModel = "gemma4:e2b"
	cfg.LocalHeavyModel = "qwen2.5-coder:7b"
	cfg.OpenAIBaseURL = "http://127.0.0.1:1234/v1"
	cfg.OpenAIAPIKey = "local-dev-key"
	cfg.OpenAIModel = "qwen2.5-coder-7b-instruct"
	cfg.Theme = "catppuccin"

	if err := SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig failed: %v", err)
	}

	reloaded, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig reload failed: %v", err)
	}
	if reloaded.RoutingMode != "local-only" ||
		reloaded.LocalFastModel != "gemma4:e2b" ||
		reloaded.LocalHeavyModel != "qwen2.5-coder:7b" ||
		reloaded.OpenAIBaseURL != "http://127.0.0.1:1234/v1" ||
		reloaded.OpenAIAPIKey != "local-dev-key" ||
		reloaded.OpenAIModel != "qwen2.5-coder-7b-instruct" ||
		reloaded.Theme != "catppuccin" {
		t.Fatalf("round-trip config mismatch: %+v", reloaded)
	}
}



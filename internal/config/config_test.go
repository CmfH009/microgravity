package config

import (
	"os"
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

func TestDetectOrbitMCPServer(t *testing.T) {
	cmd := DetectOrbitMCPServer()
	// If running in A:\ environment where orbit_mcp_server.py exists, it should be detected
	if _, err := os.Stat(`A:\system\scripts\orbit_mcp_server.py`); err == nil {
		if cmd == "" {
			t.Errorf("Expected DetectOrbitMCPServer to detect existing orbit_mcp_server.py, got empty")
		}
	}
}



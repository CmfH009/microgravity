package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDiscoverMCPServers_ReturnsCandidates(t *testing.T) {
	candidates := DiscoverMCPServers()
	// Should always return at least some candidates (npx packages if npx exists)
	if len(candidates) == 0 {
		t.Log("No candidates discovered (npx/pipx may not be installed)")
	} else {
		for _, c := range candidates {
			if c.Entry.Name == "" {
				t.Error("discovered server has empty name")
			}
			if c.Confidence < 0 || c.Confidence > 1 {
				t.Errorf("confidence %f out of range [0,1]", c.Confidence)
			}
		}
	}
}

func TestDiscoverFromEnv(t *testing.T) {
	os.Setenv("MG_MCP_SERVERS", "npx -y @modelcontextprotocol/server-filesystem, http://localhost:8765/mcp")
	defer os.Unsetenv("MG_MCP_SERVERS")

	candidates := discoverFromEnv()
	if len(candidates) != 2 {
		t.Fatalf("expected 2 candidates from env, got %d: %+v", len(candidates), candidates)
	}
	if candidates[0].Entry.Name != "npx" {
		t.Errorf("expected name 'npx', got %q", candidates[0].Entry.Name)
	}
	if candidates[1].Entry.Type != "http" {
		t.Errorf("expected type 'http', got %q", candidates[1].Entry.Type)
	}
	if candidates[1].Entry.URL != "http://localhost:8765/mcp" {
		t.Errorf("expected URL 'http://localhost:8765/mcp', got %q", candidates[1].Entry.URL)
	}
}

func TestDiscoverFromEnvEmpty(t *testing.T) {
	os.Unsetenv("MG_MCP_SERVERS")
	os.Unsetenv("MCP_SERVERS")

	candidates := discoverFromEnv()
	if len(candidates) != 0 {
		t.Fatalf("expected 0 candidates, got %d", len(candidates))
	}
}

func TestParseServerString(t *testing.T) {
	// HTTP URL
	e := parseServerString("https://example.com/mcp")
	if e.Type != "http" || e.URL != "https://example.com/mcp" {
		t.Errorf("expected http type, got %+v", e)
	}

	// Command with args
	e = parseServerString("npx -y @mcp/server-fs")
	if e.Command != "npx" || len(e.Args) != 2 || e.Args[0] != "-y" || e.Args[1] != "@mcp/server-fs" {
		t.Errorf("expected command with args, got %+v", e)
	}

	// Simple command
	e = parseServerString("python server.py")
	if e.Command != "python" || len(e.Args) != 1 || e.Args[0] != "server.py" {
		t.Errorf("expected python server.py, got %+v", e)
	}
}

func TestSanitizeName(t *testing.T) {
	if sanitizeName("npx -y @mcp/server-fs") != "npx-y-mcp-server-fs" {
		t.Errorf("unexpected sanitized name: %q", sanitizeName("npx -y @mcp/server-fs"))
	}
	if sanitizeName("https://example.com/mcp") != "https-example-com-mcp" {
		t.Errorf("unexpected sanitized name: %q", sanitizeName("https://example.com/mcp"))
	}
	if sanitizeName("!!!") != "mcp-server" {
		t.Errorf("expected fallback name, got %q", sanitizeName("!!!"))
	}
}

func TestDiscoverFromWorkspace(t *testing.T) {
	// Create a temp workspace with .mcp.json
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	defer os.Chdir(origDir)
	os.Chdir(dir)

	configJSON := `{"servers": [{"name": "workspace-fs", "command": "npx", "args": ["-y", "@modelcontextprotocol/server-filesystem"], "type": "stdio", "enabled": true}]}`
	if err := os.WriteFile(filepath.Join(dir, ".mcp.json"), []byte(configJSON), 0644); err != nil {
		t.Fatal(err)
	}

	candidates := discoverFromWorkspace()
	if len(candidates) != 1 {
		t.Fatalf("expected 1 candidate, got %d", len(candidates))
	}
	if candidates[0].Entry.Name != "workspace-fs" {
		t.Errorf("expected name 'workspace-fs', got %q", candidates[0].Entry.Name)
	}
	if candidates[0].Source != srcWorkspace {
		t.Errorf("expected source workspace, got %q", candidates[0].Source)
	}
}

func TestDiscoverFromUserConfig(t *testing.T) {
	home := t.TempDir()
	origHome := os.Getenv("HOME")
	origUserProfile := os.Getenv("USERPROFILE")
	os.Setenv("HOME", home)
	os.Setenv("USERPROFILE", home)
	defer os.Setenv("HOME", origHome)
	defer os.Setenv("USERPROFILE", origUserProfile)

	configDir := filepath.Join(home, ".microgravity")
	os.MkdirAll(configDir, 0755)

	configJSON := `{"servers": [{"name": "user-fs", "command": "pipx", "args": ["run", "mcp-server-filesystem"], "type": "stdio", "enabled": true}]}`
	if err := os.WriteFile(filepath.Join(configDir, "mcp-servers.json"), []byte(configJSON), 0644); err != nil {
		t.Fatal(err)
	}

	candidates := discoverFromUserConfig()
	if len(candidates) != 1 {
		t.Fatalf("expected 1 candidate, got %d", len(candidates))
	}
	if candidates[0].Entry.Name != "user-fs" {
		t.Errorf("expected name 'user-fs', got %q", candidates[0].Entry.Name)
	}
	if candidates[0].Source != srcUserConfig {
		t.Errorf("expected source user-config, got %q", candidates[0].Source)
	}
}

func TestApproveDiscovery(t *testing.T) {
	// Use a temp config dir
	home := t.TempDir()
	os.Setenv("HOME", home)
	defer os.Unsetenv("HOME")

	configDir := filepath.Join(home, ".microgravity")
	os.MkdirAll(configDir, 0755)

	// No existing servers
	candidates := []candidateServer{
		{
			Entry: MCPServerEntry{
				Name:    "test-server",
				Command: "npx",
				Args:    []string{"-y", "@test/server"},
				Type:    "stdio",
				Enabled: true,
			},
			Source:    srcNpx,
			Reason:    "test",
			Confidence: 0.8,
		},
	}

	added, err := ApproveDiscovery(candidates)
	if err != nil {
		t.Fatal(err)
	}
	if added != 1 {
		t.Fatalf("expected 1 added, got %d", added)
	}

	// Adding again should skip (already exists)
	added, err = ApproveDiscovery(candidates)
	if err != nil {
		t.Fatal(err)
	}
	if added != 0 {
		t.Fatalf("expected 0 added (duplicate), got %d", added)
	}

	// Verify it's in the config
	servers := GetMCPServersDetailed()
	found := false
	for _, s := range servers {
		if s.Name == "test-server" {
			found = true
		}
	}
	if !found {
		t.Error("test-server not found in config after approval")
	}
}

func TestDiscoverDeduplication(t *testing.T) {
	// Set env var with same server as npx would discover
	os.Setenv("MG_MCP_SERVERS", "npx -y @modelcontextprotocol/server-filesystem")
	defer os.Unsetenv("MG_MCP_SERVERS")

	candidates := DiscoverMCPServers()
	names := make(map[string]bool)
	for _, c := range candidates {
		if names[c.Entry.Name] {
			t.Errorf("duplicate server name discovered: %s", c.Entry.Name)
		}
		names[c.Entry.Name] = true
	}
	// filesystem should only appear once
	count := 0
	for _, c := range candidates {
		if strings.Contains(c.Entry.Name, "filesystem") {
			count++
		}
	}
	if count > 1 {
		t.Errorf("expected at most 1 filesystem server, got %d", count)
	}
}

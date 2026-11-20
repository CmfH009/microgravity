package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/oauth2"
)

type MCPServerEntry struct {
	Name    string            `json:"name"`
	Command string            `json:"command,omitempty"`
	Args    []string          `json:"args,omitempty"`
	URL     string            `json:"url,omitempty"`
	Type    string            `json:"type,omitempty"` // "stdio" or "http"
	Headers map[string]string `json:"headers,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
	Enabled bool              `json:"enabled"`
}

type Config struct {
	Token              *oauth2.Token    `json:"token,omitempty"`
	AutoAPIKey         string           `json:"auto_api_key,omitempty"`
	AuthType           string           `json:"auth_type,omitempty"`            // "studio", "vertex-express", "vertex-standard"
	VertexProjectID    string           `json:"vertex_project_id,omitempty"`    // GCP Project ID for Vertex AI
	VertexLocation     string           `json:"vertex_location,omitempty"`      // Vertex AI region e.g. "us-central1"
	VertexAPIKey       string           `json:"vertex_api_key,omitempty"`       // Express Mode API key for Vertex AI
	MCPServers         []string         `json:"mcp_servers,omitempty"`
	MCPServersDetailed []MCPServerEntry `json:"mcp_servers_detailed,omitempty"`
	ActiveModel        string           `json:"active_model,omitempty"`
	RoutingMode        string           `json:"routing_mode,omitempty"`
	LocalFastModel     string           `json:"local_fast_model,omitempty"`
	LocalHeavyModel    string           `json:"local_heavy_model,omitempty"`
	OpenAIBaseURL      string           `json:"openai_base_url,omitempty"`
	OpenAIAPIKey       string           `json:"openai_api_key,omitempty"`
	OpenAIModel        string           `json:"openai_model,omitempty"`
	Theme              string           `json:"theme,omitempty"`
}

func GetConfigPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(home, ".microgravity")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.json"), nil
}

func SaveConfig(cfg *Config) error {
	path, err := GetConfigPath()
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".config-*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if err := tmp.Chmod(0600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return err
	}
	return nil
}

func LoadConfig() (*Config, error) {
	path, err := GetConfigPath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	var cfg Config
	if err == nil {
		if err := json.Unmarshal(data, &cfg); err != nil {
			return nil, fmt.Errorf("parse config: %w", err)
		}
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("read config: %w", err)
	}

	if cfg.Token == nil {
		if home, errHome := os.UserHomeDir(); errHome == nil {
			geminiCreds := filepath.Join(home, ".gemini", "oauth_creds.json")
			if gData, errG := os.ReadFile(geminiCreds); errG == nil {
				var gMap struct {
					AccessToken  string `json:"access_token"`
					RefreshToken string `json:"refresh_token"`
					TokenType    string `json:"token_type"`
				}
				if errU := json.Unmarshal(gData, &gMap); errU == nil && gMap.RefreshToken != "" {
					cfg.Token = &oauth2.Token{
						AccessToken:  gMap.AccessToken,
						RefreshToken: gMap.RefreshToken,
						TokenType:    gMap.TokenType,
					}
				}
			}
		}
	}

	return &cfg, nil
}

func GetToken() *oauth2.Token {
	cfg, err := LoadConfig()
	if err == nil {
		return cfg.Token
	}
	return nil
}

func GetAutoAPIKey() string {
	cfg, err := LoadConfig()
	if err == nil {
		return cfg.AutoAPIKey
	}
	return ""
}

func SetAutoAPIKey(key string) error {
	cfg, err := LoadConfig()
	if err != nil {
		cfg = &Config{}
	}
	cfg.AutoAPIKey = key
	return SaveConfig(cfg)
}

func GetAuthType() string {
	cfg, err := LoadConfig()
	if err == nil && cfg.AuthType != "" {
		return cfg.AuthType
	}
	return "studio"
}

func GetVertexConfig() (authType, projectID, location, apiKey string) {
	cfg, err := LoadConfig()
	if err != nil {
		return "studio", "", "us-central1", ""
	}
	aType := cfg.AuthType
	if aType == "" {
		aType = "studio"
	}
	loc := cfg.VertexLocation
	if loc == "" {
		loc = "us-central1"
	}
	return aType, cfg.VertexProjectID, loc, cfg.VertexAPIKey
}

func SetVertexConfig(authType, projectID, location, apiKey string) error {
	cfg, err := LoadConfig()
	if err != nil {
		cfg = &Config{}
	}
	cfg.AuthType = authType
	cfg.VertexProjectID = projectID
	if location != "" {
		cfg.VertexLocation = location
	} else if cfg.VertexLocation == "" {
		cfg.VertexLocation = "us-central1"
	}
	if apiKey != "" {
		cfg.VertexAPIKey = apiKey
	}
	return SaveConfig(cfg)
}

func GetActiveModel() string {
	cfg, err := LoadConfig()
	if err == nil && cfg.ActiveModel != "" {
		return cfg.ActiveModel
	}
	return "gemini-3.6-flash" // Default AGY model tier
}

func SetActiveModel(modelID string) error {
	cfg, err := LoadConfig()
	if err != nil {
		cfg = &Config{}
	}
	cfg.ActiveModel = modelID
	return SaveConfig(cfg)
}

func normalizeRoutingMode(mode string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "local-only", "local", "offline":
		return "local-only", true
	case "hybrid", "auto", "":
		return "hybrid", true
	case "cloud-only", "cloud":
		return "cloud-only", true
	default:
		return "", false
	}
}

// GetRoutingMode returns the persisted routing mode ("local-only", "hybrid", or "cloud-only"),
// defaulting to "hybrid" when unset.
func GetRoutingMode() string {
	cfg, err := LoadConfig()
	if err == nil {
		if norm, ok := normalizeRoutingMode(cfg.RoutingMode); ok && cfg.RoutingMode != "" {
			return norm
		}
	}
	return "hybrid"
}

// SetRoutingMode validates and persists the active routing mode ("local-only", "hybrid", or "cloud-only").
func SetRoutingMode(mode string) error {
	if strings.TrimSpace(mode) == "" {
		return fmt.Errorf("routing mode cannot be empty (expected local-only, hybrid, or cloud-only)")
	}
	norm, ok := normalizeRoutingMode(mode)
	if !ok {
		return fmt.Errorf("invalid routing mode %q: expected local-only, hybrid, or cloud-only", mode)
	}
	cfg, err := LoadConfig()
	if err != nil {
		cfg = &Config{}
	}
	cfg.RoutingMode = norm
	return SaveConfig(cfg)
}

// GetTheme returns the persisted UI theme name, defaulting to "dark" when unset.
func GetTheme() string {
	cfg, err := LoadConfig()
	if err == nil && strings.TrimSpace(cfg.Theme) != "" {
		return strings.ToLower(strings.TrimSpace(cfg.Theme))
	}
	return "dark"
}

// SetTheme persists the active UI theme name.
func SetTheme(theme string) error {
	clean := strings.ToLower(strings.TrimSpace(theme))
	if clean == "" {
		return fmt.Errorf("theme name cannot be empty")
	}
	cfg, err := LoadConfig()
	if err != nil {
		cfg = &Config{}
	}
	cfg.Theme = clean
	return SaveConfig(cfg)
}

func GetSkillsDir() string {
	path, err := GetConfigPath()
	if err != nil {
		return ""
	}
	skillsDir := filepath.Join(filepath.Dir(path), "skills")
	os.MkdirAll(skillsDir, 0755)
	return skillsDir
}

func GetMCPServers() []string {
	cfg, err := LoadConfig()
	if err != nil {
		return nil
	}

	var servers []string
	// Convert legacy string servers
	for _, s := range cfg.MCPServers {
		if s != "" {
			servers = append(servers, s)
		}
	}

	// Include detailed enabled servers
	for _, d := range cfg.MCPServersDetailed {
		if d.Enabled {
			if d.URL != "" {
				servers = append(servers, d.URL)
			} else if d.Command != "" {
				cmdStr := d.Command
				if len(d.Args) > 0 {
					cmdStr += " " + strings.Join(d.Args, " ")
				}
				servers = append(servers, cmdStr)
			}
		}
	}

	return servers
}

// DetectOrbitMCPServer returns the execution command for Orbit's stdlib MCP server if present on disk.
func DetectOrbitMCPServer() string {
	var candidates []string
	if script := strings.TrimSpace(os.Getenv("MG_ORBIT_MCP_SCRIPT")); script != "" {
		candidates = append(candidates, script)
	}
	if mgHome := strings.TrimSpace(os.Getenv("MG_HOME")); mgHome != "" {
		candidates = append(candidates,
			filepath.Join(mgHome, "scripts", "orbit_mcp_server.py"),
			filepath.Join(mgHome, "system", "scripts", "orbit_mcp_server.py"),
		)
	}
	if agyHome := strings.TrimSpace(os.Getenv("AGY_HOME")); agyHome != "" {
		candidates = append(candidates,
			filepath.Join(agyHome, "system", "scripts", "orbit_mcp_server.py"),
			filepath.Join(agyHome, "scripts", "orbit_mcp_server.py"),
		)
	}
	if home, err := os.UserHomeDir(); err == nil && strings.TrimSpace(home) != "" {
		candidates = append(candidates,
			filepath.Join(home, ".microgravity", "scripts", "orbit_mcp_server.py"),
			filepath.Join(home, ".gemini", "antigravity-cli", "scripts", "orbit_mcp_server.py"),
		)
	}

	for _, p := range candidates {
		if p == "" {
			continue
		}
		if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
			return "python " + p
		}
	}
	return ""
}

func GetMCPServersDetailed() []MCPServerEntry {
	cfg, err := LoadConfig()
	if err != nil {
		return nil
	}

	var list []MCPServerEntry
	// Include detailed entries
	list = append(list, cfg.MCPServersDetailed...)

	// Include legacy entries if not already represented
	for _, s := range cfg.MCPServers {
		if s == "" {
			continue
		}
		found := false
		for _, d := range list {
			fullCmd := d.Command
			if len(d.Args) > 0 {
				fullCmd += " " + strings.Join(d.Args, " ")
			}
			if fullCmd == s || d.URL == s || d.Name == s {
				found = true
				break
			}
		}
		if !found {
			parts := strings.Fields(s)
			name := "server"
			if len(parts) > 0 {
				name = parts[0]
			}
			list = append(list, MCPServerEntry{
				Name:    name,
				Command: parts[0],
				Args:    parts[1:],
				Type:    "stdio",
				Enabled: true,
			})
		}
	}

	return list
}

func AddMCPServer(entry MCPServerEntry) error {
	cfg, err := LoadConfig()
	if err != nil {
		cfg = &Config{}
	}

	for i, existing := range cfg.MCPServersDetailed {
		if existing.Name == entry.Name {
			cfg.MCPServersDetailed[i] = entry
			return SaveConfig(cfg)
		}
	}

	cfg.MCPServersDetailed = append(cfg.MCPServersDetailed, entry)
	return SaveConfig(cfg)
}

func RemoveMCPServer(name string) error {
	cfg, err := LoadConfig()
	if err != nil {
		return err
	}

	var updated []MCPServerEntry
	found := false
	for _, s := range cfg.MCPServersDetailed {
		if s.Name == name {
			found = true
		} else {
			updated = append(updated, s)
		}
	}

	var updatedLegacy []string
	for _, s := range cfg.MCPServers {
		if !strings.Contains(s, name) {
			updatedLegacy = append(updatedLegacy, s)
		} else {
			found = true
		}
	}

	if !found {
		return fmt.Errorf("mcp server %q not found", name)
	}

	cfg.MCPServersDetailed = updated
	cfg.MCPServers = updatedLegacy
	return SaveConfig(cfg)
}

func EnableMCPServer(name string) error {
	cfg, err := LoadConfig()
	if err != nil {
		return err
	}

	found := false
	for i, s := range cfg.MCPServersDetailed {
		if s.Name == name {
			cfg.MCPServersDetailed[i].Enabled = true
			found = true
			break
		}
	}

	if !found {
		return fmt.Errorf("mcp server %q not found", name)
	}

	return SaveConfig(cfg)
}

func DisableMCPServer(name string) error {
	cfg, err := LoadConfig()
	if err != nil {
		return err
	}

	found := false
	for i, s := range cfg.MCPServersDetailed {
		if s.Name == name {
			cfg.MCPServersDetailed[i].Enabled = false
			found = true
			break
		}
	}

	if !found {
		return fmt.Errorf("mcp server %q not found", name)
	}

	return SaveConfig(cfg)
}

package plugins

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Manifest represents a plugin.json file.
type Manifest struct {
	Name        string      `json:"name"`
	Version     string      `json:"version,omitempty"`
	Description string      `json:"description,omitempty"`
	Author      string      `json:"author,omitempty"`
	Skills      interface{} `json:"skills,omitempty"`
	Rules       interface{} `json:"rules,omitempty"`
	Hooks       interface{} `json:"hooks,omitempty"`
	MCPServers  interface{} `json:"mcp_servers,omitempty"`
}

// PluginInfo describes an installed or discovered plugin.
type PluginInfo struct {
	Name        string    `json:"name"`
	Version     string    `json:"version,omitempty"`
	Description string    `json:"description,omitempty"`
	Source      string    `json:"source,omitempty"`
	ImportedAt  string    `json:"importedAt,omitempty"`
	Components  []string  `json:"components"`
	Enabled     bool      `json:"enabled"`
	Path        string    `json:"path"`
}

// PluginsConfig represents plugins.json tracking imports and active state.
type PluginsConfig struct {
	Imports  []PluginImport   `json:"imports"`
	Disabled map[string]bool  `json:"disabled,omitempty"`
}

type PluginImport struct {
	Name        string   `json:"name"`
	Source      string   `json:"source"`
	ImportedAt  string   `json:"importedAt"`
	Components  []string `json:"components"`
}

// LoadManifest reads and parses a plugin.json file from a directory.
func LoadManifest(dir string) (*Manifest, error) {
	manifestPath := filepath.Join(dir, "plugin.json")
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return nil, fmt.Errorf("missing plugin.json: %w", err)
	}

	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("invalid plugin.json: %w", err)
	}

	if m.Name == "" {
		m.Name = filepath.Base(dir)
	}
	return &m, nil
}

// DetectComponents inspects the plugin directory for components.
func DetectComponents(dir string) []string {
	var comps []string
	if stat, err := os.Stat(filepath.Join(dir, "skills")); err == nil && stat.IsDir() {
		comps = append(comps, "skills")
	}
	if stat, err := os.Stat(filepath.Join(dir, "rules")); err == nil && stat.IsDir() {
		comps = append(comps, "rules")
	}
	if _, err := os.Stat(filepath.Join(dir, "hooks.json")); err == nil {
		comps = append(comps, "hooks")
	}
	if _, err := os.Stat(filepath.Join(dir, "mcp_config.json")); err == nil {
		comps = append(comps, "mcp")
	}
	return comps
}

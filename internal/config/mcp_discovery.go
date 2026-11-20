// Package config provides MCP server auto-discovery.
//
// Discovery sources (checked in order):
//   1. MG_MCP_SERVERS / MCP_SERVERS environment variable (comma-separated
//      server commands or URLs)
//   2. Workspace .mcp.json or mcp.config.json (project-local definitions)
//   3. ~/.microgravity/mcp-servers.json (user-level definitions)
//   4. Common npx-based @modelcontextprotocol/server-* packages
//   5. Common pipx-based mcp-server-* packages
//
// Discovered servers are returned as MCPServerEntry values but are NOT
// automatically added to the config — the operator reviews and approves
// them via `mg mcp discover` (which lists candidates and interactively
// adds selected ones) or `mg mcp add`.
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// mcpDiscoverySource describes where a discovered server came from.
type mcpDiscoverySource string

const (
	srcEnv        mcpDiscoverySource = "env"
	srcWorkspace  mcpDiscoverySource = "workspace"
	srcUserConfig mcpDiscoverySource = "user-config"
	srcNpx        mcpDiscoverySource = "npx"
	srcPipx       mcpDiscoverySource = "pipx"
)

// candidateServer is a discovered MCP server candidate before approval.
type candidateServer struct {
	Entry   MCPServerEntry
	Source  mcpDiscoverySource
	Reason  string
	Confidence float64 // 0.0-1.0, how confident we are this is a real MCP server
}

// DiscoverMCPServers scans the environment for candidate MCP servers.
// It returns candidates without modifying any config. Callers can present
// them to the user for approval.
func DiscoverMCPServers() []candidateServer {
	var candidates []candidateServer

	// 1. Environment variable
	candidates = append(candidates, discoverFromEnv()...)

	// 2. Workspace config files
	candidates = append(candidates, discoverFromWorkspace()...)

	// 3. User-level config file
	candidates = append(candidates, discoverFromUserConfig()...)

	// 4. npx-based @modelcontextprotocol/server-* packages
	candidates = append(candidates, discoverFromNpx()...)

	// 5. pipx-based mcp-server-* packages
	candidates = append(candidates, discoverFromPipx()...)

	// Deduplicate by entry name
	seen := make(map[string]bool)
	var deduped []candidateServer
	for _, c := range candidates {
		if !seen[c.Entry.Name] {
			seen[c.Entry.Name] = true
			deduped = append(deduped, c)
		}
	}
	return deduped
}

// discoverFromEnv reads MG_MCP_SERVERS or MCP_SERVERS env var.
func discoverFromEnv() []candidateServer {
	for _, envKey := range []string{"MG_MCP_SERVERS", "MCP_SERVERS"} {
		v := os.Getenv(envKey)
		if v == "" {
			continue
		}
		var out []candidateServer
		for _, part := range strings.Split(v, ",") {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			entry := parseServerString(part)
			entry.Name = sanitizeName(entry.Name)
			out = append(out, candidateServer{
				Entry:     entry,
				Source:    srcEnv,
				Reason:    fmt.Sprintf("declared via %s environment variable", envKey),
				Confidence: 0.9,
			})
		}
		return out
	}
	return nil
}

// discoverFromWorkspace reads .mcp.json or mcp.config.json from CWD.
func discoverFromWorkspace() []candidateServer {
	cwd, err := os.Getwd()
	if err != nil {
		return nil
	}
	for _, name := range []string{".mcp.json", "mcp.config.json"} {
		path := filepath.Join(cwd, name)
		if fi, err := os.Stat(path); err == nil && !fi.IsDir() {
			data, err := os.ReadFile(path)
			if err != nil {
				continue
			}
			var doc struct {
				Servers []MCPServerEntry `json:"servers"`
			}
			if err := json.Unmarshal(data, &doc); err != nil {
				continue
			}
			var out []candidateServer
			for _, e := range doc.Servers {
				if e.Name == "" {
					e.Name = sanitizeName(e.Command + " " + strings.Join(e.Args, " "))
				}
				out = append(out, candidateServer{
					Entry:     e,
					Source:    srcWorkspace,
					Reason:    fmt.Sprintf("found in workspace %s", name),
					Confidence: 0.95,
				})
			}
			return out
		}
	}
	return nil
}

// discoverFromUserConfig reads ~/.microgravity/mcp-servers.json.
func discoverFromUserConfig() []candidateServer {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	path := filepath.Join(home, ".microgravity", "mcp-servers.json")
	if fi, err := os.Stat(path); err == nil && !fi.IsDir() {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		var doc struct {
			Servers []MCPServerEntry `json:"servers"`
		}
		if err := json.Unmarshal(data, &doc); err != nil {
			return nil
		}
		var out []candidateServer
		for _, e := range doc.Servers {
			if e.Name == "" {
				e.Name = sanitizeName(e.Command + " " + strings.Join(e.Args, " "))
			}
			out = append(out, candidateServer{
				Entry:     e,
				Source:    srcUserConfig,
				Reason:    "found in user config ~/.microgravity/mcp-servers.json",
				Confidence: 0.9,
			})
		}
		return out
	}
	return nil
}

// discoverFromNpx checks for common @modelcontextprotocol/server-* packages
// available via npx.
func discoverFromNpx() []candidateServer {
	if _, err := exec.LookPath("npx"); err != nil {
		return nil
	}

	knownPackages := []struct {
		pkg   string
		name  string
		desc  string
		conf  float64
	}{
		{"@modelcontextprotocol/server-filesystem", "filesystem", "Local file system access", 0.85},
		{"@modelcontextprotocol/server-memory", "memory", "Knowledge graph memory", 0.8},
		{"@modelcontextprotocol/server-git", "git", "Git repository operations", 0.85},
		{"@modelcontextprotocol/server-postgres", "postgres", "PostgreSQL database access", 0.8},
		{"@modelcontextprotocol/server-fetch", "fetch", "HTTP URL fetching", 0.75},
		{"@modelcontextprotocol/server-brave-search", "brave-search", "Web search via Brave", 0.7},
	}

	var out []candidateServer
	for _, p := range knownPackages {
		entry := MCPServerEntry{
			Name:    p.name,
			Command: "npx",
			Args:    []string{"-y", p.pkg},
			Type:    "stdio",
			Enabled: true,
		}
		out = append(out, candidateServer{
			Entry:     entry,
			Source:    srcNpx,
			Reason:    fmt.Sprintf("npx package %s available (%s)", p.pkg, p.desc),
			Confidence: p.conf,
		})
	}
	return out
}

// discoverFromPipx checks for common mcp-server-* packages via pipx.
func discoverFromPipx() []candidateServer {
	if _, err := exec.LookPath("pipx"); err != nil {
		return nil
	}

	knownPackages := []struct {
		pkg   string
		name  string
		desc  string
		conf  float64
	}{
		{"mcp-server-filesystem", "filesystem-pipx", "Local file system access (pipx)", 0.8},
		{"mcp-server-memory", "memory-pipx", "Knowledge graph memory (pipx)", 0.75},
	}

	var out []candidateServer
	for _, p := range knownPackages {
		entry := MCPServerEntry{
			Name:    p.name,
			Command: "pipx",
			Args:    []string{"run", p.pkg},
			Type:    "stdio",
			Enabled: true,
		}
		out = append(out, candidateServer{
			Entry:     entry,
			Source:    srcPipx,
			Reason:    fmt.Sprintf("pipx package %s available (%s)", p.pkg, p.desc),
			Confidence: p.conf,
		})
	}
	return out
}

// parseServerString parses a "command arg1 arg2" or "http://..." string into
// an MCPServerEntry.
func parseServerString(s string) MCPServerEntry {
	if strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://") {
		return MCPServerEntry{
			Name:  sanitizeName(s),
			Type:  "http",
			URL:   s,
			Enabled: true,
		}
	}
	parts := strings.Fields(s)
	if len(parts) == 0 {
		return MCPServerEntry{}
	}
	return MCPServerEntry{
		Name:    sanitizeName(parts[0]),
		Command: parts[0],
		Args:    parts[1:],
		Type:    "stdio",
		Enabled: true,
	}
}

// sanitizeName creates a valid MCP server name from a command string.
func sanitizeName(s string) string {
	s = strings.TrimSpace(s)
	// Replace non-alphanumeric with dashes, then collapse consecutive dashes
	var b strings.Builder
	prevDash := false
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			prevDash = false
		} else if !prevDash {
			b.WriteRune('-')
			prevDash = true
		}
	}
	name := strings.Trim(b.String(), "-")
	if name == "" {
		name = "mcp-server"
	}
	return name
}

// ApproveDiscovery adds the given candidate servers to the config, skipping
// any that already exist by name.
func ApproveDiscovery(candidates []candidateServer) (added int, err error) {
	for _, c := range candidates {
		// Check if already exists
		existing := GetMCPServersDetailed()
		exists := false
		for _, e := range existing {
			if e.Name == c.Entry.Name {
				exists = true
				break
			}
		}
		if exists {
			continue
		}
		if err := AddMCPServer(c.Entry); err != nil {
			return added, err
		}
		added++
	}
	return added, nil
}

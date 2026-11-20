package policy

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Policy controls filesystem and host-command access for agent tools.
type Policy struct {
	roots             []string
	allowHostCommands bool
	sandboxEnabled    bool

	// Fine-grained tool-level rules.
	denyTools      map[string]bool
	allowTools     map[string]bool
	denyPathPatterns []string
}

// PolicyFile is the JSON configuration for fine-grained tool policy.
type PolicyFile struct {
	DenyTools          []string `json:"deny_tools"`
	AllowTools         []string `json:"allow_tools"`
	DenyPathPatterns   []string `json:"deny_path_patterns"`
	AllowPathPatterns  []string `json:"allow_path_patterns"`
}

// New creates a Policy with filesystem roots and host-command/sandbox settings.
func New(roots []string, allowHostCommands, sandboxEnabled bool) *Policy {
	cleanRoots := make([]string, 0, len(roots))
	for _, root := range roots {
		if abs, err := filepath.Abs(root); err == nil {
			cleanRoots = append(cleanRoots, filepath.Clean(abs))
		}
	}
	return &Policy{
		roots:             cleanRoots,
		allowHostCommands: allowHostCommands,
		sandboxEnabled:    sandboxEnabled,
	}
}

// WithToolRules overlays fine-grained tool allow/deny rules onto the policy.
// Returns the same receiver for chaining.
func (p *Policy) WithToolRules(denyTools, allowTools []string, denyPathPatterns []string) *Policy {
	p.denyTools = make(map[string]bool)
	for _, t := range denyTools {
		p.denyTools[strings.TrimSpace(t)] = true
	}
	p.allowTools = make(map[string]bool)
	for _, t := range allowTools {
		p.allowTools[strings.TrimSpace(t)] = true
	}
	p.denyPathPatterns = append([]string{}, denyPathPatterns...)
	return p
}

// CheckTool returns an error if the named tool is denied by policy.
// If an explicit allow list is configured, tools not in it are denied.
func (p *Policy) CheckTool(toolName string) error {
	if p == nil {
		return fmt.Errorf("execution policy is not configured")
	}

	// Explicit deny list takes precedence.
	if p.denyTools[toolName] {
		return fmt.Errorf("tool %q is denied by policy", toolName)
	}

	// If an allow list is configured, only listed tools are permitted.
	if len(p.allowTools) > 0 && !p.allowTools[toolName] {
		return fmt.Errorf("tool %q is not in the policy allow list", toolName)
	}

	return nil
}

// CheckPathDenied returns an error if the path matches a deny pattern.
func (p *Policy) CheckPathDenied(path string) error {
	if p == nil || len(p.denyPathPatterns) == 0 {
		return nil
	}
	normPath := filepath.ToSlash(path)
	for _, pat := range p.denyPathPatterns {
		normPat := filepath.ToSlash(pat)
		// Simple glob-like match: strip '*' wildcards and check substring.
		// This matches patterns like "*/secrets/*" against any path containing "secrets",
		// or "/etc/*" against any path under /etc.
		simple := strings.ReplaceAll(normPat, "*", "")
		if simple != "" && strings.Contains(normPath, simple) {
			return fmt.Errorf("path %q matches denied pattern %q", path, pat)
		}
		// Also try exact glob match for patterns without wildcards.
		if matched, _ := filepath.Match(normPat, normPath); matched {
			return fmt.Errorf("path %q matches denied pattern %q", path, pat)
		}
	}
	return nil
}

func (p *Policy) SandboxEnabled() bool { return p != nil && p.sandboxEnabled }

func (p *Policy) ResolvePath(path string) (string, error) {
	if p == nil {
		return "", fmt.Errorf("execution policy is not configured")
	}
	if path == "" {
		return "", fmt.Errorf("path is required")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolve path: %w", err)
	}
	abs = filepath.Clean(abs)
	for _, root := range p.roots {
		rel, err := filepath.Rel(root, abs)
		if err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel) {
			if err := p.CheckPathDenied(abs); err != nil {
				return "", err
			}
			return abs, nil
		}
	}
	return "", fmt.Errorf("path %q is outside approved workspace roots", path)
}

func (p *Policy) CheckCommand(command string) error {
	if p == nil || !p.allowHostCommands {
		return fmt.Errorf("host command execution is disabled by policy")
	}
	if p.sandboxEnabled {
		return fmt.Errorf("host command execution is prohibited in sandbox mode")
	}
	if strings.TrimSpace(command) == "" {
		return fmt.Errorf("command is required")
	}
	return nil
}

func (p *Policy) CheckWorkingDirectory(dir string) (string, error) {
	if dir == "" {
		dir, _ = os.Getwd()
	}
	resolved, err := p.ResolvePath(dir)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return "", fmt.Errorf("working directory: %w", err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("working directory is not a directory: %s", resolved)
	}
	return resolved, nil
}

// ShellCommand returns the platform shell invocation for an already-authorized command.
func ShellCommand(command string) *exec.Cmd {
	if os.PathSeparator == '\\' {
		return exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command", command)
	}
	return exec.Command("sh", "-c", command)
}

// ToolRulesFromEnv builds tool allow/deny lists from environment variables:
// MG_POLICY_DENY_TOOLS, MG_POLICY_ALLOW_TOOLS, MG_POLICY_DENY_PATHS
// (all comma-separated).
func ToolRulesFromEnv() (denyTools, allowTools []string, denyPaths []string) {
	if v := os.Getenv("MG_POLICY_DENY_TOOLS"); v != "" {
		for _, t := range strings.Split(v, ",") {
			if t = strings.TrimSpace(t); t != "" {
				denyTools = append(denyTools, t)
			}
		}
	}
	if v := os.Getenv("MG_POLICY_ALLOW_TOOLS"); v != "" {
		for _, t := range strings.Split(v, ",") {
			if t = strings.TrimSpace(t); t != "" {
				allowTools = append(allowTools, t)
			}
		}
	}
	if v := os.Getenv("MG_POLICY_DENY_PATHS"); v != "" {
		for _, p := range strings.Split(v, ",") {
			if p = strings.TrimSpace(p); p != "" {
				denyPaths = append(denyPaths, p)
			}
		}
	}
	return
}

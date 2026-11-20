package policy

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
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
		if root == "" || strings.ContainsRune(root, 0) {
			continue
		}
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
	normPath := filepath.ToSlash(filepath.Clean(path))
	normPathFold := normPath
	if runtime.GOOS == "windows" {
		normPathFold = strings.ToLower(normPath)
	}
	for _, pat := range p.denyPathPatterns {
		normPat := filepath.ToSlash(pat)
		normPatFold := normPat
		if runtime.GOOS == "windows" {
			normPatFold = strings.ToLower(normPat)
		}
		// Simple glob-like match: strip '*' wildcards and check substring.
		// This matches patterns like "*/secrets/*" against any path containing "secrets",
		// or "/etc/*" against any path under /etc.
		simple := strings.ReplaceAll(normPatFold, "*", "")
		if simple != "" && strings.Contains(normPathFold, simple) {
			return fmt.Errorf("path %q matches denied pattern %q", path, pat)
		}
		// Match directory itself when pattern ends with "/*" (e.g. "*/secrets/*" matches ".../secrets").
		if strings.HasSuffix(normPatFold, "/*") {
			dirPart := strings.TrimSuffix(strings.ReplaceAll(strings.TrimSuffix(normPatFold, "/*"), "*", ""), "/")
			if dirPart != "" && (strings.HasSuffix(normPathFold, dirPart) || strings.Contains(normPathFold, dirPart+"/")) {
				return fmt.Errorf("path %q matches denied pattern %q", path, pat)
			}
		}
		// Also try exact glob match.
		if matched, _ := filepath.Match(normPatFold, normPathFold); matched {
			return fmt.Errorf("path %q matches denied pattern %q", path, pat)
		}
		// Match base filename for patterns without directory separators (e.g. "*.env").
		if !strings.Contains(normPatFold, "/") {
			if matched, _ := filepath.Match(normPatFold, filepath.Base(normPathFold)); matched {
				return fmt.Errorf("path %q matches denied pattern %q", path, pat)
			}
		}
	}
	return nil
}

func (p *Policy) SandboxEnabled() bool { return p != nil && p.sandboxEnabled }

const maxSymlinkHops = 40

func validateRawPath(path string) error {
	if path == "" {
		return fmt.Errorf("path is required")
	}
	if strings.ContainsRune(path, 0) {
		return fmt.Errorf("path contains null byte")
	}
	if runtime.GOOS == "windows" {
		slashNorm := strings.ReplaceAll(path, "/", `\`)
		if strings.HasPrefix(slashNorm, `\\?\`) || strings.HasPrefix(slashNorm, `\\.\`) || strings.HasPrefix(slashNorm, `\??\`) {
			return fmt.Errorf("device or extended-length path %q is not allowed", path)
		}
		vol := filepath.VolumeName(path)
		if len(vol) == 2 && vol[1] == ':' {
			if len(path) == 2 || (path[2] != '\\' && path[2] != '/') {
				return fmt.Errorf("drive-relative path %q is not allowed", path)
			}
		}
		rest := path[len(vol):]
		if strings.ContainsRune(rest, ':') {
			return fmt.Errorf("alternate data stream or invalid colon in path %q", path)
		}
		for _, seg := range strings.FieldsFunc(rest, func(r rune) bool { return r == '\\' || r == '/' }) {
			if seg == "" || seg == "." || seg == ".." {
				continue
			}
			if strings.HasSuffix(seg, ".") || strings.HasSuffix(seg, " ") {
				return fmt.Errorf("invalid trailing dot or space in path segment %q", seg)
			}
			if isWindowsReservedName(seg) {
				return fmt.Errorf("reserved Windows device name in path segment %q", seg)
			}
		}
	}
	return nil
}

func isWindowsReservedName(seg string) bool {
	base := seg
	if idx := strings.IndexByte(base, '.'); idx >= 0 {
		base = base[:idx]
	}
	switch strings.ToUpper(base) {
	case "CON", "PRN", "AUX", "NUL",
		"COM1", "COM2", "COM3", "COM4", "COM5", "COM6", "COM7", "COM8", "COM9",
		"LPT1", "LPT2", "LPT3", "LPT4", "LPT5", "LPT6", "LPT7", "LPT8", "LPT9":
		return true
	}
	return false
}

func isWithinRoot(root, candidate string) bool {
	root = filepath.Clean(root)
	candidate = filepath.Clean(candidate)

	if runtime.GOOS == "windows" {
		if !strings.EqualFold(filepath.VolumeName(root), filepath.VolumeName(candidate)) {
			return false
		}
	} else if filepath.VolumeName(root) != filepath.VolumeName(candidate) {
		return false
	}

	rel, err := filepath.Rel(root, candidate)
	if err != nil {
		return false
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return false
	}
	return true
}

func isNotExistOrNotDir(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, os.ErrNotExist) || os.IsNotExist(err) {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "not a directory") || strings.Contains(msg, "cannot find the path")
}

func evalSymlinksOrAncestor(abs string) (string, error) {
	return evalPathComponents(filepath.Clean(abs))
}

func splitVolumeAndParts(cleanPath string) (string, []string) {
	vol := filepath.VolumeName(cleanPath)
	rest := cleanPath[len(vol):]
	sep := string(filepath.Separator)
	root := vol
	if strings.HasPrefix(rest, sep) {
		root += sep
		rest = strings.TrimPrefix(rest, sep)
	} else if vol == "" && strings.HasPrefix(cleanPath, "/") {
		root = "/"
		rest = strings.TrimPrefix(cleanPath, "/")
	}
	if rest == "" {
		return root, nil
	}
	parts := strings.Split(rest, sep)
	filtered := make([]string, 0, len(parts))
	for _, p := range parts {
		if p != "" && p != "." {
			filtered = append(filtered, p)
		}
	}
	return root, filtered
}

func evalPathComponents(abs string) (string, error) {
	resolved, rem := splitVolumeAndParts(abs)
	hops := 0

	for len(rem) > 0 {
		comp := rem[0]
		rem = rem[1:]

		next := filepath.Join(resolved, comp)
		info, err := os.Lstat(next)
		if err != nil {
			if isNotExistOrNotDir(err) {
				// Nearest existing ancestor reached: preserve disk canonical form for resolved, then append remaining tail.
				if canonBase, cerr := filepath.EvalSymlinks(resolved); cerr == nil {
					resolved = canonBase
				}
				tail := append([]string{comp}, rem...)
				return filepath.Clean(filepath.Join(append([]string{resolved}, tail...)...)), nil
			}
			return "", fmt.Errorf("stat path %q: %w", next, err)
		}

		isReparse := info.Mode()&os.ModeSymlink != 0 || (runtime.GOOS == "windows" && info.Mode()&os.ModeIrregular != 0)
		if isReparse {
			hops++
			if hops > maxSymlinkHops {
				return "", fmt.Errorf("too many levels of symbolic links")
			}
			linkTarget, rerr := os.Readlink(next)
			if rerr != nil {
				return "", fmt.Errorf("readlink %q: %w", next, rerr)
			}
			if !filepath.IsAbs(linkTarget) {
				linkTarget = filepath.Join(resolved, linkTarget)
			}
			linkTarget = filepath.Clean(linkTarget)
			if err := validateRawPath(linkTarget); err != nil {
				return "", err
			}
			newRoot, targetParts := splitVolumeAndParts(linkTarget)
			resolved = newRoot
			rem = append(targetParts, rem...)
			continue
		}

		resolved = next
	}

	if canon, err := filepath.EvalSymlinks(resolved); err == nil {
		return filepath.Clean(canon), nil
	}
	return filepath.Clean(resolved), nil
}

func pathsEqualOS(a, b string) bool {
	if runtime.GOOS == "windows" {
		return strings.EqualFold(filepath.Clean(a), filepath.Clean(b))
	}
	return filepath.Clean(a) == filepath.Clean(b)
}

func (p *Policy) ResolvePath(path string) (string, error) {
	if p == nil {
		return "", fmt.Errorf("execution policy is not configured")
	}
	if err := validateRawPath(path); err != nil {
		return "", err
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolve path: %w", err)
	}
	abs = filepath.Clean(abs)

	lexicalMatchIdx := -1
	for i, root := range p.roots {
		if isWithinRoot(root, abs) {
			lexicalMatchIdx = i
			break
		}
	}
	if lexicalMatchIdx < 0 {
		return "", fmt.Errorf("path %q is outside approved workspace roots", path)
	}

	canonTarget, err := evalSymlinksOrAncestor(abs)
	if err != nil {
		return "", fmt.Errorf("resolve symlinks for %q: %w", path, err)
	}

	canonMatchIdx := -1
	var matchedCanonRoot string
	for i, root := range p.roots {
		canonRoot, rerr := evalSymlinksOrAncestor(root)
		if rerr != nil {
			continue
		}
		if isWithinRoot(canonRoot, canonTarget) {
			canonMatchIdx = i
			matchedCanonRoot = canonRoot
			break
		}
	}
	if canonMatchIdx < 0 {
		return "", fmt.Errorf("path %q escapes approved workspace roots via symlink", path)
	}

	if err := p.CheckPathDenied(abs); err != nil {
		return "", err
	}
	if err := p.CheckPathDenied(canonTarget); err != nil {
		return "", err
	}

	if canonMatchIdx == lexicalMatchIdx {
		matchedRoot := p.roots[lexicalMatchIdx]
		relLex, errLex := filepath.Rel(matchedRoot, abs)
		relCanon, errCanon := filepath.Rel(matchedCanonRoot, canonTarget)
		if errLex == nil && errCanon == nil && pathsEqualOS(relLex, relCanon) {
			return filepath.Join(matchedRoot, relCanon), nil
		}
	}
	return canonTarget, nil
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

package rules

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type Rules struct {
	GlobalContent    string
	UserProfile      string
	AgentProfile     string
	WorkspaceContent string
	AgentsMDContent  string
}

// LoadFromFiles loads rule contents from given file paths if they exist.
func LoadFromFiles(globalPath, workspacePath string) (*Rules, error) {
	r := &Rules{}

	if globalPath != "" {
		if data, err := os.ReadFile(globalPath); err == nil {
			r.GlobalContent = strings.TrimSpace(string(data))
		}
	}

	if workspacePath != "" {
		if data, err := os.ReadFile(workspacePath); err == nil {
			r.WorkspaceContent = strings.TrimSpace(string(data))
		}
	}

	return r, nil
}

// LoadDefaultRules loads rules with portable workspace traversal (.agents/AGENTS.md, user/agent profiles, and rules.md).
func LoadDefaultRules(cwd string) (*Rules, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		home = ""
	}
	r := &Rules{}

	if home != "" {
		// Global rules
		globalPath := filepath.Join(home, ".microgravity", "rules.md")
		if data, err := os.ReadFile(globalPath); err == nil {
			r.GlobalContent = strings.TrimSpace(string(data))
		}
	}

	// User and agent profiles (discovered portably via MG_HOME, AGY_HOME, ~/.microgravity/, ~/.gemini/antigravity-cli/)
	searchDirs := profileSearchDirs(home)
	r.UserProfile = readFirstProfileFile(searchDirs, "user_profile.md", "USER_PROFILE.md")
	r.AgentProfile = readFirstProfileFile(searchDirs, "agent_profile.md", "AGENT_PROFILE.md")

	// Traversal for .agents/AGENTS.md upwards from cwd
	curr := cwd
	for {
		agentsPath := filepath.Join(curr, ".agents", "AGENTS.md")
		if data, err := os.ReadFile(agentsPath); err == nil {
			r.AgentsMDContent = strings.TrimSpace(string(data))
			break
		}
		parent := filepath.Dir(curr)
		if parent == curr || parent == "." {
			break
		}
		curr = parent
	}

	workspacePath := filepath.Join(cwd, ".microgravity", "rules.md")
	if _, err := os.Stat(workspacePath); os.IsNotExist(err) {
		altPath := filepath.Join(cwd, "rules.md")
		if _, errAlt := os.Stat(altPath); errAlt == nil {
			workspacePath = altPath
		}
	}

	if data, err := os.ReadFile(workspacePath); err == nil {
		r.WorkspaceContent = strings.TrimSpace(string(data))
	}

	return r, nil
}

// FormatForSystemPrompt formats loaded rules into a clean prompt block.
func (r *Rules) FormatForSystemPrompt() string {
	if r.GlobalContent == "" && r.WorkspaceContent == "" && r.UserProfile == "" && r.AgentProfile == "" && r.AgentsMDContent == "" {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("\n<USER_RULES>\n")
	sb.WriteString("The following rules take precedence over general instructions. You MUST follow them strictly:\n")

	if r.GlobalContent != "" {
		sb.WriteString(fmt.Sprintf("\n--- Global Rules ---\n%s\n", r.GlobalContent))
	}

	if r.UserProfile != "" {
		sb.WriteString(fmt.Sprintf("\n--- User Context Profile ---\n%s\n", r.UserProfile))
	}

	if r.AgentProfile != "" {
		sb.WriteString(fmt.Sprintf("\n--- Agent Identity Profile ---\n%s\n", r.AgentProfile))
	}

	if r.AgentsMDContent != "" {
		sb.WriteString(fmt.Sprintf("\n--- Project .agents/AGENTS.md Rules ---\n%s\n", r.AgentsMDContent))
	}

	if r.WorkspaceContent != "" {
		sb.WriteString(fmt.Sprintf("\n--- Workspace Rules ---\n%s\n", r.WorkspaceContent))
	}

	sb.WriteString("</USER_RULES>\n")
	return sb.String()
}

func profileSearchDirs(home string) []string {
	var dirs []string
	seen := make(map[string]bool)
	add := func(dir string) {
		dir = strings.TrimSpace(dir)
		if dir == "" {
			return
		}
		clean := filepath.Clean(dir)
		if !seen[clean] {
			seen[clean] = true
			dirs = append(dirs, clean)
		}
	}

	add(os.Getenv("MG_HOME"))
	add(os.Getenv("AGY_HOME"))
	if home != "" {
		add(filepath.Join(home, ".microgravity"))
		add(filepath.Join(home, ".gemini", "antigravity-cli"))
	}
	return dirs
}

func readFirstProfileFile(dirs []string, filenames ...string) string {
	for _, dir := range dirs {
		for _, name := range filenames {
			p := filepath.Join(dir, name)
			info, err := os.Stat(p)
			if err != nil || info.IsDir() {
				continue
			}
			if data, err := os.ReadFile(p); err == nil {
				return strings.TrimSpace(string(data))
			}
		}
	}
	return ""
}

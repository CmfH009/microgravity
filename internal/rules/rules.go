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

// LoadDefaultRules loads rules with full AGY workspace traversal (.agents/AGENTS.md, USER_PROFILE.md, AGENT_PROFILE.md).
func LoadDefaultRules(cwd string) (*Rules, error) {
	home, err := os.UserHomeDir()
	r := &Rules{}

	if err == nil {
		// Global rules
		globalPath := filepath.Join(home, ".microgravity", "rules.md")
		if data, err := os.ReadFile(globalPath); err == nil {
			r.GlobalContent = strings.TrimSpace(string(data))
		}

		// User profile
		userProfPath := filepath.Join(home, ".gemini", "antigravity-cli", "user_profile.md")
		if _, errStat := os.Stat(userProfPath); os.IsNotExist(errStat) {
			userProfPath = filepath.Join("C:\\AgyHut", "USER_PROFILE.md")
		}
		if data, err := os.ReadFile(userProfPath); err == nil {
			r.UserProfile = strings.TrimSpace(string(data))
		}

		// Agent profile
		agentProfPath := filepath.Join("C:\\AgyHut", "AGENT_PROFILE.md")
		if data, err := os.ReadFile(agentProfPath); err == nil {
			r.AgentProfile = strings.TrimSpace(string(data))
		}
	}

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

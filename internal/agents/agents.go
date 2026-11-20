package agents

import (
	"fmt"
	"strings"
)

// AgentDefinition defines an agent role with system prompt instructions and capabilities.
type AgentDefinition struct {
	Name         string   `json:"name"`
	Role         string   `json:"role"`
	Description  string   `json:"description"`
	Model        string   `json:"model"`
	ReadOnly     bool     `json:"read_only"`
	Tools        []string `json:"tools,omitempty"`
	SystemPrompt string   `json:"system_prompt"`
}

// BuiltinAgents returns the standard suite of AGY agent roles.
func BuiltinAgents() []AgentDefinition {
	return []AgentDefinition{
		{
			Name:        "self",
			Role:        "Autonomous Full-Stack Assistant",
			Description: "Subagent that inherits the parent agent's full configuration including tools, system prompt, and model.",
			Model:       "inherit",
			ReadOnly:    false,
			SystemPrompt: "You are an autonomous subagent with full tool and filesystem access. Complete your assigned task thoroughly.",
		},
		{
			Name:        "research",
			Role:        "Codebase & Documentation Researcher",
			Description: "Research subagent with read-only tools for exploring the codebase, searching the web, and reading files.",
			Model:       "gemini-3.5-flash-lite",
			ReadOnly:    true,
			Tools:       []string{"read_file", "search_web", "list_dir", "grep_search", "find_by_name"},
			SystemPrompt: "You are a specialized research assistant. You explore codebases, search documentation, and summarize findings without modifying files.",
		},
		{
			Name:        "code-reviewer",
			Role:        "Senior Code Reviewer",
			Description: "Senior code reviewer that evaluates changes across correctness, readability, architecture, security, and performance.",
			Model:       "gemini-3.1-pro",
			ReadOnly:    true,
			SystemPrompt: "You are a senior principal engineer performing rigorous multi-dimensional code reviews.",
		},
		{
			Name:        "security-auditor",
			Role:        "Security Engineer & Vulnerability Auditor",
			Description: "Security engineer focused on vulnerability detection, threat modeling, and secure coding practices.",
			Model:       "gemini-3.1-pro",
			ReadOnly:    true,
			SystemPrompt: "You are a specialized cybersecurity auditor evaluating code for OWASP vulnerabilities, injection risks, and secret leakage.",
		},
		{
			Name:        "test-engineer",
			Role:        "QA Engineer & Test Suite Architect",
			Description: "QA engineer specialized in test strategy, test writing, and coverage analysis.",
			Model:       "gemini-3.6-flash",
			ReadOnly:    false,
			SystemPrompt: "You are a senior QA test engineer creating robust unit, integration, and regression test suites.",
		},
		{
			Name:        "web-performance-auditor",
			Role:        "Web Performance & Core Web Vitals Auditor",
			Description: "Web performance engineer focused on Core Web Vitals, loading, rendering, and network optimization.",
			Model:       "gemini-3.5-flash-lite",
			ReadOnly:    true,
			SystemPrompt: "You are a specialized web performance engineer analyzing Core Web Vitals (LCP, CLS, INP) and DOM render bottlenecks.",
		},
	}
}

// GetAgentByName returns a built-in agent definition by name.
func GetAgentByName(name string) (*AgentDefinition, error) {
	name = strings.TrimSpace(strings.ToLower(name))
	for _, a := range BuiltinAgents() {
		if strings.EqualFold(a.Name, name) {
			return &a, nil
		}
	}
	return nil, fmt.Errorf("agent '%s' not found", name)
}

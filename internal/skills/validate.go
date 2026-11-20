package skills

import (
	"fmt"
	"strings"
)

// Validate checks a Skill for correctness and returns a list of warnings/errors.
func Validate(s *Skill) []string {
	var issues []string

	if s.Name == "" {
		issues = append(issues, "skill is missing a name")
	}
	if s.Description == "" {
		issues = append(issues, fmt.Sprintf("skill %q is missing a description", s.Name))
	}
	if strings.TrimSpace(s.Body) == "" {
		issues = append(issues, fmt.Sprintf("skill %q has an empty body", s.Name))
	}

	switch s.Type {
	case "tool":
		if s.Command == "" {
			issues = append(issues, fmt.Sprintf("skill %q has type 'tool' but no command", s.Name))
		}
	case "prompt", "":
		// prompt-type skills are injected into the system prompt; no command needed
	default:
		issues = append(issues, fmt.Sprintf("skill %q has unknown type %q (expected 'tool' or 'prompt')", s.Name, s.Type))
	}

	return issues
}

// FormatSkillSummary returns a human-readable one-line summary of a skill.
func FormatSkillSummary(s *Skill) string {
	skillType := s.Type
	if skillType == "" {
		skillType = "prompt"
	}
	desc := s.Description
	if len(desc) > 60 {
		desc = desc[:57] + "..."
	}
	return fmt.Sprintf("  %-20s [%s]  %s", s.Name, skillType, desc)
}

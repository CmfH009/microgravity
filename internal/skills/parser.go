package skills

import (
	"bufio"
	"bytes"
	"errors"
	"io"
	"strings"

	"gopkg.in/yaml.v3"
)

// Skill represents a parsed skill from a markdown file.
type Skill struct {
	Name                string   `yaml:"name"`
	Description         string   `yaml:"description"`
	Type                string   `yaml:"type"`
	Command             string   `yaml:"command"`
	Category            string   `yaml:"category"`
	Tags                []string `yaml:"tags"`
	DisableSlashCommand bool     `yaml:"disable-slash-command"`
	Body                string
}

// ParseSkill reads from an io.Reader and parses a markdown file with YAML frontmatter.
func ParseSkill(r io.Reader) (*Skill, error) {
	scanner := bufio.NewScanner(r)
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 1024*1024)

	// Ensure the file starts with "---"
	if !scanner.Scan() {
		if err := scanner.Err(); err != nil {
			return nil, err
		}
		return nil, errors.New("empty file or missing frontmatter")
	}
	firstLine := scanner.Text()
	if firstLine != "---" {
		return nil, errors.New("missing frontmatter")
	}

	var frontmatter bytes.Buffer
	frontmatterFound := false
	for scanner.Scan() {
		line := scanner.Text()
		if line == "---" {
			frontmatterFound = true
			break
		}
		frontmatter.WriteString(line)
		frontmatter.WriteString("\n")
	}

	if !frontmatterFound {
		if err := scanner.Err(); err != nil {
			return nil, err
		}
		return nil, errors.New("incomplete frontmatter")
	}

	var skill Skill
	if err := yaml.Unmarshal(frontmatter.Bytes(), &skill); err != nil {
		return nil, err
	}

	if skill.Name == "" {
		return nil, errors.New("missing name in frontmatter")
	}

	if skill.Category == "" {
		skill.Category = inferCategory(skill.Name, skill.Description)
	}

	var body bytes.Buffer
	first := true
	for scanner.Scan() {
		if !first {
			body.WriteString("\n")
		}
		body.WriteString(scanner.Text())
		first = false
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	skill.Body = body.String()

	return &skill, nil
}

func inferCategory(name, desc string) string {
	combined := strings.ToLower(name + " " + desc)
	switch {
	case strings.Contains(combined, "test") || strings.Contains(combined, "qa") || strings.Contains(combined, "coverage"):
		return "TESTING"
	case strings.Contains(combined, "debug") || strings.Contains(combined, "error") || strings.Contains(combined, "leak") || strings.Contains(combined, "troubleshoot"):
		return "DEBUGGING"
	case strings.Contains(combined, "git") || strings.Contains(combined, "commit") || strings.Contains(combined, "branch") || strings.Contains(combined, "worktree"):
		return "VERSION CONTROL"
	case strings.Contains(combined, "web") || strings.Contains(combined, "browser") || strings.Contains(combined, "devtools") || strings.Contains(combined, "html") || strings.Contains(combined, "chrome") || strings.Contains(combined, "ui") || strings.Contains(combined, "css"):
		return "WEB DEV"
	case strings.Contains(combined, "agent") || strings.Contains(combined, "swarm") || strings.Contains(combined, "mcp") || strings.Contains(combined, "subagent") || strings.Contains(combined, "antigravity"):
		return "AGENTIC ENGINE"
	case strings.Contains(combined, "security") || strings.Contains(combined, "hardening") || strings.Contains(combined, "audit"):
		return "SECURITY"
	case strings.Contains(combined, "perf") || strings.Contains(combined, "lcp") || strings.Contains(combined, "optimize") || strings.Contains(combined, "speed"):
		return "PERFORMANCE"
	default:
		return "GENERAL"
	}
}

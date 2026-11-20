package ui

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

var (
	ToolCardBoxSuccess = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(RoleColor(RoleSuccess)).
				Padding(0, 1).
				MarginTop(0).
				MarginBottom(1)

	ToolCardBoxFailed = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(RoleColor(RoleError)).
				Padding(0, 1).
				MarginTop(0).
				MarginBottom(1)

	ToolHeaderStyle = lipgloss.NewStyle().
			Foreground(RoleColor(RoleHighlight)).
			Bold(true)

	ToolBadgeSuccess = lipgloss.NewStyle().
				Foreground(lipgloss.Color("15")).
				Background(RoleColor(RoleSuccess)).
				Bold(true).
				Padding(0, 1)

	ToolBadgeFailed = lipgloss.NewStyle().
				Foreground(lipgloss.Color("15")).
				Background(RoleColor(RoleError)).
				Bold(true).
				Padding(0, 1)

	ToolMetaStyle = lipgloss.NewStyle().
			Foreground(RoleColor(RoleTextMuted))

	ToolKeyStyle = lipgloss.NewStyle().
			Foreground(RoleColor(RoleAccent)).
			Bold(true)

	ToolValStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("255"))
)

func getToolIcon(toolName string) string {
	switch {
	case strings.Contains(toolName, "search") || strings.Contains(toolName, "web") || strings.Contains(toolName, "http"):
		return "🌐"
	case strings.Contains(toolName, "file") || strings.Contains(toolName, "read") || strings.Contains(toolName, "write"):
		return "📂"
	case strings.Contains(toolName, "exec") || strings.Contains(toolName, "cmd") || strings.Contains(toolName, "run"):
		return "⚙️"
	case strings.Contains(toolName, "test") || strings.Contains(toolName, "audit") || strings.Contains(toolName, "check"):
		return "🧪"
	case strings.Contains(toolName, "subagent") || strings.Contains(toolName, "agent"):
		return "🤖"
	case strings.Contains(toolName, "git") || strings.Contains(toolName, "worktree"):
		return "🌿"
	default:
		return "🛠️"
	}
}

// RenderToolCallCard renders an Antigravity-style tool call card.
func RenderToolCallCard(toolName string, args map[string]interface{}, duration time.Duration, success bool, output string) string {
	var sb strings.Builder

	icon := getToolIcon(toolName)
	statusText := ToolBadgeSuccess.Render("SUCCESS") + ToolMetaStyle.Render(fmt.Sprintf(" ⏱ %v", duration))
	if !success {
		statusText = ToolBadgeFailed.Render("FAILED") + ToolMetaStyle.Render(fmt.Sprintf(" ⏱ %v", duration))
	}

	header := fmt.Sprintf("%s %s   %s", icon, ToolHeaderStyle.Render(toolName), statusText)
	sb.WriteString(header + "\n")

	// Render args neatly
	if len(args) > 0 {
		sb.WriteString(ToolMetaStyle.Render("Parameters:") + "\n")
		for k, v := range args {
			vBytes, _ := json.Marshal(v)
			sb.WriteString(fmt.Sprintf("  • %s: %s\n", ToolKeyStyle.Render(k), ToolValStyle.Render(string(vBytes))))
		}
	}

	// Output preview (truncated if too long)
	if output != "" {
		sb.WriteString(ToolMetaStyle.Render("Output Preview:") + "\n")
		lines := strings.Split(strings.TrimSpace(output), "\n")
		maxLines := 6
		if len(lines) > maxLines {
			preview := strings.Join(lines[:maxLines], "\n")
			sb.WriteString(preview + "\n" + ToolMetaStyle.Render(fmt.Sprintf("... (%d lines truncated)", len(lines)-maxLines)))
		} else {
			sb.WriteString(strings.Join(lines, "\n"))
		}
	}

	if success {
		return ToolCardBoxSuccess.Render(sb.String())
	}
	return ToolCardBoxFailed.Render(sb.String())
}


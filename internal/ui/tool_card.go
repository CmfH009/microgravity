package ui

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/charmbracelet/lipgloss"
)

var (
	ToolCardBoxSuccess = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(RoleColor(RoleSuccess)).
				Padding(0, 1)

	ToolCardBoxFailed = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(RoleColor(RoleError)).
				Padding(0, 1)

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
			Foreground(RoleColor(RoleText))
)

type toolCardPalette struct {
	boxSuccess   lipgloss.Style
	boxFailed    lipgloss.Style
	header       lipgloss.Style
	badgeSuccess lipgloss.Style
	badgeFailed  lipgloss.Style
	meta         lipgloss.Style
	key          lipgloss.Style
	val          lipgloss.Style
}

func currentToolCardPalette() toolCardPalette {
	successColor := RoleColor(RoleSuccess)
	errorColor := RoleColor(RoleError)
	return toolCardPalette{
		boxSuccess: lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(successColor).
			Padding(0, 1),
		boxFailed: lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(errorColor).
			Padding(0, 1),
		header: lipgloss.NewStyle().
			Foreground(RoleColor(RoleHighlight)).
			Bold(true),
		badgeSuccess: lipgloss.NewStyle().
			Foreground(lipgloss.Color("15")).
			Background(successColor).
			Bold(true).
			Padding(0, 1),
		badgeFailed: lipgloss.NewStyle().
			Foreground(lipgloss.Color("15")).
			Background(errorColor).
			Bold(true).
			Padding(0, 1),
		meta: lipgloss.NewStyle().
			Foreground(RoleColor(RoleTextMuted)),
		key: lipgloss.NewStyle().
			Foreground(RoleColor(RoleAccent)).
			Bold(true),
		val: lipgloss.NewStyle().
			Foreground(RoleColor(RoleText)),
	}
}

func refreshToolCardStyles() {
	p := currentToolCardPalette()
	ToolCardBoxSuccess = p.boxSuccess
	ToolCardBoxFailed = p.boxFailed
	ToolHeaderStyle = p.header
	ToolBadgeSuccess = p.badgeSuccess
	ToolBadgeFailed = p.badgeFailed
	ToolMetaStyle = p.meta
	ToolKeyStyle = p.key
	ToolValStyle = p.val
}

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

// RenderToolCallCard renders an Antigravity-style tool call card in expanded mode.
func RenderToolCallCard(toolName string, args map[string]interface{}, duration time.Duration, success bool, output string) string {
	return RenderCollapsibleToolCard(toolName, args, duration, success, output, true)
}

func formatToolDuration(d time.Duration) string {
	switch {
	case d >= time.Second:
		return d.Round(10 * time.Millisecond).String()
	case d >= time.Millisecond:
		return d.Round(time.Millisecond).String()
	default:
		return d.String()
	}
}

func truncateToolLine(s string, maxRunes int) string {
	s = strings.ReplaceAll(s, "\r\n", " ")
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\r", " ")
	if maxRunes <= 3 || utf8.RuneCountInString(s) <= maxRunes {
		return s
	}
	runes := []rune(s)
	return string(runes[:maxRunes-3]) + "..."
}

// RenderCollapsibleToolCard renders a tool call card either as a compact 1-line bordered
// summary (expanded == false) or with full parameters and output preview (expanded == true).
// All styles are resolved dynamically from RoleColor at render time.
func RenderCollapsibleToolCard(toolName string, args map[string]interface{}, duration time.Duration, success bool, output string, expanded bool) string {
	pal := currentToolCardPalette()
	icon := getToolIcon(toolName)

	statusBadge := pal.badgeSuccess.Render("SUCCESS")
	if !success {
		statusBadge = pal.badgeFailed.Render("FAILED")
	}
	durationText := pal.meta.Render(fmt.Sprintf("⏱ %s", formatToolDuration(duration)))

	box := pal.boxSuccess
	if !success {
		box = pal.boxFailed
	}

	if !expanded {
		summaryParts := make([]string, 0, 2)
		if len(args) > 0 {
			summaryParts = append(summaryParts, fmt.Sprintf("%d arg(s)", len(args)))
		}
		trimmedOut := strings.TrimSpace(output)
		if trimmedOut != "" {
			lineCount := len(strings.Split(trimmedOut, "\n"))
			summaryParts = append(summaryParts, fmt.Sprintf("%d line(s)", lineCount))
		}
		metaSummary := ""
		if len(summaryParts) > 0 {
			metaSummary = "  " + pal.meta.Render("("+strings.Join(summaryParts, ", ")+")")
		}
		compactLine := fmt.Sprintf("▸ %s %s  %s %s%s", icon, pal.header.Render(toolName), statusBadge, durationText, metaSummary)
		return box.Render(compactLine)
	}

	var sb strings.Builder
	header := fmt.Sprintf("▾ %s %s   %s %s", icon, pal.header.Render(toolName), statusBadge, durationText)
	sb.WriteString(header + "\n")

	if len(args) > 0 {
		sb.WriteString(pal.meta.Render("Parameters:") + "\n")
		keys := make([]string, 0, len(args))
		for k := range args {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			vBytes, _ := json.Marshal(args[k])
			valPreview := truncateToolLine(string(vBytes), 88)
			sb.WriteString(fmt.Sprintf("  • %s: %s\n", pal.key.Render(k), pal.val.Render(valPreview)))
		}
	}

	if output != "" {
		sb.WriteString(pal.meta.Render("Output Preview:") + "\n")
		lines := strings.Split(strings.TrimSpace(output), "\n")
		maxLines := 6
		limit := len(lines)
		if limit > maxLines {
			limit = maxLines
		}
		for i := 0; i < limit; i++ {
			sb.WriteString(truncateToolLine(lines[i], 92) + "\n")
		}
		if len(lines) > maxLines {
			sb.WriteString(pal.meta.Render(fmt.Sprintf("... (%d lines truncated)", len(lines)-maxLines)))
		}
	}

	return box.Render(strings.TrimRight(sb.String(), "\n"))
}


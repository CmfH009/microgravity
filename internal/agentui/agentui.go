package agentui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

var (
	SubagentHeaderBox = lipgloss.NewStyle().
				Border(lipgloss.DoubleBorder()).
				BorderForeground(lipgloss.Color("99")). // Purple/violet
				Padding(0, 2).
				MarginTop(0).
				MarginBottom(1)

	SubagentMessageBox = lipgloss.NewStyle().
				Border(lipgloss.DoubleBorder()).
				BorderForeground(lipgloss.Color("39")). // Deep cyan
				Padding(0, 2).
				MarginTop(0).
				MarginBottom(1)

	SubagentTitleStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("213")).
				Bold(true)

	SubagentSenderStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("15")).
				Background(lipgloss.Color("39")).
				Bold(true).
				Padding(0, 1)

	MetaStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("246"))

	// Alert Box Styles
	AlertNoteStyle = lipgloss.NewStyle().
			Border(lipgloss.NormalBorder(), false, false, false, true).
			BorderForeground(lipgloss.Color("39")). // Blue/Cyan
			Padding(0, 1).
			Foreground(lipgloss.Color("252"))

	AlertTipStyle = lipgloss.NewStyle().
			Border(lipgloss.NormalBorder(), false, false, false, true).
			BorderForeground(lipgloss.Color("48")). // Green
			Padding(0, 1).
			Foreground(lipgloss.Color("252"))

	AlertWarningStyle = lipgloss.NewStyle().
			Border(lipgloss.NormalBorder(), false, false, false, true).
			BorderForeground(lipgloss.Color("208")). // Orange
			Padding(0, 1).
			Foreground(lipgloss.Color("252"))

	AlertImportantStyle = lipgloss.NewStyle().
			Border(lipgloss.NormalBorder(), false, false, false, true).
			BorderForeground(lipgloss.Color("141")). // Purple
			Padding(0, 1).
			Foreground(lipgloss.Color("252"))

	AlertCautionStyle = lipgloss.NewStyle().
			Border(lipgloss.NormalBorder(), false, false, false, true).
			BorderForeground(lipgloss.Color("196")). // Red
			Padding(0, 1).
			Foreground(lipgloss.Color("252"))

	DiffAddStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("48"))
	DiffDeleteStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
)

// RenderSubagentHeader renders a styled card when a subagent is launched.
func RenderSubagentHeader(id, role, prompt string) string {
	var sb strings.Builder
	sb.WriteString(SubagentTitleStyle.Render("🤖 SUBAGENT DISPATCHED") + "\n")
	sb.WriteString(MetaStyle.Render(fmt.Sprintf("ID: %s | Role: %s", id, role)) + "\n")
	if prompt != "" {
		sb.WriteString(fmt.Sprintf("Task: %s", prompt))
	}
	return SubagentHeaderBox.Render(strings.TrimRight(sb.String(), "\n"))
}

// RenderSubagentMessage renders inter-agent communication messages.
func RenderSubagentMessage(sender, message string) string {
	var sb strings.Builder
	header := fmt.Sprintf("%s %s", SubagentSenderStyle.Render("MESSAGE FROM"), SubagentTitleStyle.Render(sender))
	sb.WriteString(header + "\n\n")
	sb.WriteString(message)
	return SubagentMessageBox.Render(strings.TrimRight(sb.String(), "\n"))
}

// RenderAlert renders GitHub-style markdown alerts ([!NOTE], [!TIP], [!WARNING], [!IMPORTANT], [!CAUTION]).
func RenderAlert(alertType, text string) string {
	title := strings.ToUpper(alertType)
	content := fmt.Sprintf("[%s]\n%s", title, text)

	switch strings.ToUpper(alertType) {
	case "NOTE":
		return AlertNoteStyle.Render(content)
	case "TIP":
		return AlertTipStyle.Render(content)
	case "WARNING":
		return AlertWarningStyle.Render(content)
	case "IMPORTANT":
		return AlertImportantStyle.Render(content)
	case "CAUTION":
		return AlertCautionStyle.Render(content)
	default:
		return AlertNoteStyle.Render(content)
	}
}

// RenderDiff highlights code diff lines (+ additions green, - deletions red).
func RenderDiff(diffText string) string {
	lines := strings.Split(diffText, "\n")
	var out []string
	for _, l := range lines {
		if strings.HasPrefix(l, "+") && !strings.HasPrefix(l, "+++") {
			out = append(out, DiffAddStyle.Render(l))
		} else if strings.HasPrefix(l, "-") && !strings.HasPrefix(l, "---") {
			out = append(out, DiffDeleteStyle.Render(l))
		} else {
			out = append(out, l)
		}
	}
	return strings.Join(out, "\n")
}



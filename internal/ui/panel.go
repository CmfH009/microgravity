package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// Panel is a reusable bordered card with a title, used for consistent
// dashboard layout across Microgravity's terminal views.
type Panel struct {
	Title      string
	TitleRole  ThemeRole
	BorderRole ThemeRole
	Width      int
	PadTop     int
	PadRight   int
	PadBottom  int
	PadLeft    int
	Content    string
}

// NewPanel creates a Panel with default Microgravity styling.
func NewPanel(title string) *Panel {
	return &Panel{
		Title:      title,
		TitleRole:  RoleBrand,
		BorderRole: RoleSeparator,
		Width:      0, // 0 = auto-width
		PadTop:     1,
		PadRight:   2,
		PadBottom:  1,
		PadLeft:    2,
	}
}

// Render renders the panel as a styled string.
func (p *Panel) Render() string {
	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(RoleColor(p.TitleRole))

	borderColor := RoleColor(p.BorderRole)

	boxStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(borderColor).
		Padding(p.PadTop, p.PadRight, p.PadBottom, p.PadLeft)

	if p.Width > 0 {
		boxStyle = boxStyle.Width(p.Width)
	}

	header := titleStyle.Render(p.Title)
	if p.Content == "" {
		return boxStyle.Render(header)
	}

	content := header + "\n" + p.Content
	return boxStyle.Render(content)
}

// PanelGrid arranges multiple panels horizontally or vertically.
type PanelGrid struct {
	Panels    []*Panel
	Direction lipgloss.Position
	Gap       string
}

// NewHorizontalGrid creates a side-by-side panel grid.
func NewHorizontalGrid(panels ...*Panel) *PanelGrid {
	return &PanelGrid{
		Panels:    panels,
		Direction: lipgloss.Top,
		Gap:       "  ",
	}
}

// NewVerticalGrid creates a stacked panel grid.
func NewVerticalGrid(panels ...*Panel) *PanelGrid {
	return &PanelGrid{
		Panels:    panels,
		Direction: lipgloss.Left,
		Gap:       "\n",
	}
}

// Render renders all panels joined in the grid direction.
func (g *PanelGrid) Render() string {
	var rendered []string
	for _, p := range g.Panels {
		rendered = append(rendered, p.Render())
	}
	return lipgloss.JoinVertical(g.Direction, rendered...)
}

// SectionHeader renders a styled section header with a decorative rule.
func SectionHeader(text string, role ThemeRole) string {
	style := lipgloss.NewStyle().
		Bold(true).
		Foreground(RoleColor(role)).
		PaddingBottom(1)

	rule := lipgloss.NewStyle().
		Foreground(RoleColor(RoleSeparator)).
		Render(strings.Repeat("─", max(40, len(text)+4)))

	return style.Render(text) + "\n" + rule
}

// max returns the larger of two integers.
func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// FormatValue renders a label/value pair with theme-aware styling.
func FormatValue(label, value string, role ThemeRole) string {
	labelStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(RoleColor(role)).
		Width(18).
		Align(lipgloss.Right)

	valStyle := lipgloss.NewStyle().
		Foreground(RoleColor(RoleText))

	return fmt.Sprintf("%s  %s", labelStyle.Render(label), valStyle.Render(value))
}

// FormatStat renders a stat line with label, value, and optional unit.
func FormatStat(label string, value int, unit string, role ThemeRole) string {
	labelStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(RoleColor(role)).
		Width(16).
		Align(lipgloss.Right)

	valStyle := lipgloss.NewStyle().
		Foreground(RoleColor(RoleText)).
		Bold(true)

	unitStyle := lipgloss.NewStyle().
		Foreground(RoleColor(RoleTextMuted))

	return fmt.Sprintf("%s  %s%s",
		labelStyle.Render(label),
		valStyle.Render(fmt.Sprintf("%d", value)),
		unitStyle.Render(unit),
	)
}

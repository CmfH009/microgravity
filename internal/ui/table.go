package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// Table renders aligned, styled tabular data for terminal output.
type Table struct {
	Headers []string
	Rows    [][]string
	Widths  []int // computed column widths
	Align   []lipgloss.Position // per-column alignment
	Style   lipgloss.Style
	HeaderStyle lipgloss.Style
	RowStyle    lipgloss.Style
}

// NewTable creates a table from headers and rows.
func NewTable(headers []string, rows [][]string) *Table {
	t := &Table{
		Headers:   headers,
		Rows:      rows,
		Style:     lipgloss.NewStyle().Padding(0, 1),
		HeaderStyle: lipgloss.NewStyle().
			Bold(true).
			Foreground(RoleColor(RolePrimary)).
			Padding(0, 1),
		RowStyle: lipgloss.NewStyle().
			Foreground(RoleColor(RoleText)).
			Padding(0, 1),
		Align: make([]lipgloss.Position, len(headers)),
	}
	// Default: left-align all columns
	for i := range headers {
		t.Align[i] = lipgloss.Left
	}
	t.computeWidths()
	return t
}

// SetColumnAlign sets the alignment for a specific column.
func (t *Table) SetColumnAlign(col int, align lipgloss.Position) {
	if col >= 0 && col < len(t.Align) {
		t.Align[col] = align
	}
}

// computeWidths calculates optimal column widths.
func (t *Table) computeWidths() {
	nCols := len(t.Headers)
	t.Widths = make([]int, nCols)
	for i, h := range t.Headers {
		t.Widths[i] = len(h)
	}
	for _, row := range t.Rows {
		for i, cell := range row {
			if i < nCols {
				if w := len(cell); w > t.Widths[i] {
					t.Widths[i] = w
				}
			}
		}
	}
}

// Render renders the table as a string.
func (t *Table) Render() string {
	if len(t.Headers) == 0 && len(t.Rows) == 0 {
		return ""
	}

	var sb strings.Builder

	// Header row
	if len(t.Headers) > 0 {
		headerCells := make([]string, len(t.Headers))
		for i, h := range t.Headers {
			headerCells[i] = t.styleCell(h, t.Widths[i], t.Align[i], t.HeaderStyle)
		}
		sb.WriteString(t.Style.Render(strings.Join(headerCells, "")))
		sb.WriteString("\n")

		// Separator row
		sepCells := make([]string, len(t.Headers))
		for i, w := range t.Widths {
			sepCells[i] = strings.Repeat("─", w)
		}
		sepStyle := lipgloss.NewStyle().Foreground(RoleColor(RoleSeparator)).Padding(0, 1)
		sb.WriteString(sepStyle.Render(strings.Join(sepCells, "")))
		sb.WriteString("\n")
	}

	// Data rows
	for _, row := range t.Rows {
		cells := make([]string, len(t.Headers))
		for i := range t.Headers {
			var cell string
			if i < len(row) {
				cell = row[i]
			}
			cells[i] = t.styleCell(cell, t.Widths[i], t.Align[i], t.RowStyle)
		}
		sb.WriteString(t.Style.Render(strings.Join(cells, "")))
		sb.WriteString("\n")
	}

	return strings.TrimRight(sb.String(), "\n")
}

func (t *Table) styleCell(text string, width int, align lipgloss.Position, baseStyle lipgloss.Style) string {
	switch align {
	case lipgloss.Center:
		return baseStyle.Render(lipgloss.Place(width, 1, lipgloss.Center, lipgloss.Center, text))
	case lipgloss.Right:
		return baseStyle.Render(lipgloss.Place(width, 1, lipgloss.Right, lipgloss.Center, text))
	default:
		return baseStyle.Render(lipgloss.Place(width, 1, lipgloss.Left, lipgloss.Center, text))
	}
}

// FormatKeyValue renders a two-column key/value table.
func FormatKeyValue(pairs [][2]string) string {
	if len(pairs) == 0 {
		return ""
	}

	maxKey := 0
	for _, p := range pairs {
		if len(p[0]) > maxKey {
			maxKey = len(p[0])
		}
	}

	var sb strings.Builder
	for _, p := range pairs {
		keyStyle := lipgloss.NewStyle().
			Bold(true).
			Foreground(RoleColor(RolePrimary)).
			Width(maxKey + 1).
			Align(lipgloss.Right)
		valStyle := lipgloss.NewStyle().
			Foreground(RoleColor(RoleText))
		sb.WriteString(fmt.Sprintf("%s %s\n",
			keyStyle.Render(p[0]),
			valStyle.Render(p[1])))
	}
	return strings.TrimRight(sb.String(), "\n")
}

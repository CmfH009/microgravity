package skills

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type tuiModel struct {
	skills    []*Skill
	filtered  []*Skill
	cursor    int
	filter    string
	searching bool
	selected  *Skill
}

func (m tuiModel) Init() tea.Cmd {
	return nil
}

func (m tuiModel) filterSkills() []*Skill {
	if m.filter == "" {
		return m.skills
	}
	query := strings.ToLower(m.filter)
	var matches []*Skill
	for _, s := range m.skills {
		if strings.Contains(strings.ToLower(s.Name), query) ||
			strings.Contains(strings.ToLower(s.Description), query) ||
			strings.Contains(strings.ToLower(s.Category), query) ||
			strings.Contains(strings.ToLower(s.Type), query) {
			matches = append(matches, s)
		}
	}
	return matches
}

func (m tuiModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "esc":
			return m, tea.Quit
		case "up", "ctrl+k":
			if m.cursor > 0 {
				m.cursor--
			} else if len(m.filtered) > 0 {
				m.cursor = len(m.filtered) - 1
			}
		case "down", "ctrl+j":
			if m.cursor < len(m.filtered)-1 {
				m.cursor++
			} else {
				m.cursor = 0
			}
		case "backspace":
			if len(m.filter) > 0 {
				m.filter = m.filter[:len(m.filter)-1]
				m.filtered = m.filterSkills()
				m.cursor = 0
			}
		case "enter":
			if len(m.filtered) > 0 && m.cursor < len(m.filtered) {
				m.selected = m.filtered[m.cursor]
			}
			return m, tea.Quit
		default:
			if len(msg.String()) == 1 && msg.String() >= " " && msg.String() <= "~" {
				m.filter += msg.String()
				m.filtered = m.filterSkills()
				m.cursor = 0
			}
		}
	}
	return m, nil
}

func (m tuiModel) View() string {
	if len(m.skills) == 0 {
		return "No skills available.\nPress 'esc' to exit.\n"
	}

	titleStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("213")).Bold(true)
	header := titleStyle.Render("⚡ AGY INTERACTIVE SKILL SELECTOR")

	filterPrompt := lipgloss.NewStyle().Foreground(lipgloss.Color("39")).Bold(true).Render("🔍 Search: ")
	filterInput := m.filter
	if filterInput == "" {
		filterInput = lipgloss.NewStyle().Foreground(lipgloss.Color("240")).Italic(true).Render("type to filter skills...")
	}
	filterBar := filterPrompt + filterInput

	var leftPane strings.Builder
	leftPane.WriteString(filterBar + "\n\n")

	selectedStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("213")).Bold(true)
	toolBadgeStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("15")).Background(lipgloss.Color("99")).Bold(true).Padding(0, 1)
	promptBadgeStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("15")).Background(lipgloss.Color("39")).Bold(true).Padding(0, 1)

	displayList := m.filtered
	if len(displayList) == 0 {
		leftPane.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Render("  No skills match query: ") + m.filter + "\n")
	} else {
		maxItems := 12
		start := 0
		if m.cursor >= maxItems {
			start = m.cursor - maxItems + 1
		}
		end := start + maxItems
		if end > len(displayList) {
			end = len(displayList)
		}

		for i := start; i < end; i++ {
			item := displayList[i]
			cursor := "  "
			if m.cursor == i {
				cursor = "❯ "
			}

			badge := promptBadgeStyle.Render("PROMPT")
			if item.Type == "tool" {
				badge = toolBadgeStyle.Render("TOOL")
			}

			line := fmt.Sprintf("%s%-22s %s", cursor, item.Name, badge)
			if m.cursor == i {
				leftPane.WriteString(selectedStyle.Render(line) + "\n")
			} else {
				leftPane.WriteString(line + "\n")
			}
		}
		leftPane.WriteString(fmt.Sprintf("\nShowing %d of %d skills", len(displayList), len(m.skills)))
	}

	// Right Details Pane
	var rightPane strings.Builder
	if len(displayList) > 0 && m.cursor < len(displayList) {
		cur := displayList[m.cursor]
		rightPane.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("214")).Bold(true).Render("📌 Skill: "+cur.Name) + "\n\n")
		rightPane.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("252")).Bold(true).Render("Category: ") + cur.Category + "\n")
		rightPane.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("252")).Bold(true).Render("Type:     ") + cur.Type + "\n")
		if cur.Command != "" {
			rightPane.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("252")).Bold(true).Render("Command: ") + cur.Command + "\n")
		}
		rightPane.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("252")).Bold(true).Render("\nDescription:\n") + cur.Description + "\n")

		if cur.Body != "" {
			bodySnippet := cur.Body
			if len(bodySnippet) > 250 {
				bodySnippet = bodySnippet[:247] + "..."
			}
			rightPane.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("246")).Italic(true).Render("\nInstructions:\n"+bodySnippet) + "\n")
		}
	}

	leftBox := lipgloss.NewStyle().Width(42).Render(leftPane.String())
	rightBox := lipgloss.NewStyle().
		Width(45).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("63")).
		Padding(0, 1).
		Render(rightPane.String())

	content := lipgloss.JoinHorizontal(lipgloss.Top, leftBox, "  ", rightBox)
	footer := lipgloss.NewStyle().Foreground(lipgloss.Color("147")).Render("[↑/↓] Navigate  │  [Type] Filter  │  [Backspace] Delete  │  [Enter] Select  │  [Esc] Quit")

	boxStyle := lipgloss.NewStyle().
		Border(lipgloss.DoubleBorder()).
		BorderForeground(lipgloss.Color("99")).
		Padding(1, 2)

	return boxStyle.Render(header + "\n\n" + content + "\n\n" + footer)
}

// RunTUI launches the interactive skill picker interface
func RunTUI(loaded []*Skill) (*Skill, bool) {
	if len(loaded) == 0 {
		return nil, false
	}

	p := tea.NewProgram(tuiModel{
		skills:   loaded,
		filtered: loaded,
		cursor:   0,
	})

	m, err := p.Run()
	if err != nil {
		return nil, false
	}

	if finalModel, ok := m.(tuiModel); ok && finalModel.selected != nil {
		return finalModel.selected, true
	}
	return nil, false
}

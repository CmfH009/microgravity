package models

import (
	"context"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/CmfH009/microgravity/internal/config"
)

type model struct {
	catalog  []ModelInfo
	cursor   int
	selected ModelInfo
	activeID string
}

func (m model) Init() tea.Cmd {
	return nil
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q", "esc":
			return m, tea.Quit
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			} else {
				m.cursor = len(m.catalog) - 1
			}
		case "down", "j":
			if m.cursor < len(m.catalog)-1 {
				m.cursor++
			} else {
				m.cursor = 0
			}
		case "enter":
			if len(m.catalog) > 0 {
				m.selected = m.catalog[m.cursor]
				config.SetActiveModel(m.selected.ID)
			}
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m model) View() string {
	if len(m.catalog) == 0 {
		return "No models available in catalog.\nPress 'q' to quit.\n"
	}

	var sb strings.Builder
	sb.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("213")).Bold(true).Render("🤖  AGY MODEL SELECTOR & HARDWARE AUDIT") + "\n\n")

	selectedStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("213")).Bold(true)
	activeBadgeStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("15")).Background(lipgloss.Color("48")).Bold(true).Padding(0, 1)

	var currentCategory string
	for i, item := range m.catalog {
		if item.Category != currentCategory {
			currentCategory = item.Category
			sb.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("39")).Bold(true).Render("━━━━ "+currentCategory+" ━━━━") + "\n")
		}

		cursor := "  "
		if m.cursor == i {
			cursor = "❯ "
		}

		activeTag := ""
		if item.ID == m.activeID {
			activeTag = "  " + activeBadgeStyle.Render("ACTIVE MODEL")
		}

		line := fmt.Sprintf("%s%-24s %s  (%s)%s\n    %s\n",
			cursor, item.ID, item.RenderBadge(), item.Status, activeTag, item.Description)

		if m.cursor == i {
			sb.WriteString(selectedStyle.Render(line) + "\n")
		} else {
			sb.WriteString(line + "\n")
		}
	}

	sb.WriteString("\n" + lipgloss.NewStyle().Foreground(lipgloss.Color("147")).Render("[↑/↓/k/j] Navigate  │  [Enter] Select Model  │  [q/Esc] Quit") + "\n")

	boxStyle := lipgloss.NewStyle().
		Border(lipgloss.DoubleBorder()).
		BorderForeground(lipgloss.Color("99")).
		Padding(1, 3)


	return boxStyle.Render(sb.String())
}

func RunTUI(ctx context.Context) (ModelInfo, bool) {
	scanner := NewScanner()
	catalog, err := scanner.Scan(ctx)
	if err != nil || len(catalog) == 0 {
		return ModelInfo{}, false
	}

	activeID := config.GetActiveModel()

	// Initial cursor position at active model
	initialCursor := 0
	for i, m := range catalog {
		if m.ID == activeID {
			initialCursor = i
			catalog[i].IsActive = true
			break
		}
	}

	p := tea.NewProgram(model{
		catalog:  catalog,
		cursor:   initialCursor,
		activeID: activeID,
	})

	m, err := p.Run()
	if err != nil {
		return ModelInfo{}, false
	}

	if finalModel, ok := m.(model); ok && finalModel.selected.ID != "" {
		return finalModel.selected, true
	}
	return ModelInfo{}, false
}

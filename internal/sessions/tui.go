package sessions

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type model struct {
	sessions []SessionInfo
	cursor   int
	selected string
	deleted  bool
}

func (m model) Init() tea.Cmd {
	return nil
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q":
			return m, tea.Quit
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down", "j":
			if m.cursor < len(m.sessions)-1 {
				m.cursor++
			}
		case "enter":
			if len(m.sessions) > 0 {
				m.selected = m.sessions[m.cursor].Name
			}
			return m, tea.Quit
		case "x", "d", "delete":
			if len(m.sessions) > 0 {
				name := m.sessions[m.cursor].Name
				manager := NewManager()
				manager.DeleteSession(name)
				m.sessions, _ = manager.ListSessions()
				if m.cursor >= len(m.sessions) && m.cursor > 0 {
					m.cursor--
				}
				m.deleted = true
			}
		}
	}
	return m, nil
}

func (m model) View() string {
	if len(m.sessions) == 0 {
		return "No saved chat sessions found.\nPress 'q' to quit.\n"
	}

	s := "🗂️  SAVED CHAT SESSIONS\n\n"

	selectedStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("205")).Bold(true)
	snippetStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("242")).Italic(true)

	for i, session := range m.sessions {
		cursor := "  "
		if m.cursor == i {
			cursor = "➔ "
		}

		item := fmt.Sprintf("%s%s  (%d bytes, %s)\n", cursor, session.Name, session.Size, session.ModTime.Format("2006-01-02 15:04:05"))
		if session.Snippet != "" {
			item += fmt.Sprintf("    %s\n", snippetStyle.Render("\""+session.Snippet+"\""))
		}

		if m.cursor == i {
			s += selectedStyle.Render(item) + "\n"
		} else {
			s += item + "\n"
		}
	}

	s += "\n[↑/↓] Navigate  [Enter] Resume  [d/x] Delete  [q] Quit\n"
	return s
}

func RunTUI() string {
	manager := NewManager()
	list, err := manager.ListSessions()
	if err != nil {
		return ""
	}

	p := tea.NewProgram(model{sessions: list, cursor: 0}, tea.WithAltScreen())
	m, err := p.Run()
	if err != nil {
		return ""
	}

	if finalModel, ok := m.(model); ok {
		return finalModel.selected
	}
	return ""
}

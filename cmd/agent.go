package cmd

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/purav/microgravity/internal/agents"
	"github.com/spf13/cobra"
)

var agentCmd = &cobra.Command{
	Use:     "agent [flags]",
	Aliases: []string{"agents"},
	Short:   "List available agents",
	Long:    "List built-in and specialized autonomous agent roles and subagents.",
	Run: func(cmd *cobra.Command, args []string) {
		list := agents.BuiltinAgents()

		boxStyle := lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("39")).
			Padding(1, 2)

		var sb strings.Builder
		sb.WriteString("🤖 AVAILABLE AGENT ROLES & SUBAGENTS\n\n")

		for i, a := range list {
			modeTag := "Read/Write"
			if a.ReadOnly {
				modeTag = "Read-Only"
			}
			badge := lipgloss.NewStyle().Foreground(lipgloss.Color("15")).Background(lipgloss.Color("63")).Padding(0, 1).Bold(true).Render(a.Name)
			sb.WriteString(fmt.Sprintf("%d. %s  [%s]  Model: %s\n   Role: %s\n   Description: %s\n\n",
				i+1, badge, modeTag, a.Model, a.Role, a.Description))
		}

		sb.WriteString("Usage: Pass --agent <name> when starting a session (e.g., mg --agent code-reviewer)")
		fmt.Println(boxStyle.Render(sb.String()))
	},
}

func init() {
	rootCmd.AddCommand(agentCmd)
}

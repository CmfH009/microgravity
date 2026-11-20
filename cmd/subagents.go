package cmd

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/CmfH009/microgravity/internal/subagent"
	"github.com/CmfH009/microgravity/internal/ui"
	"github.com/spf13/cobra"
)

var (
	spawnRole      string
	spawnProfile   string
	spawnModel     string
	spawnWorkspace string
)

var subagentsCmd = &cobra.Command{
	Use:   "subagents",
	Short: "List, inspect, spawn, and manage active subagents",
	Run: func(cmd *cobra.Command, args []string) {
		cmd.Help()
	},
}

var subagentsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all active and recent subagents",
	Run: func(cmd *cobra.Command, args []string) {
		m := subagent.GetManager()
		list := m.List()

		if len(list) == 0 {
			fmt.Println(ui.PrintSystemInfo("No subagents currently active."))
			return
		}

		boxStyle := lipgloss.NewStyle().
			Border(lipgloss.DoubleBorder()).
			BorderForeground(lipgloss.Color("99")).
			Padding(1, 3)

		var sb strings.Builder
		sb.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("213")).Bold(true).Render("🤖  ACTIVE & RECENT SUBAGENTS") + "\n\n")

		for _, sa := range list {
			elapsed := time.Since(sa.StartTime).Truncate(time.Second)
			statusBadge := lipgloss.NewStyle().Foreground(lipgloss.Color("15")).Background(lipgloss.Color("48")).Bold(true).Padding(0, 1).Render(sa.Status)
			if sa.Status != "RUNNING" {
				statusBadge = lipgloss.NewStyle().Foreground(lipgloss.Color("15")).Background(lipgloss.Color("240")).Bold(true).Padding(0, 1).Render(sa.Status)
			}
			wsMode := sa.Workspace
			if wsMode == "" {
				wsMode = subagent.WorkspaceInherit
			}
			sb.WriteString(fmt.Sprintf("  • %s  ID: %s | Role: %s | Model: %s | Workspace: %s (⏱ %v)\n    Task: %s\n",
				statusBadge, sa.ID, sa.Role, sa.Model, wsMode, elapsed, sa.Prompt))
			if sa.Result != "" {
				sb.WriteString(fmt.Sprintf("    Result: %s\n", sa.Result))
			}
			sb.WriteString("\n")
		}

		fmt.Println(boxStyle.Render(strings.TrimRight(sb.String(), "\n")))
	},
}

var subagentsSpawnCmd = &cobra.Command{
	Use:   "spawn [prompt]",
	Short: "Spawn a parallel subagent with role profile, workspace mode, and model overrides",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		prompt := args[0]
		m := subagent.GetManager()

		id := fmt.Sprintf("sub-%d", time.Now().UnixNano()%1000000)
		prof := subagent.SubagentProfile(spawnProfile)
		if spawnProfile == "" {
			prof = subagent.ProfileGeneral
		}

		wsMode := subagent.WorkspaceMode(spawnWorkspace)
		if wsMode == "" {
			wsMode = subagent.WorkspaceInherit
		}

		info, err := m.Spawn(context.Background(), id, spawnRole, prof, prompt, spawnModel, wsMode, nil)
		if err != nil {
			fmt.Printf("%s\n", ui.PrintError(err))
			return
		}

		ui.PrintStepSuccess(fmt.Sprintf("Spawned Subagent [%s] (Role: %s, Model: %s, Profile: %s, Workspace: %s)", info.ID, info.Role, info.Model, info.Profile, info.Workspace))
	},
}

var subagentsKillCmd = &cobra.Command{
	Use:   "kill [subagent-id]",
	Short: "Terminate a running subagent",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		id := args[0]
		m := subagent.GetManager()

		if m.Kill(id) {
			ui.PrintStepSuccess(fmt.Sprintf("Subagent %s terminated.", id))
		} else {
			fmt.Printf("%s\n", ui.PrintError(fmt.Errorf("Subagent %s not found", id)))
		}
	},
}

var subagentsKillAllCmd = &cobra.Command{
	Use:   "kill-all",
	Short: "Terminate all running subagents",
	Run: func(cmd *cobra.Command, args []string) {
		m := subagent.GetManager()
		count := m.KillAll()
		ui.PrintStepSuccess(fmt.Sprintf("Terminated %d running subagent(s).", count))
	},
}

func init() {
	subagentsSpawnCmd.Flags().StringVarP(&spawnRole, "role", "r", "Subagent Worker", "Role title of the subagent")
	subagentsSpawnCmd.Flags().StringVarP(&spawnProfile, "profile", "p", "general", "Subagent profile (researcher, qa-auditor, security-auditor, general)")
	subagentsSpawnCmd.Flags().StringVarP(&spawnModel, "model", "m", "", "Model override (e.g., gemini-3.5-flash-lite, gemma:7b)")
	subagentsSpawnCmd.Flags().StringVarP(&spawnWorkspace, "workspace", "w", "inherit", "Workspace mode (inherit, branch, share)")

	subagentsCmd.AddCommand(subagentsListCmd)
	subagentsCmd.AddCommand(subagentsSpawnCmd)
	subagentsCmd.AddCommand(subagentsKillCmd)
	subagentsCmd.AddCommand(subagentsKillAllCmd)
	rootCmd.AddCommand(subagentsCmd)
}


package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/CmfH009/microgravity/internal/git"
	"github.com/CmfH009/microgravity/internal/ui"
	"github.com/spf13/cobra"
)

var worktreeCmd = &cobra.Command{
	Use:   "worktree",
	Short: "Manage isolated git worktrees for autonomous feature development",
}

var worktreeListCmd = &cobra.Command{
	Use:   "list",
	Short: "List active git worktrees",
	Run: func(cmd *cobra.Command, args []string) {
		cwd, _ := os.Getwd()
		list, err := git.ListWorktrees(cwd)
		if err != nil {
			fmt.Println(ui.PrintError(err))
			return
		}

		cardStyle := lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("63")).
			Padding(1, 2)

		var sb strings.Builder
		sb.WriteString("🌿 ISOLATED GIT WORKTREES\n\n")
		for i, wt := range list {
			sb.WriteString(fmt.Sprintf("%d. %s\n   Path: %s\n   Branch: %s\n\n", i+1, wt.Name, wt.Path, wt.Branch))
		}

		fmt.Println(cardStyle.Render(sb.String()))
	},
}

var worktreeCreateCmd = &cobra.Command{
	Use:   "create [name]",
	Short: "Create an isolated git worktree in .worktrees/[name]",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		cwd, _ := os.Getwd()
		name := args[0]

		path, err := git.CreateWorktree(cwd, name)
		if err != nil {
			fmt.Println(ui.PrintError(err))
			return
		}

		ui.PrintStepSuccess(fmt.Sprintf("Created isolated worktree '%s' at: %s", name, path))
	},
}

var worktreeRemoveCmd = &cobra.Command{
	Use:   "remove [name]",
	Short: "Remove an isolated git worktree",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		cwd, _ := os.Getwd()
		name := args[0]

		err := git.RemoveWorktree(cwd, name)
		if err != nil {
			fmt.Println(ui.PrintError(err))
			return
		}

		ui.PrintStepSuccess(fmt.Sprintf("Removed worktree '%s'", name))
	},
}

func init() {
	worktreeCmd.AddCommand(worktreeListCmd)
	worktreeCmd.AddCommand(worktreeCreateCmd)
	worktreeCmd.AddCommand(worktreeRemoveCmd)
	rootCmd.AddCommand(worktreeCmd)
}

package cmd

import (
	"fmt"
	"os"

	"github.com/CmfH009/microgravity/internal/git"
	"github.com/CmfH009/microgravity/internal/plan"
	"github.com/CmfH009/microgravity/internal/ui"
	"github.com/spf13/cobra"
)

var workflowCmd = &cobra.Command{
	Use:   "workflow",
	Short: "Automated git worktree, master plan, and conventional commit workflow manager",
}

var workflowStartCmd = &cobra.Command{
	Use:   "start [feature-name]",
	Short: "Start a new feature workflow (creates isolated worktree & initializes PLAN.md)",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		cwd, _ := os.Getwd()
		feature := args[0]

		ui.PrintPhaseHeader("Starting Automated Workflow: "+feature, "🌿")

		wtPath, err := git.CreateWorktree(cwd, feature)
		if err != nil {
			fmt.Println(ui.PrintError(fmt.Errorf("failed to create worktree: %v", err)))
			return
		}
		ui.PrintStepSuccess("Created isolated worktree at: " + wtPath)

		p := plan.NewPlan("Feature Workflow: " + feature)
		if err := p.SaveToFile(wtPath + "/PLAN.md"); err != nil {
			ui.PrintStepFailed("Failed to initialize PLAN.md: " + err.Error())
		} else {
			ui.PrintStepSuccess("Initialized PLAN.md in worktree")
		}

		ui.PrintStepComplete(fmt.Sprintf("Workflow ready! Cd into '%s' to begin editing.", wtPath))
	},
}

func init() {
	workflowCmd.AddCommand(workflowStartCmd)
	rootCmd.AddCommand(workflowCmd)
}

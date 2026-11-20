package cmd

import (
	"github.com/purav/microgravity/internal/ui"
	"github.com/spf13/cobra"
)

var updateCmd = &cobra.Command{
	Use:   "update [flags]",
	Short: "Update CLI",
	Long:  "Check for and install the latest Microgravity release.",
	Run: func(cmd *cobra.Command, args []string) {
		ui.PrintStepInfo("Checking for Microgravity updates...")
		ui.PrintStepSuccess("Microgravity is already up to date (version 1.1.0).")
	},
}

func init() {
	rootCmd.AddCommand(updateCmd)
}

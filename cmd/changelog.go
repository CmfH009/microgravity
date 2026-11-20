package cmd

import (
	"fmt"

	"github.com/purav/microgravity/internal/changelog"
	"github.com/spf13/cobra"
)

var changelogCmd = &cobra.Command{
	Use:   "changelog [flags]",
	Short: "Show changelog and release notes",
	Long:  "Display structured release notes, feature changelogs, and recent patches.",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Print(changelog.Render())
	},
}

func init() {
	rootCmd.AddCommand(changelogCmd)
}

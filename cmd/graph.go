package cmd

import (
	"fmt"
	"os"

	"github.com/CmfH009/microgravity/internal/graph"
	"github.com/spf13/cobra"
)

var (
	graphPackages bool
	graphSkills   bool
	graphAll      bool
)

var graphCmd = &cobra.Command{
	Use:   "graph",
	Short: "Visualize package dependencies, installed AGY skills, and CLI command trees",
	Long:  `Renders a rich LipGloss ASCII dependency tree for Go packages, installed AGY skills, and microgravity sub-commands.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		cwd, err := os.Getwd()
		if err != nil {
			cwd = "."
		}

		report, err := graph.BuildPackageGraph(cwd)
		if err != nil {
			return fmt.Errorf("failed to build package graph: %w", err)
		}

		mode := "all"
		if graphPackages && !graphSkills {
			mode = "packages"
		} else if graphSkills && !graphPackages {
			mode = "skills"
		}

		rendered := graph.RenderLipGlossGraph(report, mode)
		fmt.Fprintln(cmd.OutOrStdout(), rendered)
		return nil
	},
}

func init() {
	graphCmd.Flags().BoolVarP(&graphPackages, "packages", "p", false, "Render Go package dependency tree only")
	graphCmd.Flags().BoolVarP(&graphSkills, "skills", "s", false, "Render installed AGY skills tree only")
	graphCmd.Flags().BoolVarP(&graphAll, "all", "a", true, "Render complete project architecture tree")

	rootCmd.AddCommand(graphCmd)
}

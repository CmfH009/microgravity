package cmd

import (
	"context"
	"fmt"

	"github.com/CmfH009/microgravity/internal/profiler"
	"github.com/spf13/cobra"
)

var profileCmd = &cobra.Command{
	Use:   "profile [package]",
	Short: "Automated benchmark & runtime memory profiler for Go packages",
	Long:  `Runs memory allocation and GC cycle profiling across target Go packages, rendering a LipGloss performance scorecard.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		targetPkg := "."
		if len(args) > 0 {
			targetPkg = args[0]
		}

		ctx := context.Background()
		report, err := profiler.ProfilePackage(ctx, targetPkg)
		if err != nil {
			return fmt.Errorf("failed to profile package: %w", err)
		}

		rendered := profiler.RenderProfileReport(report)
		fmt.Fprintln(cmd.OutOrStdout(), rendered)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(profileCmd)
}

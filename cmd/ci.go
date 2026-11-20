package cmd

import (
	"fmt"

	"github.com/purav/microgravity/internal/ci"
	"github.com/spf13/cobra"
)

var (
	ciOutputDir      string
	ciBinaryName     string
	ciGoVersion      string
	ciDryRun         bool
	ciForce          bool
	ciDisableLint    bool
	ciDisableTest    bool
	ciDisableMatrix  bool
	ciDisableRelease bool
)

var ciCmd = &cobra.Command{
	Use:   "ci",
	Short: "Generate GitHub Actions CI/CD workflows for linting, testing, matrix builds, and releases",
	Long: `Generates production-grade GitHub Actions workflow YAML files for:
- Code linting (golangci-lint & go vet)
- Unit testing with race detector and code coverage
- Cross-platform matrix binary compilation (Windows, macOS, Linux x amd64/arm64)
- Automated tag-triggered release deployment and asset publishing`,
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg := ci.Config{
			AppName:        ciBinaryName,
			GoVersion:      ciGoVersion,
			OutputDir:      ciOutputDir,
			Platforms:      ci.DefaultPlatforms(),
			EnableLint:     !ciDisableLint,
			EnableTest:     !ciDisableTest,
			EnableMatrix:   !ciDisableMatrix,
			EnableRelease:  !ciDisableRelease,
			ForceOverwrite: ciForce,
		}

		if err := ci.ValidateConfig(cfg); err != nil {
			return fmt.Errorf("invalid CI configuration: %w", err)
		}

		if ciDryRun {
			workflows := ci.GenerateAll(cfg)
			out := cmd.OutOrStdout()
			for filename, content := range workflows {
				fmt.Fprintf(out, "=== File: %s ===\n%s\n", filename, content)
			}
			return nil
		}

		written, err := ci.WriteWorkflows(cfg)
		if err != nil {
			return fmt.Errorf("failed to generate workflows: %w", err)
		}

		summary := ci.RenderSummary(cfg, written)
		fmt.Fprintln(cmd.OutOrStdout(), summary)
		return nil
	},
}

func init() {
	ciCmd.Flags().StringVarP(&ciOutputDir, "output", "o", ".github/workflows", "Output directory for workflow files")
	ciCmd.Flags().StringVarP(&ciBinaryName, "binary-name", "b", "microgravity", "Binary name for compilation and release assets")
	ciCmd.Flags().StringVarP(&ciGoVersion, "go-version", "g", "1.23", "Target Go version for setup-go step")
	ciCmd.Flags().BoolVar(&ciDryRun, "dry-run", false, "Preview generated workflows in stdout without writing files")
	ciCmd.Flags().BoolVarP(&ciForce, "force", "f", false, "Force overwrite existing workflow files")
	ciCmd.Flags().BoolVar(&ciDisableLint, "no-lint", false, "Disable code linting job generation")
	ciCmd.Flags().BoolVar(&ciDisableTest, "no-test", false, "Disable unit test job generation")
	ciCmd.Flags().BoolVar(&ciDisableMatrix, "no-matrix", false, "Disable cross-platform build matrix job generation")
	ciCmd.Flags().BoolVar(&ciDisableRelease, "no-release", false, "Disable automated release workflow generation")

	rootCmd.AddCommand(ciCmd)
}

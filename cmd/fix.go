package cmd

import (
	"context"
	"fmt"
	"os"

	"github.com/purav/microgravity/internal/git"
	"github.com/purav/microgravity/internal/ui"
	"github.com/purav/microgravity/internal/verifier"
	"github.com/spf13/cobra"
)

var (
	fixMaxAttempts int
	fixAutoCommit  bool
	fixFramework   string
)

var fixCmd = &cobra.Command{
	Use:   "fix",
	Short: "Automatically diagnose, repair, and verify failing workspace tests or compilation errors across Go, TS/JS, Python, and Rust",
	Run: func(cmd *cobra.Command, args []string) {
		cwd, _ := os.Getwd()
		ctx := context.Background()

		fmt.Println(ui.PrintHeader("🔧 AUTONOMOUS SELF-HEALING ENGINE"))

		v := verifier.New()

		// 1. Initial multi-framework verification pass
		res, err := v.Verify(ctx, cwd)
		if err == nil && res.Passed {
			ui.PrintStepSuccess(fmt.Sprintf("Workspace tests are 100%% passing cleanly! Runner: %s", res.Command))
			return
		}

		errOutput := res.Output
		fmt.Println(ui.PrintSystemInfo(fmt.Sprintf("Detected test/compilation failures (%s). Initiating doubt-driven repair loop...", res.Command)))
		fmt.Println(res.RenderBadge())

		if len(res.Diagnoses) > 0 {
			fmt.Println("\n🔍 Failure Diagnosis:")
			for _, diag := range res.Diagnoses {
				fmt.Println(diag.RenderSummary())
			}
		}

		// 2. Invoke Verifier AutoFix with Feedback Loop
		opts := verifier.FixOptions{
			MaxAttempts: fixMaxAttempts,
			AutoCommit:  fixAutoCommit,
			Framework:   verifier.FrameworkType(fixFramework),
		}

		fixRes, repairErr := v.AutoFix(ctx, cwd, opts)
		if fixRes != nil && fixRes.Passed {
			ui.PrintStepSuccess("Workspace auto-repaired successfully!")
			fmt.Println(fixRes.RenderBadge())
			if fixAutoCommit {
				if err := git.GitCommit(cwd, "fix: auto-repaired broken workspace tests"); err == nil {
					ui.PrintStepSuccess("Git commit created for auto-repaired tests.")
				}
			}
		} else {
			details := ""
			if fixRes != nil {
				details = fixRes.Output
			}
			fmt.Println(ui.PrintError(fmt.Errorf("Auto-repair loop complete. Remaining details:\n%s\n%s\n%v", errOutput, details, repairErr)))
		}
	},
}

func init() {
	fixCmd.Flags().IntVarP(&fixMaxAttempts, "max-attempts", "a", 5, "Maximum number of repair feedback loop attempts")
	fixCmd.Flags().BoolVarP(&fixAutoCommit, "commit", "c", true, "Automatically git commit when tests pass")
	fixCmd.Flags().StringVarP(&fixFramework, "framework", "f", "", "Specific framework to target (go, vitest, jest, npm, pytest, cargo)")
	rootCmd.AddCommand(fixCmd)
}

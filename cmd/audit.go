package cmd

import (
	"fmt"
	"os"

	"github.com/purav/microgravity/internal/audit"
	"github.com/purav/microgravity/internal/ui"
	"github.com/spf13/cobra"
)

var autoFix bool

var auditCmd = &cobra.Command{
	Use:   "audit [path]",
	Short: "Audit a workspace for code standards, tests, and security hygiene",
	Run: func(cmd *cobra.Command, args []string) {
		targetDir := "."
		if len(args) > 0 {
			targetDir = args[0]
		}

		cwd, err := os.Getwd()
		if err == nil && targetDir == "." {
			targetDir = cwd
		}

		res, err := audit.AuditWorkspace(targetDir)
		if err != nil {
			fmt.Printf("%s\n", ui.PrintError(err))
			return
		}

		fmt.Println(res.RenderScorecard())

		if autoFix {
			patched := res.ApplyPatches(targetDir)
			if patched > 0 {
				ui.PrintStepSuccess(fmt.Sprintf("Applied %d automatic patch(es) to %s!", patched, targetDir))
			} else {
				ui.PrintStepInfo("No auto-fixable patches required.")
			}
		}
	},
}

func init() {
	auditCmd.Flags().BoolVarP(&autoFix, "fix", "f", false, "Automatically apply repair patches for fixable issues")
	rootCmd.AddCommand(auditCmd)
}

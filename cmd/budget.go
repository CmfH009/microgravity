package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/purav/microgravity/internal/budget"
	"github.com/spf13/cobra"
)

var (
	budgetJSONFlag  bool
	budgetResetFlag bool
)

var budgetCmd = &cobra.Command{
	Use:   "budget",
	Short: "Show today's token and cost budget usage and enforcement limits",
	Long: `Budget guardrails accumulate per calendar day and are enforced before every
cloud model call. Once a hard limit is reached, model calls fail fast with an
actionable error instead of silently burning more budget.

Configuration (environment variables):
  MG_BUDGET_MAX_TOKENS    hard cap on total daily tokens (default 3,000,000)
  MG_BUDGET_MAX_COST_USD  hard cap on daily estimated spend (default 10.00)
  MG_BUDGET_WARN_PCT      utilization percent for one-time warning (default 80)
  MG_BUDGET_DISABLE       set to 1 to disable enforcement entirely`,
	Run: func(cmd *cobra.Command, args []string) {
		if budgetResetFlag {
			if err := budget.Reset(); err != nil {
				fmt.Fprintf(os.Stderr, "Failed to reset budget: %s\n", err)
				os.Exit(1)
			}
			fmt.Println("Daily budget counters reset for today.")
			return
		}

		s := budget.GetStatus()
		if budgetJSONFlag {
			data, err := json.MarshalIndent(s, "", "  ")
			if err != nil {
				fmt.Fprintf(os.Stderr, "Failed to encode budget status: %s\n", err)
				os.Exit(1)
			}
			fmt.Println(string(data))
			return
		}

		state := "OK"
		switch {
		case s.Limits.Disabled:
			state = "DISABLED"
		case s.Exceeded:
			state = "EXCEEDED"
		case s.UtilizationPct >= s.Limits.WarnPct || s.CostUtilization >= s.Limits.WarnPct:
			state = "WARNING"
		}

		fmt.Printf("Daily Budget \u2014 %s  [%s]\n", s.Date, state)
		fmt.Printf("  Tokens : %s  %d / %d (%.1f%%)\n",
			utilBar(s.UtilizationPct), s.TotalTokens, s.Limits.MaxTokens, s.UtilizationPct)
		fmt.Printf("  Cost   : %s  $%.4f / $%.2f (%.1f%%)\n",
			utilBar(s.CostUtilization), s.CostUSD, s.Limits.MaxCostUSD, s.CostUtilization)
		if len(s.Models) > 0 {
			fmt.Println("  Models :")
			for m, tok := range s.Models {
				fmt.Printf("    %-44s %d tokens\n", m, tok)
			}
		}
		fmt.Println()
		fmt.Println("Enforced before every cloud model call. Raise caps via MG_BUDGET_MAX_TOKENS /")
		fmt.Println("MG_BUDGET_MAX_COST_USD, reset counters with --reset, or disable with MG_BUDGET_DISABLE=1.")
	},
}

// utilBar renders a small text progress meter for a utilization percentage.
func utilBar(pct float64) string {
	const width = 20
	filled := int(pct / 100 * width)
	if filled > width {
		filled = width
	}
	if filled < 0 {
		filled = 0
	}
	return "[" + strings.Repeat("#", filled) + strings.Repeat(".", width-filled) + "]"
}

func init() {
	budgetCmd.Flags().BoolVar(&budgetJSONFlag, "json", false, "Output budget status as JSON")
	budgetCmd.Flags().BoolVar(&budgetResetFlag, "reset", false, "Reset today's budget counters")
	rootCmd.AddCommand(budgetCmd)
}

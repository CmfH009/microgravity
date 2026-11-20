package cmd

import (
	"fmt"
	"os"
	"strconv"

	"github.com/CmfH009/microgravity/internal/journal"
	"github.com/spf13/cobra"
)

var (
	undoCount   int
	undoList    bool
	undoClear   bool
)

var undoCmd = &cobra.Command{
	Use:   "undo [n]",
	Short: "Revert the last n file modifications made by mg write tools",
	Long: `Revert the last n file modifications made by Microgravity's write tools
(write_to_file, replace_file_content).

Each modification is journaled before it is applied, capturing the target path
and the original content. mg undo restores the original content for the last n
entries, providing a safety net for autonomous agent file edits.

Usage:
  mg undo            Revert the last file modification
  mg undo 3          Revert the last 3 file modifications
  mg undo --list     List all journaled modifications
  mg undo --clear    Clear the journal

Journal is stored at ~/.microgravity/journal.json.
Disable journaling with MG_JOURNAL_DISABLE=1.`,
	Run: func(cmd *cobra.Command, args []string) {
		j := journal.Default()
		if !j.Enabled() {
			fmt.Println("Journaling is disabled (MG_JOURNAL_DISABLE=1).")
			return
		}

		if undoList {
			entries := j.List()
			if len(entries) == 0 {
				fmt.Println("Journal is empty.")
				return
			}
			fmt.Println("Journaled file modifications:")
			for i, e := range entries {
				fmt.Printf("  %d. [%s] %s %s\n", i+1, e.Timestamp, e.Op, e.Target)
			}
			return
		}

		if undoClear {
			if err := j.Clear(); err != nil {
				fmt.Fprintf(os.Stderr, "Failed to clear journal: %v\n", err)
				os.Exit(1)
			}
			fmt.Println("Journal cleared.")
			return
		}

		n := undoCount
		if n <= 0 {
			n = 1
		}
		if len(args) > 0 {
			parsed, err := strconv.Atoi(args[0])
			if err != nil || parsed < 1 {
				fmt.Fprintf(os.Stderr, "Invalid undo count: %q (use a positive integer)\n", args[0])
				os.Exit(1)
			}
			n = parsed
		}

		undone, err := j.Undo(n)
		if err != nil {
			if err == journal.ErrJournalDisabled {
				fmt.Println("Journaling is disabled.")
				return
			}
			fmt.Fprintf(os.Stderr, "Undo failed: %v\n", err)
			os.Exit(1)
		}

		if len(undone) == 0 {
			fmt.Println("Nothing to undo.")
			return
		}

		fmt.Printf("Reverted %d modification(s):\n", len(undone))
		for _, e := range undone {
			action := "restored"
			if e.Original == "" {
				action = "removed"
			}
			fmt.Printf("  %s %s (%s)\n", action, e.Target, e.Op)
		}
	},
}

func init() {
	undoCmd.Flags().IntVar(&undoCount, "count", 1, "Number of modifications to revert (alias for positional arg)")
	undoCmd.Flags().BoolVar(&undoList, "list", false, "List journaled modifications")
	undoCmd.Flags().BoolVar(&undoClear, "clear", false, "Clear the journal")
	rootCmd.AddCommand(undoCmd)
}

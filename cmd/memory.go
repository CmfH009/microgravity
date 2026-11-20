package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/CmfH009/microgravity/internal/memory"
	"github.com/CmfH009/microgravity/internal/ui"
	"github.com/spf13/cobra"
)

var memoryCmd = &cobra.Command{
	Use:   "memory [query]",
	Short: "Search cross-session semantic memory",
	Long: `Search semantically similar past conversation turns stored in the
cross-session memory store. Uses feature-hashing embeddings for offline
semantic similarity — no external embedding model required.

Usage:
  mg memory <query>              Search past conversations
  mg memory --clear              Clear the memory store
  mg memory --store              (internal) Store current session turn`,
	Run: func(cmd *cobra.Command, args []string) {
		home, _ := os.UserHomeDir()
		dbPath := filepath.Join(home, ".microgravity", "memory.db")

		if memoryClear {
			if err := os.Remove(dbPath); err != nil && !os.IsNotExist(err) {
				fmt.Fprintf(os.Stderr, "%s\n", ui.PrintError(fmt.Errorf("failed to clear memory store: %w", err)))
				os.Exit(1)
			}
			ui.PrintStepSuccess("Memory store cleared.")
			return
		}

		store, err := memory.NewMemoryStore(dbPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s\n", ui.PrintError(fmt.Errorf("failed to open memory store: %w", err)))
			os.Exit(1)
		}
		defer store.Close()

		if len(args) < 1 {
			fmt.Println(ui.PrintSystemInfo("Usage: mg memory <query>"))
			return
		}

		query := strings.Join(args, " ")
		results, err := store.RetrieveRelevant(query, 10)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s\n", ui.PrintError(fmt.Errorf("memory search failed: %w", err)))
			os.Exit(1)
		}

		if len(results) == 0 {
			fmt.Println(ui.PrintSystemInfo("No relevant past conversations found."))
			return
		}

		memory.SortResults(results)

		fmt.Println(ui.PrintHeader(" 🧠 Cross-Session Memory Search "))
		fmt.Printf("Query: %s\n\n", query)
		for i, r := range results {
			fmt.Printf("  %d.%s\n", i+1, memory.FormatResult(r))
		}
	},
}

var memoryClear bool

func init() {
	memoryCmd.Flags().BoolVar(&memoryClear, "clear", false, "Clear the memory store")
	rootCmd.AddCommand(memoryCmd)
}

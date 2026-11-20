package cmd

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/charmbracelet/lipgloss"
	"github.com/CmfH009/microgravity/internal/indexer"
	"github.com/CmfH009/microgravity/internal/rag"
	"github.com/spf13/cobra"
)

var queryCmd = &cobra.Command{
	Use:   "query <search-prompt>",
	Short: "Perform semantic search across indexed codebase",
	Args:  cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		queryPrompt := args[0]
		absDir, err := filepath.Abs(".")
		if err != nil {
			return err
		}

		dbPath := filepath.Join(absDir, ".mg", "vector_store.db")
		store, err := rag.NewStore(dbPath)
		if err != nil {
			return fmt.Errorf("vector index not found. Run 'mg index' first. (%v)", err)
		}
		defer store.Close()

		embedder := indexer.NewHybridEmbedder("http://localhost:11434", "nomic-embed-text")
		idx := indexer.NewIndexer(store, embedder, absDir)

		results, err := idx.Query(context.Background(), queryPrompt, 5)
		if err != nil {
			return err
		}

		cardStyle := lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("63")).
			Padding(0, 1).
			MarginBottom(1)

		headerStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("212"))

		fmt.Printf("🔍 Search results for: %q\n\n", queryPrompt)
		for i, res := range results {
			header := fmt.Sprintf("#%d (Score: %.3f) %s", i+1, res.Score, res.ID)
			content := fmt.Sprintf("%s\n\n%s", headerStyle.Render(header), res.Text)
			fmt.Println(cardStyle.Render(content))
		}

		return nil
	},
}

func init() {
	rootCmd.AddCommand(queryCmd)
}

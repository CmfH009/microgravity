package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/purav/microgravity/internal/indexer"
	"github.com/purav/microgravity/internal/rag"
	"github.com/spf13/cobra"
)

var indexCmd = &cobra.Command{
	Use:   "index [path]",
	Short: "Index repository source code into vector store",
	RunE: func(cmd *cobra.Command, args []string) error {
		targetDir := "."
		if len(args) > 0 {
			targetDir = args[0]
		}

		absDir, err := filepath.Abs(targetDir)
		if err != nil {
			return err
		}

		dbPath := filepath.Join(absDir, ".mg", "vector_store.db")
		if err := os.MkdirAll(filepath.Dir(dbPath), 0755); err != nil {
			return err
		}

		store, err := rag.NewStore(dbPath)
		if err != nil {
			return fmt.Errorf("failed to open vector DB: %w", err)
		}
		defer store.Close()

		embedder := indexer.NewHybridEmbedder("http://localhost:11434", "nomic-embed-text")
		idx := indexer.NewIndexer(store, embedder, absDir)

		titleStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("86"))
		fmt.Println(titleStyle.Render("🔍 Indexing codebase at: ") + absDir)

		start := time.Now()
		count, err := idx.IndexDirectory(context.Background(), absDir)
		if err != nil {
			return err
		}

		elapsed := time.Since(start).Round(time.Millisecond)
		successStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("82"))
		fmt.Println(successStyle.Render(fmt.Sprintf("✨ Successfully indexed %d code chunks in %v", count, elapsed)))
		return nil
	},
}

func init() {
	rootCmd.AddCommand(indexCmd)
}

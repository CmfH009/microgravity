package indexer

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/CmfH009/microgravity/internal/rag"
)

func TestIndexer_IndexAndQuery(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "mg_index_test_*")
	if err != nil {
		t.Fatalf("failed to create tmp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create test file
	testFile := filepath.Join(tmpDir, "calculator.go")
	code := `package calc

func Add(a, b int) int {
	return a + b
}
`
	if err := os.WriteFile(testFile, []byte(code), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	dbPath := filepath.Join(tmpDir, "test_vector.db")
	store, err := rag.NewStore(dbPath)
	if err != nil {
		t.Fatalf("failed to create RAG store: %v", err)
	}
	defer store.Close()

	embedder := NewLexicalEmbedder(384)
	idx := NewIndexer(store, embedder, tmpDir)

	count, err := idx.IndexDirectory(context.Background(), tmpDir)
	if err != nil {
		t.Fatalf("indexing failed: %v", err)
	}

	if count == 0 {
		t.Fatalf("expected at least 1 indexed chunk, got 0")
	}

	// Query
	results, err := idx.Query(context.Background(), "Add calculator sum", 3)
	if err != nil {
		t.Fatalf("query failed: %v", err)
	}

	if len(results) == 0 {
		t.Fatalf("expected query results, got 0")
	}
}

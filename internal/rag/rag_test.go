package rag

import (
	"context"
	"path/filepath"
	"testing"
)

func TestRAGStore_Basic(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test_rag.db")
	store, err := NewStore(dbPath)
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}
	defer store.Close()

	ctx := context.Background()

	// Add some documents
	docs := []struct {
		id        string
		text      string
		embedding []float32
	}{
		{"doc1", "This is about Go programming", []float32{1.0, 0.0, 0.0}},
		{"doc2", "This is about Python programming", []float32{0.0, 1.0, 0.0}},
		{"doc3", "This is about Rust programming", []float32{0.0, 0.0, 1.0}},
	}

	for _, d := range docs {
		if err := store.AddDocument(ctx, d.id, d.text, d.embedding); err != nil {
			t.Fatalf("Failed to add document %s: %v", d.id, err)
		}
	}

	// Search
	query := []float32{0.9, 0.1, 0.0}
	results, err := store.Search(ctx, query, 2)
	if err != nil {
		t.Fatalf("Failed to search: %v", err)
	}

	if len(results) != 2 {
		t.Fatalf("Expected 2 results, got %d", len(results))
	}

	if results[0].ID != "doc1" {
		t.Errorf("Expected first result to be doc1, got %s", results[0].ID)
	}
	if results[1].ID != "doc2" {
		t.Errorf("Expected second result to be doc2, got %s", results[1].ID)
	}
}

func TestRAGStore_EdgeCases(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test_rag.db")
	store, err := NewStore(dbPath)
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	query := []float32{0.5, 0.5}

	t.Run("negative topK", func(t *testing.T) {
		res, err := store.Search(ctx, query, -1)
		if err != nil {
			t.Fatalf("expected no error for negative topK, got %v", err)
		}
		if len(res) != 0 {
			t.Errorf("expected 0 results, got %d", len(res))
		}
	})

	t.Run("zero topK", func(t *testing.T) {
		res, err := store.Search(ctx, query, 0)
		if err != nil {
			t.Fatalf("expected no error for zero topK, got %v", err)
		}
		if len(res) != 0 {
			t.Errorf("expected 0 results, got %d", len(res))
		}
	})

	t.Run("empty embedding in query", func(t *testing.T) {
		store.AddDocument(ctx, "doc1", "text", []float32{1.0, 1.0})
		res, err := store.Search(ctx, []float32{}, 2)
		if err != nil {
			t.Fatalf("expected no error for empty embedding, got %v", err)
		}
		if len(res) != 1 {
			t.Fatalf("expected 1 result, got %d", len(res))
		}
		if res[0].Score != 0 {
			t.Errorf("expected score to be 0 for empty query embedding, got %f", res[0].Score)
		}
	})

	t.Run("context cancellation", func(t *testing.T) {
		ctxCancel, cancel := context.WithCancel(ctx)
		cancel() // immediately cancel

		_, err := store.Search(ctxCancel, query, 2)
		if err == nil {
			t.Error("expected error due to cancelled context, got nil")
		}
	})

	t.Run("large text chunk", func(t *testing.T) {
		largeText := string(make([]byte, 1024*1024)) // 1MB string
		err := store.AddDocument(ctx, "large_doc", largeText, []float32{0.1, 0.2})
		if err != nil {
			t.Fatalf("Failed to add large document: %v", err)
		}
	})

	t.Run("mismatched dimensions", func(t *testing.T) {
		err := store.AddDocument(ctx, "dim_mismatch", "text", []float32{1.0, 2.0, 3.0})
		if err != nil {
			t.Fatalf("failed to add doc: %v", err)
		}
		res, err := store.Search(ctx, []float32{1.0, 1.0}, 10)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		found := false
		for _, r := range res {
			if r.ID == "dim_mismatch" {
				found = true
				if r.Score != 0 {
					t.Errorf("expected score 0 for mismatched dimensions, got %f", r.Score)
				}
			}
		}
		if !found {
			t.Error("did not find dim_mismatch doc")
		}
	})
}

func TestStore_Errors(t *testing.T) {
	t.Run("invalid db path", func(t *testing.T) {
		_, err := NewStore("file:invalid?mode=ro")
		if err == nil {
			t.Error("expected error for invalid db configuration")
		}
	})

	t.Run("missing table", func(t *testing.T) {
		dbPath := filepath.Join(t.TempDir(), "missing_table.db")
		store, err := NewStore(dbPath)
		if err != nil {
			t.Fatal(err)
		}

		_, err = store.db.Exec("DROP TABLE documents")
		if err != nil {
			t.Fatal(err)
		}

		_, err = store.Search(context.Background(), []float32{1.0}, 2)
		if err == nil {
			t.Error("expected error searching when table is missing")
		}

		store.Close()
	})
}

func TestCosineSimilarity(t *testing.T) {
	tests := []struct {
		a, b []float32
		want float32
	}{
		{[]float32{1, 0}, []float32{1, 0}, 1},
		{[]float32{1, 0}, []float32{0, 1}, 0},
		{[]float32{1, 1}, []float32{1, 1}, 1},
		{[]float32{1, 0}, []float32{-1, 0}, -1},
		{[]float32{0, 0}, []float32{0, 0}, 0},
		{[]float32{1, 0}, []float32{1, 0, 0}, 0},
		{nil, nil, 0},
		{[]float32{}, []float32{}, 0},
	}
	for _, tt := range tests {
		got := cosineSimilarity(tt.a, tt.b)
		if got < tt.want-0.0001 || got > tt.want+0.0001 {
			t.Errorf("cosineSimilarity(%v, %v) = %v; want %v", tt.a, tt.b, got, tt.want)
		}
	}
}

func TestBytesFloatConversion(t *testing.T) {
	floats := []float32{1.23, 4.56, -7.89, 0.0}
	bytes := float32SliceToBytes(floats)
	floats2 := bytesToFloat32Slice(bytes)

	if len(floats) != len(floats2) {
		t.Fatalf("length mismatch: %d vs %d", len(floats), len(floats2))
	}
	for i := range floats {
		if floats[i] != floats2[i] {
			t.Errorf("mismatch at index %d: %v vs %v", i, floats[i], floats2[i])
		}
	}

	malformedBytes := append(bytes, 0x00, 0x01)
	floats3 := bytesToFloat32Slice(malformedBytes)
	if len(floats3) != len(floats) {
		t.Errorf("expected malformed bytes to ignore extra bytes, got len %d", len(floats3))
	}
}

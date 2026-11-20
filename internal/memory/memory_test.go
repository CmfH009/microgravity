package memory

import (
	"path/filepath"
	"testing"

	"github.com/CmfH009/microgravity/internal/rag"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMemoryStore_Embedding_Deterministic(t *testing.T) {
	dir := t.TempDir()
	store, err := NewMemoryStore(filepath.Join(dir, "test.db"))
	require.NoError(t, err)
	defer store.Close()

	e1 := store.Embedding("hello world")
	e2 := store.Embedding("hello world")
	assert.Equal(t, e1, e2, "same text must produce same embedding")

	e3 := store.Embedding("goodbye world")
	assert.NotEqual(t, e1, e3, "different text must produce different embedding")
}

func TestMemoryStore_Embedding_Dimension(t *testing.T) {
	dir := t.TempDir()
	store, err := NewMemoryStore(filepath.Join(dir, "test.db"))
	require.NoError(t, err)
	defer store.Close()

	e := store.Embedding("test")
	assert.Equal(t, defaultDim, len(e))
}

func TestMemoryStore_StoreAndRetrieve(t *testing.T) {
	dir := t.TempDir()
	store, err := NewMemoryStore(filepath.Join(dir, "test.db"))
	require.NoError(t, err)
	defer store.Close()

	sessionID := "test-session"

	// Store several turns
	err = store.StoreTurn(sessionID, "How do I write a Go function?", "Use the func keyword followed by the function name.")
	require.NoError(t, err)

	err = store.StoreTurn(sessionID, "What is the capital of France?", "The capital of France is Paris.")
	require.NoError(t, err)

	err = store.StoreTurn(sessionID, "Explain Go concurrency", "Go uses goroutines and channels for concurrency.")
	require.NoError(t, err)

	// Retrieve relevant results
	results, err := store.RetrieveRelevant("Go function syntax", 5)
	require.NoError(t, err)
	require.NotEmpty(t, results)

	// The Go function question should score highest
	SortResults(results)
	assert.Contains(t, results[0].ID, "user")
	assert.Contains(t, results[0].Text, "Go function")
}

func TestMemoryStore_EmptyQuery(t *testing.T) {
	dir := t.TempDir()
	store, err := NewMemoryStore(filepath.Join(dir, "test.db"))
	require.NoError(t, err)
	defer store.Close()

	results, err := store.RetrieveRelevant("", 5)
	require.NoError(t, err)
	assert.Empty(t, results)
}

func TestMemoryStore_FormatResult(t *testing.T) {
	r := FormatResult(rag.SearchResult{
		ID:    "test:1",
		Text:  "This is a test document with some content",
		Score: 0.85,
	})
	assert.Contains(t, r, "85%")
	assert.Contains(t, r, "test:1")
	assert.Contains(t, r, "This is a test document")
}

func TestSortResults(t *testing.T) {
	results := []rag.SearchResult{
		{ID: "a", Text: "a", Score: 0.1},
		{ID: "b", Text: "b", Score: 0.9},
		{ID: "c", Text: "c", Score: 0.5},
	}
	SortResults(results)
	assert.Equal(t, float32(0.9), results[0].Score)
	assert.Equal(t, float32(0.5), results[1].Score)
	assert.Equal(t, float32(0.1), results[2].Score)
}

func TestMemoryStore_Close(t *testing.T) {
	dir := t.TempDir()
	store, err := NewMemoryStore(filepath.Join(dir, "test.db"))
	require.NoError(t, err)
	assert.NoError(t, store.Close())
}

func TestTokenize(t *testing.T) {
	tokens := tokenize("Hello, world! This is a test.")
	assert.Contains(t, tokens, "hello")
	assert.Contains(t, tokens, "world")
	assert.Contains(t, tokens, "this")
	assert.Contains(t, tokens, "test")
	// Character trigrams should be present
	hasHel := false
	for _, t := range tokens {
		if t == "hel" {
			hasHel = true
		}
	}
	assert.True(t, hasHel, "expected 'hel' trigram in tokens")
}

// Package memory implements cross-session semantic memory for Microgravity.
//
// Conversation turns are embedded and stored in a vector store (SQLite with
// cosine similarity). On subsequent sessions, semantically similar past
// conversations are retrieved and injected as context, giving the agent
// continuity across sessions.
//
// Embeddings are generated deterministically offline (feature hashing), so no
// external embedding model or API is required.
package memory

import (
	"context"
	"fmt"
	"hash/fnv"
	"math"
	"sort"
	"strings"

	"github.com/CmfH009/microgravity/internal/rag"
)

const (
	defaultDim = 256
)

// MemoryStore stores and retrieves conversation turns using semantic search.
type MemoryStore struct {
	ragStore *rag.Store
	dim      int
}

// NewMemoryStore creates a memory store backed by the given SQLite database path.
func NewMemoryStore(dbPath string) (*MemoryStore, error) {
	rs, err := rag.NewStore(dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open rag store: %w", err)
	}
	return &MemoryStore{
		ragStore: rs,
		dim:      defaultDim,
	}, nil
}

// Close closes the underlying vector store.
func (m *MemoryStore) Close() error {
	return m.ragStore.Close()
}

// Embedding generates a deterministic embedding for text using feature hashing.
// The same text always produces the same vector, enabling offline semantic
// similarity without an external embedding model.
func (m *MemoryStore) Embedding(text string) []float32 {
	vec := make([]float32, m.dim)

	for _, tok := range tokenize(text) {
		h := fnv.New32a()
		h.Write([]byte(tok))
		idx := int(h.Sum32()) % m.dim
		vec[idx] += 1.0
	}

	// L2 normalize
	var norm float32
	for _, v := range vec {
		norm += v * v
	}
	norm = float32(math.Sqrt(float64(norm)))
	if norm > 0 {
		for i := range vec {
			vec[i] /= norm
		}
	}

	return vec
}

// tokenize splits text into lowercase words and character 3-grams.
func tokenize(text string) []string {
	text = strings.ToLower(text)
	words := strings.Fields(text)

	var tokens []string
	for _, w := range words {
		// Strip leading/trailing punctuation
		w = strings.Trim(w, ".,!?:;\"'()[]{}")
		if w != "" {
			tokens = append(tokens, w)
		}
	}

	// Character 3-grams for subword matching
	runes := []rune(text)
	for i := 0; i+3 <= len(runes); i++ {
		gram := string(runes[i : i+3])
		// Skip grams that are only whitespace
		if strings.TrimSpace(gram) != "" {
			tokens = append(tokens, gram)
		}
	}

	return tokens
}

// StoreTurn persists a conversation turn (user message + assistant response)
// as a searchable document in the vector store.
func (m *MemoryStore) StoreTurn(sessionID, userMsg, assistantMsg string) error {
	ctx := context.Background()

	// Store user message
	userEmbedding := m.Embedding(userMsg)
	userID := fmt.Sprintf("session:%s:user:%d", sessionID, hashString(userMsg))
	if err := m.ragStore.AddDocument(ctx, userID, "User: "+userMsg, userEmbedding); err != nil {
		return fmt.Errorf("failed to store user message: %w", err)
	}

	// Store assistant response
	asstEmbedding := m.Embedding(assistantMsg)
	asstID := fmt.Sprintf("session:%s:assistant:%d", sessionID, hashString(assistantMsg))
	if err := m.ragStore.AddDocument(ctx, asstID, "Assistant: "+assistantMsg, asstEmbedding); err != nil {
		return fmt.Errorf("failed to store assistant message: %w", err)
	}

	return nil
}

// RetrieveRelevant searches past conversations for content semantically
// similar to the query, returning the most relevant results.
func (m *MemoryStore) RetrieveRelevant(query string, topK int) ([]rag.SearchResult, error) {
	ctx := context.Background()
	queryEmbedding := m.Embedding(query)
	return m.ragStore.Search(ctx, queryEmbedding, topK)
}

// hashString returns a deterministic hash of a string for ID generation.
func hashString(s string) uint32 {
	h := fnv.New32a()
	h.Write([]byte(s))
	return h.Sum32()
}

// SortResults sorts search results by score descending.
func SortResults(results []rag.SearchResult) {
	sort.Slice(results, func(i, j int) bool {
		return results[i].Score > results[j].Score
	})
}

// FormatResult formats a single search result for display.
func FormatResult(r rag.SearchResult) string {
	return fmt.Sprintf("  %.0f%%  %s\n    %s", r.Score*100, r.ID, truncate(r.Text, 120))
}

// truncate limits a string to maxLen characters.
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

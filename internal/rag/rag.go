package rag

import (
	"container/heap"
	"context"
	"database/sql"
	"encoding/binary"
	"fmt"
	"math"

	_ "modernc.org/sqlite"
)

type Store struct {
	db *sql.DB
}

type SearchResult struct {
	ID    string
	Text  string
	Score float32
}

type searchResultHeap []SearchResult

func (h searchResultHeap) Len() int           { return len(h) }
func (h searchResultHeap) Less(i, j int) bool { return h[i].Score < h[j].Score }
func (h searchResultHeap) Swap(i, j int)      { h[i], h[j] = h[j], h[i] }

func (h *searchResultHeap) Push(x any) {
	*h = append(*h, x.(SearchResult))
}

func (h *searchResultHeap) Pop() any {
	old := *h
	n := len(old)
	x := old[n-1]
	*h = old[0 : n-1]
	return x
}

func NewStore(dbPath string) (*Store, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS documents (
			id TEXT PRIMARY KEY,
			text TEXT,
			embedding BLOB
		)
	`)
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to create table: %w", err)
	}

	return &Store{db: db}, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

func float32SliceToBytes(floats []float32) []byte {
	bytes := make([]byte, len(floats)*4)
	for i, f := range floats {
		binary.LittleEndian.PutUint32(bytes[i*4:], math.Float32bits(f))
	}
	return bytes
}

func bytesToFloat32Slice(bytes []byte) []float32 {
	floats := make([]float32, len(bytes)/4)
	for i := range floats {
		floats[i] = math.Float32frombits(binary.LittleEndian.Uint32(bytes[i*4:]))
	}
	return floats
}

func cosineSimilarity(a, b []float32) float32 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}
	var dotProduct, normA, normB float32
	for i := range a {
		dotProduct += a[i] * b[i]
		normA += a[i] * a[i]
		normB += b[i] * b[i]
	}
	if normA == 0 || normB == 0 {
		return 0
	}
	return dotProduct / (float32(math.Sqrt(float64(normA))) * float32(math.Sqrt(float64(normB))))
}

func (s *Store) AddDocument(ctx context.Context, id string, text string, embedding []float32) error {
	embeddingBytes := float32SliceToBytes(embedding)
	_, err := s.db.ExecContext(ctx, "INSERT OR REPLACE INTO documents (id, text, embedding) VALUES (?, ?, ?)", id, text, embeddingBytes)
	return err
}

func (s *Store) DeleteDocumentsForFile(ctx context.Context, filePath string) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM documents WHERE id LIKE ?", filePath+":L%")
	return err
}

func (s *Store) Search(ctx context.Context, query []float32, topK int) ([]SearchResult, error) {
	if topK <= 0 {
		return nil, nil
	}

	rows, err := s.db.QueryContext(ctx, "SELECT id, text, embedding FROM documents")
	if err != nil {
		return nil, fmt.Errorf("failed to query documents: %w", err)
	}
	defer rows.Close()

	h := &searchResultHeap{}
	heap.Init(h)

	for rows.Next() {
		// Defensive context check for early termination during large dataset iteration
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		var id, text string
		var embeddingBytes []byte
		if err := rows.Scan(&id, &text, &embeddingBytes); err != nil {
			return nil, fmt.Errorf("failed to scan row: %w", err)
		}

		docEmbedding := bytesToFloat32Slice(embeddingBytes)
		score := cosineSimilarity(query, docEmbedding)

		heap.Push(h, SearchResult{
			ID:    id,
			Text:  text,
			Score: score,
		})

		if h.Len() > topK {
			heap.Pop(h)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	results := make([]SearchResult, h.Len())
	for i := len(results) - 1; i >= 0; i-- {
		results[i] = heap.Pop(h).(SearchResult)
	}

	return results, nil
}

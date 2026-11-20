# Codebase Indexer (`mg index` / `mg query`) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a high-performance codebase indexer and RAG query engine into `mg` with incremental scanning, AST/line chunking, hybrid embeddings (Ollama + lexical TF-IDF fallback), SQLite vector storage, and interactive TUI output.

**Architecture:** A modular `internal/indexer` package handles file discovery, chunking, embedding, and manifest tracking, while existing `internal/rag` manages SQLite vector storage and Cosine Similarity math. CLI commands `cmd/index.go` and `cmd/query.go` expose `mg index` and `mg query`.

**Tech Stack:** Go 1.22+, SQLite (`modernc.org/sqlite`), LipGloss TUI formatting, Ollama REST API.

---

### Task 1: Source Code Chunker (`internal/indexer/chunker.go`)

**Files:**
- Create: `internal/indexer/chunker.go`
- Test: `internal/indexer/chunker_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/indexer/chunker_test.go`:
```go
package indexer

import (
	"testing"
)

func TestChunker_ChunkFile(t *testing.T) {
	content := `package main

import "fmt"

func main() {
	fmt.Println("Hello, World!")
}
`
	chunks := ChunkFile("main.go", content, 3, 1)
	if len(chunks) == 0 {
		t.Fatalf("expected at least 1 chunk, got 0")
	}

	if chunks[0].FilePath != "main.go" {
		t.Errorf("expected FilePath 'main.go', got %q", chunks[0].FilePath)
	}

	if chunks[0].StartLine != 1 {
		t.Errorf("expected StartLine 1, got %d", chunks[0].StartLine)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/indexer`
Expected: FAIL with "undefined: ChunkFile"

- [ ] **Step 3: Write implementation**

Create `internal/indexer/chunker.go`:
```go
package indexer

import (
	"crypto/sha256"
	"fmt"
	"strings"
)

type Chunk struct {
	ID        string `json:"id"`
	FilePath  string `json:"filepath"`
	StartLine int    `json:"start_line"`
	EndLine   int    `json:"end_line"`
	Content   string `json:"content"`
}

// ChunkFile splits text into chunks of chunkSize lines with overlap lines.
func ChunkFile(filePath string, content string, chunkSize, overlap int) []Chunk {
	lines := strings.Split(content, "\n")
	if len(lines) == 0 || strings.TrimSpace(content) == "" {
		return nil
	}

	if chunkSize <= 0 {
		chunkSize = 30
	}
	if overlap < 0 || overlap >= chunkSize {
		overlap = 5
	}

	var chunks []Chunk
	step := chunkSize - overlap
	if step <= 0 {
		step = 1
	}

	for i := 0; i < len(lines); i += step {
		end := i + chunkSize
		if end > len(lines) {
			end = len(lines)
		}

		chunkLines := lines[i:end]
		chunkText := strings.Join(chunkLines, "\n")

		if strings.TrimSpace(chunkText) == "" {
			if end >= len(lines) {
				break
			}
			continue
		}

		startLine := i + 1
		endLine := end

		h := sha256.New()
		h.Write([]byte(fmt.Sprintf("%s:%d-%d:%s", filePath, startLine, endLine, chunkText)))
		id := fmt.Sprintf("%x", h.Sum(nil))[:16]

		chunks = append(chunks, Chunk{
			ID:        id,
			FilePath:  filePath,
			StartLine: startLine,
			EndLine:   endLine,
			Content:   chunkText,
		})

		if end >= len(lines) {
			break
		}
	}

	return chunks
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/indexer`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/indexer/chunker.go internal/indexer/chunker_test.go
git commit -m "feat(indexer): add source code file chunker and unit tests"
```

---

### Task 2: Hybrid Embedding Provider (`internal/indexer/embedder.go`)

**Files:**
- Create: `internal/indexer/embedder.go`
- Test: `internal/indexer/embedder_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/indexer/embedder_test.go`:
```go
package indexer

import (
	"context"
	"testing"
)

func TestLexicalEmbedder_Embed(t *testing.T) {
	embedder := &LexicalEmbedder{Dimension: 384}
	vec, err := embedder.Embed(context.Background(), "function main package fmt")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(vec) != 384 {
		t.Errorf("expected vector dimension 384, got %d", len(vec))
	}
}

func TestHybridEmbedder_Fallback(t *testing.T) {
	hybrid := NewHybridEmbedder("http://invalid-ollama-port-9999", "nomic-embed-text")
	vec, err := hybrid.Embed(context.Background(), "hello world")
	if err != nil {
		t.Fatalf("expected fallback to succeed, got error: %v", err)
	}

	if len(vec) != 384 {
		t.Errorf("expected dimension 384 from fallback embedder, got %d", len(vec))
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/indexer`
Expected: FAIL with "undefined: LexicalEmbedder"

- [ ] **Step 3: Write implementation**

Create `internal/indexer/embedder.go`:
```go
package indexer

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"math"
	"net/http"
	"strings"
	"time"
)

type Embedder interface {
	Embed(ctx context.Context, text string) ([]float32, error)
}

type LexicalEmbedder struct {
	Dimension int
}

func NewLexicalEmbedder(dim int) *LexicalEmbedder {
	if dim <= 0 {
		dim = 384
	}
	return &LexicalEmbedder{Dimension: dim}
}

func (l *LexicalEmbedder) Embed(ctx context.Context, text string) ([]float32, error) {
	vec := make([]float32, l.Dimension)
	words := strings.Fields(strings.ToLower(text))
	if len(words) == 0 {
		return vec, nil
	}

	for _, word := range words {
		h := fnv.New32a()
		h.Write([]byte(word))
		idx := int(h.Sum32()) % l.Dimension
		vec[idx] += 1.0
	}

	// Normalize vector L2 norm
	var norm float64
	for _, val := range vec {
		norm += float64(val * val)
	}
	norm = math.Sqrt(norm)
	if norm > 0 {
		for i := range vec {
			vec[i] = float32(float64(vec[i]) / norm)
		}
	}

	return vec, nil
}

type HybridEmbedder struct {
	OllamaBaseURL string
	Model         string
	HTTPClient    *http.Client
	Fallback      Embedder
}

func NewHybridEmbedder(ollamaURL, model string) *HybridEmbedder {
	if ollamaURL == "" {
		ollamaURL = "http://localhost:11434"
	}
	if model == "" {
		model = "nomic-embed-text"
	}
	return &HybridEmbedder{
		OllamaBaseURL: ollamaURL,
		Model:         model,
		HTTPClient:    &http.Client{Timeout: 3 * time.Second},
		Fallback:      NewLexicalEmbedder(384),
	}
}

type ollamaEmbedRequest struct {
	Model  string `json:"model"`
	Prompt string `json:"prompt"`
}

type ollamaEmbedResponse struct {
	Embedding []float32 `json:"embedding"`
}

func (h *HybridEmbedder) Embed(ctx context.Context, text string) ([]float32, error) {
	reqBody := ollamaEmbedRequest{
		Model:  h.Model,
		Prompt: text,
	}
	buf := new(bytes.Buffer)
	if err := json.NewEncoder(buf).Encode(reqBody); err != nil {
		return h.Fallback.Embed(ctx, text)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", h.OllamaBaseURL+"/api/embeddings", buf)
	if err != nil {
		return h.Fallback.Embed(ctx, text)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := h.HTTPClient.Do(req)
	if err != nil || resp.StatusCode != http.StatusOK {
		if resp != nil {
			resp.Body.Close()
		}
		return h.Fallback.Embed(ctx, text)
	}
	defer resp.Body.Close()

	var embedResp ollamaEmbedResponse
	if err := json.NewDecoder(resp.Body).Decode(&embedResp); err != nil || len(embedResp.Embedding) == 0 {
		return h.Fallback.Embed(ctx, text)
	}

	return embedResp.Embedding, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/indexer`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/indexer/embedder.go internal/indexer/embedder_test.go
git commit -m "feat(indexer): add hybrid embedder with Ollama API and lexical fallback"
```

---

### Task 3: Indexer Manifest & Core Engine (`internal/indexer/indexer.go`)

**Files:**
- Create: `internal/indexer/manifest.go`
- Create: `internal/indexer/indexer.go`
- Test: `internal/indexer/indexer_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/indexer/indexer_test.go`:
```go
package indexer

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/purav/microgravity/internal/rag"
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/indexer`
Expected: FAIL with "undefined: NewIndexer"

- [ ] **Step 3: Write implementation**

Create `internal/indexer/manifest.go`:
```go
package indexer

import (
	"encoding/json"
	"os"
	"path/filepath"
)

type FileMeta struct {
	Hash       string `json:"hash"`
	ModTime    int64  `json:"mod_time"`
	ChunkCount int    `json:"chunk_count"`
}

type Manifest struct {
	Files map[string]FileMeta `json:"files"`
}

func LoadManifest(manifestPath string) (*Manifest, error) {
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return &Manifest{Files: make(map[string]FileMeta)}, nil
	}
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return &Manifest{Files: make(map[string]FileMeta)}, nil
	}
	if m.Files == nil {
		m.Files = make(map[string]FileMeta)
	}
	return &m, nil
}

func (m *Manifest) Save(manifestPath string) error {
	dir := filepath.Dir(manifestPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(manifestPath, data, 0644)
}
```

Create `internal/indexer/indexer.go`:
```go
package indexer

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/purav/microgravity/internal/rag"
)

type Indexer struct {
	Store    *rag.Store
	Embedder Embedder
	RootDir  string
}

func NewIndexer(store *rag.Store, embedder Embedder, rootDir string) *Indexer {
	return &Indexer{
		Store:    store,
		Embedder: embedder,
		RootDir:  rootDir,
	}
}

var supportedExtensions = map[string]bool{
	".go":   true,
	".py":   true,
	".ts":   true,
	".js":   true,
	".jsx":  true,
	".tsx":  true,
	".md":   true,
	".json": true,
	".rs":   true,
	".c":    true,
	".cpp":  true,
	".h":    true,
}

var ignoredDirs = map[string]bool{
	".git":         true,
	".worktrees":   true,
	"node_modules": true,
	"vendor":       true,
	"dist":         true,
	"bin":          true,
	"build":        true,
	".idea":        true,
	".vscode":      true,
}

func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", h.Sum(nil)), nil
}

func (idx *Indexer) IndexDirectory(ctx context.Context, targetDir string) (int, error) {
	manifestPath := filepath.Join(idx.RootDir, ".mg", "index_manifest.json")
	manifest, _ := LoadManifest(manifestPath)

	totalChunks := 0

	err := filepath.Walk(targetDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}

		if info.IsDir() {
			if ignoredDirs[info.Name()] {
				return filepath.SkipDir
			}
			return nil
		}

		ext := strings.ToLower(filepath.Ext(path))
		if !supportedExtensions[ext] {
			return nil
		}

		relPath, err := filepath.Rel(idx.RootDir, path)
		if err != nil {
			relPath = path
		}

		// Incremental check
		hash, err := fileSHA256(path)
		if err == nil {
			if meta, exists := manifest.Files[relPath]; exists {
				if meta.Hash == hash && meta.ModTime == info.ModTime().Unix() {
					totalChunks += meta.ChunkCount
					return nil // Skip unchanged file
				}
			}
		}

		content, err := os.ReadFile(path)
		if err != nil {
			return nil
		}

		chunks := ChunkFile(relPath, string(content), 30, 5)
		for _, chunk := range chunks {
			vec, err := idx.Embedder.Embed(ctx, chunk.Content)
			if err != nil {
				continue
			}

			docID := fmt.Sprintf("%s:L%d-L%d", chunk.FilePath, chunk.StartLine, chunk.EndLine)
			formattedText := fmt.Sprintf("[%s L%d-L%d]\n%s", chunk.FilePath, chunk.StartLine, chunk.EndLine, chunk.Content)
			_ = idx.Store.AddDocument(ctx, docID, formattedText, vec)
			totalChunks++
		}

		manifest.Files[relPath] = FileMeta{
			Hash:       hash,
			ModTime:    info.ModTime().Unix(),
			ChunkCount: len(chunks),
		}

		return nil
	})

	if err != nil {
		return 0, err
	}

	_ = manifest.Save(manifestPath)
	return totalChunks, nil
}

func (idx *Indexer) Query(ctx context.Context, queryText string, topK int) ([]rag.SearchResult, error) {
	qVec, err := idx.Embedder.Embed(ctx, queryText)
	if err != nil {
		return nil, fmt.Errorf("failed to embed query: %w", err)
	}

	return idx.Store.Search(ctx, qVec, topK)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/indexer`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/indexer/manifest.go internal/indexer/indexer.go internal/indexer/indexer_test.go
git commit -m "feat(indexer): implement incremental codebase indexer and manifest tracking"
```

---

### Task 4: CLI Commands (`cmd/index.go` & `cmd/query.go`)

**Files:**
- Create: `cmd/index.go`
- Create: `cmd/query.go`

- [ ] **Step 1: Implement `mg index` command (`cmd/index.go`)**

Create `cmd/index.go`:
```go
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
```

- [ ] **Step 2: Implement `mg query` command (`cmd/query.go`)**

Create `cmd/query.go`:
```go
package cmd

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/charmbracelet/lipgloss"
	"github.com/purav/microgravity/internal/indexer"
	"github.com/purav/microgravity/internal/rag"
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
```

- [ ] **Step 3: Test compilation and all package tests**

Run: `go test ./...`
Expected: ALL PASS

- [ ] **Step 4: Rebuild binaries and commit**

```bash
go build -o mg.exe main.go
git add cmd/index.go cmd/query.go
git commit -m "feat(cli): add 'mg index' and 'mg query' commands for codebase RAG"
```

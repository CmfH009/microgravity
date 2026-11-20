# Design Spec: Streaming RAG & Codebase Indexer (`mg index` / `mg query`)

**Date**: 2026-08-03  
**Status**: Approved  
**Target Package**: `github.com/purav/microgravity/internal/indexer` & `cmd`  

---

## 1. Overview
The Codebase Indexer enables Microgravity (`mg`) to create persistent semantic vector indices of local repositories. By chunking source code, generating vector embeddings (via Ollama or built-in TF-IDF fallback), and querying SQLite vector storage, agents and developers can instantly retrieve relevant context across large codebases.

---

## 2. Requirements & Goals

### Functional Requirements
1. **Repository Indexing (`mg index [path]`)**:
   - Recursively scan workspace directories for source files (`.go`, `.py`, `.ts`, `.js`, `.md`, `.json`, `.toml`, `.yaml`, `.rs`, `.c`, `.cpp`, `.h`).
   - Automatically ignore non-code/heavy directories: `.git`, `.worktrees`, `node_modules`, `vendor`, `dist`, `bin`, `.idea`, `.vscode`, and binary file extensions.
   - Perform incremental re-indexing based on file modification timestamps and SHA256 content hashes.
   - Chunk files into overlapping semantic blocks (30 lines with 5 line overlap) preserving file path and line numbers (`start_line`, `end_line`).

2. **Hybrid Embedding Provider**:
   - Primary: Ollama POST `/api/embeddings` (using `nomic-embed-text` or current model).
   - Fallback: Deterministic 384-dimensional term-frequency / lexical vectorizer (`LexicalTFIDFEmbedder`) when Ollama is offline.

3. **Semantic Querying (`mg query <prompt>`)**:
   - Embed search query string using the active embedder.
   - Query `internal/rag` SQLite vector store using Cosine Similarity math.
   - Render top $K$ ($K=5$ default) matching code snippets with relevance score, file path, line range, and code snippet cards in LipGloss TUI formatting.

---

## 3. Architecture & Package Structure

```
microgravity/
├── internal/
│   ├── rag/               # Vector storage & Cosine similarity (Existing)
│   │   ├── rag.go
│   │   └── rag_test.go
│   └── indexer/           # New Indexer engine
│       ├── chunker.go     # File scanner & source code chunker
│       ├── embedder.go    # Ollama + TF-IDF hybrid embedding provider
│       ├── manifest.go    # File hash & modtime tracking for incremental builds
│       ├── indexer.go     # Core Indexer service orchestrator
│       └── indexer_test.go# Unit & integration tests
├── cmd/
│   ├── index.go           # mg index CLI command
│   └── query.go           # mg query CLI command
```

---

## 4. Component Details

### 4.1 Chunker (`internal/indexer/chunker.go`)
```go
type Chunk struct {
    ID        string `json:"id"`        // e.g. "internal/rag/rag.go:L10-L40"
    FilePath  string `json:"filepath"`  // e.g. "internal/rag/rag.go"
    StartLine int    `json:"start_line"`
    EndLine   int    `json:"end_line"`
    Content   string `json:"content"`
}

func ChunkFile(path string, content string) []Chunk
```

### 4.2 Hybrid Embedder (`internal/indexer/embedder.go`)
```go
type Embedder interface {
    Embed(ctx context.Context, text string) ([]float32, error)
}

type OllamaEmbedder struct {
    Client *ollama.Client
    Model  string
}

type LexicalEmbedder struct{}
```
If `OllamaEmbedder.Embed` returns an error (e.g. connection refused, daemon offline), the `HybridEmbedder` automatically delegates to `LexicalEmbedder.Embed`.

### 4.3 Manifest & Incremental Engine (`internal/indexer/manifest.go`)
Tracks index metadata in `.mg/index_manifest.json`:
```json
{
  "files": {
    "internal/rag/rag.go": {
      "hash": "a1b2c3...",
      "mod_time": 1722690000,
      "chunk_count": 4
    }
  }
}
```

---

## 5. Testing & Quality Standards

1. Unit tests for `ChunkFile` verifying line bounds and overlap behavior.
2. Unit tests for `HybridEmbedder` testing fallback execution when Ollama endpoint returns error.
3. Unit tests for `Indexer` verifying incremental skip logic when file modtime/hash is unchanged.
4. End-to-end integration test creating a temporary directory with test files, indexing them, and searching.

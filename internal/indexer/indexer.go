package indexer

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/CmfH009/microgravity/internal/rag"
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
	manifest, err := LoadManifest(manifestPath)
	if err != nil {
		return 0, fmt.Errorf("load index manifest: %w", err)
	}

	seenFiles := make(map[string]bool)
	totalChunks := 0

	err = filepath.Walk(targetDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
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

		seenFiles[relPath] = true
		// Remove prior chunks before re-indexing a changed file.
		if old, exists := manifest.Files[relPath]; exists {
			if err := idx.Store.DeleteDocumentsForFile(ctx, relPath); err != nil {
				return err
			}
			_ = old
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
			return err
		}

		chunks := ChunkFile(relPath, string(content), 30, 5)
		for _, chunk := range chunks {
			vec, err := idx.Embedder.Embed(ctx, chunk.Content)
			if err != nil {
				return err
			}

			docID := fmt.Sprintf("%s:L%d-L%d", chunk.FilePath, chunk.StartLine, chunk.EndLine)
			formattedText := fmt.Sprintf("[%s L%d-L%d]\n%s", chunk.FilePath, chunk.StartLine, chunk.EndLine, chunk.Content)
			if err := idx.Store.AddDocument(ctx, docID, formattedText, vec); err != nil {
				return err
			}
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

	for path := range manifest.Files {
		if !seenFiles[path] {
			delete(manifest.Files, path)
		}
	}
	if err := manifest.Save(manifestPath); err != nil {
		return 0, fmt.Errorf("save index manifest: %w", err)
	}
	return totalChunks, nil
}

func (idx *Indexer) Query(ctx context.Context, queryText string, topK int) ([]rag.SearchResult, error) {
	qVec, err := idx.Embedder.Embed(ctx, queryText)
	if err != nil {
		return nil, fmt.Errorf("failed to embed query: %w", err)
	}

	return idx.Store.Search(ctx, qVec, topK)
}

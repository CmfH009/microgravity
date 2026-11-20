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

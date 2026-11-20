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

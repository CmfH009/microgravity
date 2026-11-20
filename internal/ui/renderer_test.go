package ui

import (
	"strings"
	"testing"
)

func TestRenderMarkdown(t *testing.T) {
	renderer, err := NewArtifactRenderer()
	if err != nil {
		t.Fatalf("Failed to create renderer: %v", err)
	}
	renderer.isTTY = true

	markdown := "# Hello World"
	output, err := renderer.Render(markdown)
	if err != nil {
		t.Fatalf("Failed to render markdown: %v", err)
	}

	if !strings.Contains(output, "Hello World") {
		t.Errorf("Expected output to contain 'Hello World', got: %q", output)
	}
}

func TestRenderAlerts(t *testing.T) {
	input := "> [!NOTE]\n> This is an important test note.\n"
	out := RenderAlerts(input)

	if !strings.Contains(out, "NOTE") || !strings.Contains(out, "This is an important test note.") {
		t.Errorf("alert rendering failed, got: %s", out)
	}
}

func TestGracefulDegradation(t *testing.T) {
	renderer, err := NewArtifactRenderer()
	if err != nil {
		t.Fatalf("Failed to create renderer: %v", err)
	}

	renderer.isTTY = false

	markdown := "# Hello World"
	output, err := renderer.Render(markdown)
	if err != nil {
		t.Fatalf("Failed to render markdown: %v", err)
	}

	if output != markdown+"\n" {
		t.Errorf("Expected output %q, got: %q", markdown+"\n", output)
	}
}

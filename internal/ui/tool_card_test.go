package ui

import (
	"strings"
	"testing"
	"time"
)

func TestRenderToolCallCard(t *testing.T) {
	toolName := "read_file"
	args := map[string]interface{}{"path": "C:\\AgyHut\\microgravity\\PLAN.md"}
	duration := 15 * time.Millisecond
	output := "File content preview line 1\nFile content preview line 2"

	card := RenderToolCallCard(toolName, args, duration, true, output)
	if card == "" {
		t.Fatalf("Expected non-empty card output")
	}

	if !strings.Contains(card, "read_file") {
		t.Errorf("Card should contain tool name")
	}
	if !strings.Contains(card, "SUCCESS") {
		t.Errorf("Card should contain status SUCCESS")
	}
}

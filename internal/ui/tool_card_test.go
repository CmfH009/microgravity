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

func TestRenderCollapsibleToolCard_CollapsedVsExpanded(t *testing.T) {
	args := map[string]interface{}{
		"path":  "main.go",
		"limit": 50,
	}
	output := "line 1\nline 2\nline 3\nline 4\nline 5\nline 6\nline 7\nline 8"
	dur := 42 * time.Millisecond

	collapsed := RenderCollapsibleToolCard("read_file", args, dur, true, output, false)
	expanded := RenderCollapsibleToolCard("read_file", args, dur, true, output, true)

	collapsedLines := strings.Split(strings.TrimSpace(collapsed), "\n")
	expandedLines := strings.Split(strings.TrimSpace(expanded), "\n")

	if len(collapsedLines) != 3 {
		t.Errorf("expected compact 1-line bordered summary (3 total lines), got %d lines:\n%s", len(collapsedLines), collapsed)
	}
	if len(expandedLines) <= len(collapsedLines) {
		t.Errorf("expected expanded card (%d lines) to be taller than collapsed card (%d lines)", len(expandedLines), len(collapsedLines))
	}

	if !strings.Contains(collapsed, "read_file") || !strings.Contains(collapsed, "SUCCESS") {
		t.Errorf("collapsed card missing tool name or status: %s", collapsed)
	}
	if strings.Contains(collapsed, "Parameters:") || strings.Contains(collapsed, "Output Preview:") {
		t.Errorf("collapsed card should not include full Parameters or Output Preview: %s", collapsed)
	}

	if !strings.Contains(expanded, "Parameters:") || !strings.Contains(expanded, "Output Preview:") {
		t.Errorf("expanded card missing Parameters or Output Preview: %s", expanded)
	}
	if !strings.Contains(expanded, "2 lines truncated") {
		t.Errorf("expanded card should truncate output beyond 6 lines, got:\n%s", expanded)
	}

	failed := RenderCollapsibleToolCard("exec_cmd", nil, dur, false, "exit status 1", false)
	if !strings.Contains(failed, "FAILED") {
		t.Errorf("expected FAILED badge in failed collapsed card, got:\n%s", failed)
	}
}

func TestRenderToolCard_DynamicThemeSwitch(t *testing.T) {
	defer SetTheme("dark")

	SetTheme("dark")
	darkPal := currentToolCardPalette()

	SetTheme("tokyo-night")
	tokyoPal := currentToolCardPalette()

	if darkPal.boxSuccess.GetBorderTopForeground() == tokyoPal.boxSuccess.GetBorderTopForeground() {
		t.Errorf("expected tool card border color to change dynamically between dark and tokyo-night themes")
	}
	if darkPal.header.GetForeground() == tokyoPal.header.GetForeground() {
		t.Errorf("expected tool card header color to change dynamically between dark and tokyo-night themes")
	}
	if ToolCardBoxSuccess.GetBorderTopForeground() != tokyoPal.boxSuccess.GetBorderTopForeground() {
		t.Errorf("expected exported ToolCardBoxSuccess to stay synced with SetTheme")
	}
}

func TestRenderToolCallCard_TruncatesLongLinesAndFormatsDuration(t *testing.T) {
	longQuery := strings.Repeat("word ", 60) // 300 chars with newlines
	longOutput := strings.Repeat("x", 220) + "\nsecond line"
	dur := 1645407100 * time.Nanosecond // 1.6454071s -> 1.65s

	card := RenderToolCallCard("search_web", map[string]interface{}{"query": longQuery}, dur, true, longOutput)
	if !strings.Contains(card, "1.65s") {
		t.Errorf("expected rounded duration 1.65s, got:\n%s", card)
	}
	if strings.Contains(card, "1.6454071s") {
		t.Errorf("unexpected raw nanosecond duration in card:\n%s", card)
	}
	if strings.HasSuffix(card, " ") {
		t.Errorf("expected no trailing margin space line on card")
	}
	for _, line := range strings.Split(card, "\n") {
		if len([]rune(line)) > 140 {
			t.Errorf("expected card line width to remain bounded, got %d runes: %s", len([]rune(line)), line)
		}
	}
}

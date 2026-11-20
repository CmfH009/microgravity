package rules

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadRules(t *testing.T) {
	tempDir := t.TempDir()

	globalRuleFile := filepath.Join(tempDir, "global_rules.md")
	globalContent := "# Global Rules\n- Rule 1: Always verify code.\n"
	if err := os.WriteFile(globalRuleFile, []byte(globalContent), 0644); err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	workspaceRuleFile := filepath.Join(tempDir, "workspace_rules.md")
	workspaceContent := "# Workspace Rules\n- Rule 2: Use Go 1.22+ conventions.\n"
	if err := os.WriteFile(workspaceRuleFile, []byte(workspaceContent), 0644); err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	r, err := LoadFromFiles(globalRuleFile, workspaceRuleFile)
	if err != nil {
		t.Fatalf("LoadFromFiles returned unexpected error: %v", err)
	}

	prompt := r.FormatForSystemPrompt()
	if prompt == "" {
		t.Errorf("Expected formatted system prompt, got empty string")
	}

	if !containsSubstring(prompt, "Rule 1: Always verify code.") {
		t.Errorf("System prompt missing global rule content")
	}
	if !containsSubstring(prompt, "Rule 2: Use Go 1.22+ conventions.") {
		t.Errorf("System prompt missing workspace rule content")
	}
}

func containsSubstring(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || searchSub(s, sub))
}

func searchSub(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

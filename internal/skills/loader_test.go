package skills

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadAll(t *testing.T) {
	// Create a temp directory with some skill files
	dir := t.TempDir()

	// Valid prompt skill
	promptSkill := `---
name: code-review
description: A code review helper
---
# Code Review Guide
Review code for correctness and style.
`
	if err := os.WriteFile(filepath.Join(dir, "code-review.md"), []byte(promptSkill), 0644); err != nil {
		t.Fatal(err)
	}

	// Valid tool skill
	toolSkill := `---
name: git-status
description: Show git status
type: tool
command: git status
---
Runs git status in the current directory.
`
	if err := os.WriteFile(filepath.Join(dir, "git-status.md"), []byte(toolSkill), 0644); err != nil {
		t.Fatal(err)
	}

	// Invalid file (no frontmatter)
	invalid := `# Just a plain markdown file
No frontmatter here.
`
	if err := os.WriteFile(filepath.Join(dir, "invalid.md"), []byte(invalid), 0644); err != nil {
		t.Fatal(err)
	}

	// Non-markdown file (should be skipped)
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("not a skill"), 0644); err != nil {
		t.Fatal(err)
	}

	loaded, err := LoadDir(dir)
	if err != nil {
		t.Fatalf("LoadDir() returned error: %v", err)
	}

	if len(loaded) != 2 {
		t.Fatalf("LoadAll() loaded %d skills, want 2", len(loaded))
	}

	// Check that we got the right skills (order may vary)
	names := map[string]bool{}
	for _, s := range loaded {
		names[s.Name] = true
	}
	if !names["code-review"] {
		t.Error("missing skill 'code-review'")
	}
	if !names["git-status"] {
		t.Error("missing skill 'git-status'")
	}
}

func TestLoadAllEmptyDir(t *testing.T) {
	dir := t.TempDir()
	loaded, err := LoadDir(dir)
	if err != nil {
		t.Fatalf("LoadDir() returned error: %v", err)
	}
	if len(loaded) != 0 {
		t.Fatalf("LoadAll() loaded %d skills from empty dir, want 0", len(loaded))
	}
}

func TestLoadAllNonexistentDir(t *testing.T) {
	_, err := LoadDir(filepath.Join(t.TempDir(), "does-not-exist"))
	if err == nil {
		t.Fatal("LoadDir() expected error for nonexistent dir, got nil")
	}
}

func TestLoadAllSubdirectory(t *testing.T) {
	dir := t.TempDir()
	subdir := filepath.Join(dir, "nested", "deep")
	if err := os.MkdirAll(subdir, 0755); err != nil {
		t.Fatal(err)
	}

	skill := `---
name: nested-skill
description: Found in a subdirectory
---
# Nested Skill
This skill is deeply nested.
`
	if err := os.WriteFile(filepath.Join(subdir, "nested.md"), []byte(skill), 0644); err != nil {
		t.Fatal(err)
	}

	loaded, err := LoadDir(dir)
	if err != nil {
		t.Fatalf("LoadDir() returned error: %v", err)
	}
	if len(loaded) != 1 {
		t.Fatalf("LoadAll() loaded %d skills, want 1", len(loaded))
	}
	if loaded[0].Name != "nested-skill" {
		t.Errorf("loaded skill name = %q, want %q", loaded[0].Name, "nested-skill")
	}
}

package audit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAuditWorkspaceCleanGo(t *testing.T) {
	tempDir := t.TempDir()

	// Create go.mod, .gitignore, PLAN.md, and sample test file
	os.WriteFile(filepath.Join(tempDir, "go.mod"), []byte("module testmod\n\ngo 1.22\n"), 0644)
	os.WriteFile(filepath.Join(tempDir, ".gitignore"), []byte("*.exe\n"), 0644)
	os.WriteFile(filepath.Join(tempDir, "PLAN.md"), []byte("# Plan\n"), 0644)
	os.WriteFile(filepath.Join(tempDir, "sample_test.go"), []byte("package testmod\n"), 0644)
	os.WriteFile(filepath.Join(tempDir, "README.md"), []byte("# Test Project\n"), 0644)

	res, err := AuditWorkspace(tempDir)
	if err != nil {
		t.Fatalf("AuditWorkspace failed: %v", err)
	}

	if res.Grade != "Grade A" {
		t.Errorf("Expected Grade A for clean project, got %s (Score: %d)", res.Grade, res.Score)
	}

	card := res.RenderScorecard()
	if !strings.Contains(card, "Grade A") {
		t.Errorf("Scorecard output missing Grade A")
	}
}

func TestAuditWorkspaceNeedsPatches(t *testing.T) {
	tempDir := t.TempDir()

	// Project missing README and tests
	os.WriteFile(filepath.Join(tempDir, "go.mod"), []byte("module testmod\n\ngo 1.22\n"), 0644)
	os.WriteFile(filepath.Join(tempDir, "main.go"), []byte("package main\n"), 0644)

	res, err := AuditWorkspace(tempDir)
	if err != nil {
		t.Fatalf("AuditWorkspace failed: %v", err)
	}

	if res.Score >= 100 {
		t.Errorf("Expected score penalty for missing README/tests, got %d", res.Score)
	}

	patchedCount := res.ApplyPatches(tempDir)
	if patchedCount == 0 {
		t.Errorf("Expected at least 1 patch to be applied")
	}

	// Verify README was created
	if _, err := os.Stat(filepath.Join(tempDir, "README.md")); err != nil {
		t.Errorf("Expected README.md to be created by patch: %v", err)
	}
}

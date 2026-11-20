package context

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInjectContext(t *testing.T) {
	tempDir := t.TempDir()
	sampleFile := filepath.Join(tempDir, "sample.go")
	os.WriteFile(sampleFile, []byte("package sample\nfunc Hello() {}\n"), 0644)

	prompt := "Refactor this code @" + sampleFile

	cleanPrompt, injectedText, err := ProcessFileReferences(prompt)
	if err != nil {
		t.Fatalf("ProcessFileReferences failed: %v", err)
	}

	if !strings.Contains(cleanPrompt, "Refactor this code") {
		t.Errorf("Expected cleanPrompt to preserve user query, got: %s", cleanPrompt)
	}

	if !strings.Contains(injectedText, "package sample") {
		t.Errorf("Expected injectedText to contain file content, got: %s", injectedText)
	}
}

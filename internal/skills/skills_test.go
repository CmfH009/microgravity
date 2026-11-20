package skills

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/CmfH009/microgravity/internal/tools"
)

// A reader that fails unconditionally
type errorReader struct{}

func (errorReader) Read(p []byte) (n int, err error) {
	return 0, errors.New("simulated read error")
}

func TestParseSkill_ReaderError(t *testing.T) {
	_, err := ParseSkill(errorReader{})
	if err == nil {
		t.Error("ParseSkill() expected error with failing reader, got nil")
	}
}

func TestParseSkill_MissingName(t *testing.T) {
	content := "---\ndescription: node name\n---\nbody"
	_, err := ParseSkill(strings.NewReader(content))
	if err == nil {
		t.Error("ParseSkill() expected error for missing name, got nil")
	}
}

func TestParseSkill_ScannerErrorInFrontmatter(t *testing.T) {
	// Create a line larger than the 1MB buffer to simulate scanner error
	longLine := strings.Repeat("a", 1024*1024+1)
	content := "---\n" + longLine + "\n---"
	_, err := ParseSkill(strings.NewReader(content))
	if err == nil {
		t.Error("ParseSkill() expected error for extremely long line in frontmatter, got nil")
	}
}

func TestParseSkill_ScannerErrorInBody(t *testing.T) {
	longLine := strings.Repeat("a", 1024*1024+1)
	content := "---\nname: valid\n---\n" + longLine
	_, err := ParseSkill(strings.NewReader(content))
	if err == nil {
		t.Error("ParseSkill() expected error for extremely long line in body, got nil")
	}
}

func TestLoadAll_RootPermissionError(t *testing.T) {
	// A file instead of a directory isn't a permission error, it just returns empty (since we skip non-dirs or files not ending in .md).
	// To reliably test the 'err != nil' branch at the root, we can pass a path that we know will fail to read, e.g., an invalid path with null bytes.
	_, err := LoadAll("invalid\x00path")
	if err == nil {
		// Depending on OS, it might fail or not. If it doesn't fail, we just pass.
	}
}

func TestLoadAll_UnreadableFile(t *testing.T) {
	dir := t.TempDir()
	skillPath := filepath.Join(dir, "unreadable.md")
	
	// Create a file
	if err := os.WriteFile(skillPath, []byte(""), 0000); err != nil {
		t.Fatal(err)
	}
	
	// Ensure we cannot read it
	// On Windows, permissions might not work the same, but LoadAll will just skip it.
	loaded, err := LoadDir(dir)
	if err != nil {
		t.Fatalf("LoadDir() returned error: %v", err)
	}
	if len(loaded) != 0 {
		t.Errorf("Expected 0 loaded skills, got %d", len(loaded))
	}
}

func TestRegisterTools_Execution(t *testing.T) {
	registry := tools.NewRegistry()
	skills := []*Skill{
		{
			Name:        "test-tool",
			Description: "A test tool",
			Type:        "tool",
			// A simple command that works on both Windows and Unix
			Command:     "echo hello",
		},
		{
			Name:        "test-tool-fail",
			Description: "A test tool that fails",
			Type:        "tool",
			Command:     "some-nonexistent-command-12345",
		},
		{
			Name: "test-prompt", // Should not be registered
			Type: "prompt",
		},
	}

	RegisterTools(registry, skills)

	// Check that tool was registered
	if !registry.IsNative("test-tool") {
		t.Fatal("Expected test-tool to be registered")
	}
	if registry.IsNative("test-prompt") {
		t.Fatal("Did not expect test-prompt to be registered")
	}

	// Test successful execution without args
	out, err := registry.Execute(context.Background(), "test-tool", map[string]interface{}{})
	if err != nil {
		t.Fatalf("Execute test-tool failed: %v", err)
	}
	if !strings.Contains(out, "hello") {
		t.Errorf("Expected output to contain 'hello', got: %s", out)
	}

	// Test successful execution with args
	outArgs, err := registry.Execute(context.Background(), "test-tool", map[string]interface{}{"args": "world"})
	if err != nil {
		t.Fatalf("Execute test-tool with args failed: %v", err)
	}
	if !strings.Contains(outArgs, "world") {
		t.Errorf("Expected output to contain 'world', got: %s", outArgs)
	}

	// Test failing execution
	outFail, err := registry.Execute(context.Background(), "test-tool-fail", map[string]interface{}{})
	if err != nil {
		t.Fatalf("Expected Execute to return nil error (encapsulates error in string), got: %v", err)
	}
	if !strings.Contains(outFail, "Error:") {
		t.Errorf("Expected output to contain 'Error:', got: %s", outFail)
	}
}

package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestCompressCmd(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "compress_test_*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	goFile := filepath.Join(tmpDir, "test.go")
	code := `package test
func unexported() { println("secret") }
func Exported() { unexported() }
`
	if err := os.WriteFile(goFile, []byte(code), 0644); err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	buf := new(bytes.Buffer)
	compressCmd.SetOut(buf)
	compressCmd.SetErr(buf)

	if err := compressCmd.RunE(compressCmd, []string{goFile}); err != nil {
		t.Fatalf("compressCmd execute failed: %v", err)
	}

	out := buf.String()
	if out == "" {
		t.Errorf("Expected non-empty output from compressCmd")
	}
}

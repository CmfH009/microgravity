package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestSetupCommand(t *testing.T) {
	// Create a temporary home directory
	tempHome := t.TempDir()

	// Set HOME environment variable to our temp dir
	// Store original HOME to restore later
	originalHome := os.Getenv("HOME")
	originalUserProfile := os.Getenv("USERPROFILE")
	originalTestMode := os.Getenv("TEST_MODE")
	os.Setenv("HOME", tempHome)
	os.Setenv("USERPROFILE", tempHome)
	os.Setenv("TEST_MODE", "1")
	defer func() {
		os.Setenv("HOME", originalHome)
		os.Setenv("USERPROFILE", originalUserProfile)
		os.Setenv("TEST_MODE", originalTestMode)
	}()

	// Change working directory to temp dir so .env is created there
	originalWD, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	err = os.Chdir(tempHome)
	if err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(originalWD)

	// We'll capture standard output
	var stdout bytes.Buffer
	setupCmd.SetOut(&stdout)

	// Run the setup command
	err = setupCmd.RunE(setupCmd, []string{})
	if err != nil {
		t.Fatalf("setup command failed: %v", err)
	}

	// Verify .microgravity directory was created
	configDir := filepath.Join(tempHome, ".microgravity")
	if _, err := os.Stat(configDir); os.IsNotExist(err) {
		t.Errorf("expected config directory %s to be created, but it was not", configDir)
	}

	// Verify SQLite DB files were created
	memoryDB := filepath.Join(configDir, "memory.db")
	if _, err := os.Stat(memoryDB); os.IsNotExist(err) {
		t.Errorf("expected memory DB %s to be created", memoryDB)
	}

	ragDB := filepath.Join(configDir, "rag.db")
	if _, err := os.Stat(ragDB); os.IsNotExist(err) {
		t.Errorf("expected RAG DB %s to be created", ragDB)
	}
}

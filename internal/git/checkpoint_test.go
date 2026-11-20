package git

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func setupTestGitRepo(t *testing.T) string {
	dir := t.TempDir()

	// Init git repo
	cmd := exec.Command("git", "init")
	cmd.Dir = dir
	if err := cmd.Run(); err != nil {
		t.Fatalf("git init failed: %v", err)
	}

	// Configure user for commits
	_ = exec.Command("git", "-C", dir, "config", "user.name", "Test User").Run()
	_ = exec.Command("git", "-C", dir, "config", "user.email", "test@example.com").Run()

	// Initial commit
	initFile := filepath.Join(dir, "README.md")
	_ = os.WriteFile(initFile, []byte("# Initial"), 0644)
	_ = exec.Command("git", "-C", dir, "add", ".").Run()
	_ = exec.Command("git", "-C", dir, "commit", "-m", "Initial commit").Run()

	return dir
}

func TestCheckpointManager_CreateAndRewind(t *testing.T) {
	repoDir := setupTestGitRepo(t)
	cm := NewCheckpointManager(repoDir, "test-session")

	// 1. Create initial checkpoint
	cp1, err := cm.CreateCheckpoint(1, "Pre-turn 1")
	if err != nil {
		t.Fatalf("CreateCheckpoint failed: %v", err)
	}
	if cp1.CommitSHA == "" {
		t.Fatalf("expected non-empty commit SHA")
	}

	// 2. Modify a file and create a new file
	testFile := filepath.Join(repoDir, "new_file.txt")
	if err := os.WriteFile(testFile, []byte("uncommitted change"), 0644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	readmeFile := filepath.Join(repoDir, "README.md")
	if err := os.WriteFile(readmeFile, []byte("# Modified"), 0644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	// Verify file exists
	if _, err := os.Stat(testFile); os.IsNotExist(err) {
		t.Fatalf("expected new_file.txt to exist")
	}

	// 3. Rewind to checkpoint
	rewound, err := cm.Rewind()
	if err != nil {
		t.Fatalf("Rewind failed: %v", err)
	}
	if rewound.ID != "cp-1" {
		t.Errorf("expected rewound ID cp-1, got %s", rewound.ID)
	}

	// 4. Verify working tree was restored cleanly
	if _, err := os.Stat(testFile); !os.IsNotExist(err) {
		t.Errorf("expected new_file.txt to be removed after rewind")
	}

	content, err := os.ReadFile(readmeFile)
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}
	if string(content) != "# Initial" {
		t.Errorf("expected '# Initial', got '%s'", string(content))
	}
}

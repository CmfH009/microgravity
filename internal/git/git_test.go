package git

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestGitDiff(t *testing.T) {
	tempDir := t.TempDir()

	// Initialize git repo
	cmdInit := exec.Command("git", "init")
	cmdInit.Dir = tempDir
	if err := cmdInit.Run(); err != nil {
		t.Fatalf("Failed to git init: %v", err)
	}
	if err := exec.Command("git", "-C", tempDir, "config", "user.email", "test@test.com").Run(); err != nil {
		t.Fatal(err)
	}
	if err := exec.Command("git", "-C", tempDir, "config", "user.name", "Test").Run(); err != nil {
		t.Fatal(err)
	}

	// Create initial commit
	dummyFile := filepath.Join(tempDir, "README.md")
	os.WriteFile(dummyFile, []byte("# Test Repo\n"), 0644)
	exec.Command("git", "-C", tempDir, "add", ".").Run()
	exec.Command("git", "-C", tempDir, "commit", "-m", "initial commit").Run()

	// Modify a file - unstaged diff should show changes
	os.WriteFile(dummyFile, []byte("# Test Repo\nmodified\n"), 0644)
	diff, err := GitDiff(tempDir, false)
	if err != nil {
		t.Fatalf("GitDiff failed: %v", err)
	}
	if !strings.Contains(diff, "modified") {
		t.Fatalf("Expected diff to contain 'modified', got: %s", diff)
	}

	// Stage the file - unstaged diff should now be empty
	exec.Command("git", "-C", tempDir, "add", ".").Run()
	diff, err = GitDiff(tempDir, false)
	if err != nil {
		t.Fatalf("GitDiff failed: %v", err)
	}
	if strings.TrimSpace(diff) != "" {
		t.Fatalf("Expected empty unstaged diff after staging, got: %s", diff)
	}

	// Staged diff should show changes
	diff, err = GitDiff(tempDir, true)
	if err != nil {
		t.Fatalf("GitDiff(staged) failed: %v", err)
	}
	if !strings.Contains(diff, "modified") {
		t.Fatalf("Expected staged diff to contain 'modified', got: %s", diff)
	}

	// Diffstat should work
	stat, err := GitDiffStat(tempDir, true)
	if err != nil {
		t.Fatalf("GitDiffStat failed: %v", err)
	}
	if !strings.Contains(stat, "README.md") {
		t.Fatalf("Expected diffstat to contain 'README.md', got: %s", stat)
	}

	// File-specific diff
	fileDiff, err := GitDiffFile(tempDir, "README.md", true)
	if err != nil {
		t.Fatalf("GitDiffFile failed: %v", err)
	}
	if !strings.Contains(fileDiff, "modified") {
		t.Fatalf("Expected file diff to contain 'modified', got: %s", fileDiff)
	}

	// File-specific diff for nonexistent file should be empty
	emptyDiff, err := GitDiffFile(tempDir, "nonexistent.txt", true)
	if err != nil {
		t.Fatalf("GitDiffFile for nonexistent file failed: %v", err)
	}
	if strings.TrimSpace(emptyDiff) != "" {
		t.Fatalf("Expected empty diff for nonexistent file, got: %s", emptyDiff)
	}
}

func TestWorktreeLifecycle(t *testing.T) {
	tempDir := t.TempDir()

	// Initialize git repo in tempDir
	cmdInit := exec.Command("git", "init")
	cmdInit.Dir = tempDir
	if err := cmdInit.Run(); err != nil {
		t.Fatalf("Failed to git init: %v", err)
	}

	// Create initial commit
	dummyFile := filepath.Join(tempDir, "README.md")
	os.WriteFile(dummyFile, []byte("# Test Repo"), 0644)
	exec.Command("git", "-C", tempDir, "add", ".").Run()
	exec.Command("git", "-C", tempDir, "commit", "-m", "initial commit").Run()

	featureName := "test-feature"

	// 1. Create Worktree
	wtPath, err := CreateWorktree(tempDir, featureName)
	if err != nil {
		t.Fatalf("CreateWorktree failed: %v", err)
	}

	if !strings.Contains(wtPath, featureName) {
		t.Errorf("Expected worktree path to contain %s, got: %s", featureName, wtPath)
	}

	// 2. List Worktrees
	list, err := ListWorktrees(tempDir)
	if err != nil {
		t.Fatalf("ListWorktrees failed: %v", err)
	}

	found := false
	for _, wt := range list {
		if strings.Contains(wt.Name, featureName) || strings.Contains(wt.Path, featureName) {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Expected worktree %s in ListWorktrees output, got %v", featureName, list)
	}

	// 3. Remove Worktree
	err = RemoveWorktree(tempDir, featureName)
	if err != nil {
		t.Fatalf("RemoveWorktree failed: %v", err)
	}
}

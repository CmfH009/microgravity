package git

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	
	"github.com/purav/microgravity/internal/tools"
)

func setupTestRepo(t *testing.T) string {
	dir := t.TempDir()

	cmd := exec.Command("git", "init")
	cmd.Dir = dir
	if err := cmd.Run(); err != nil {
		t.Fatalf("failed to init git: %v", err)
	}
	
	cmd = exec.Command("git", "config", "user.email", "test@example.com")
	cmd.Dir = dir
	cmd.Run()
	cmd = exec.Command("git", "config", "user.name", "Test User")
	cmd.Dir = dir
	cmd.Run()

	return dir
}

func TestGitTools(t *testing.T) {
	registry := tools.NewRegistry()
	err := RegisterGitTools(registry)
	if err != nil {
		t.Fatalf("Failed to register git tools: %v", err)
	}

	if !registry.IsNative("git_diff") {
		t.Errorf("git_diff tool not registered")
	}
	if !registry.IsNative("git_commit") {
		t.Errorf("git_commit tool not registered")
	}
	if !registry.IsNative("github_create_pr") {
		t.Errorf("github_create_pr tool not registered")
	}
}

func TestHandleGitDiff(t *testing.T) {
	ctx := context.Background()

	t.Run("ValidDiff", func(t *testing.T) {
		dir := setupTestRepo(t)
		testFile := filepath.Join(dir, "test.txt")
		os.WriteFile(testFile, []byte("hello"), 0644)

		// Unstaged diff
		args := map[string]interface{}{"dir": dir, "staged": false}
		diff, err := handleGitDiff(ctx, args)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if diff != "" {
			t.Errorf("expected empty diff since file is untracked, got %q", diff)
		}

		exec.Command("git", "-C", dir, "add", "test.txt").Run()

		// Staged diff
		args["staged"] = true
		diff, err = handleGitDiff(ctx, args)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if !strings.Contains(diff, "+hello") {
			t.Errorf("expected diff to contain +hello, got %q", diff)
		}
	})

	t.Run("MissingDir", func(t *testing.T) {
		args := map[string]interface{}{}
		_, err := handleGitDiff(ctx, args)
		if err == nil || !strings.Contains(err.Error(), "missing or invalid 'dir'") {
			t.Errorf("expected missing dir error, got %v", err)
		}
	})

	t.Run("EmptyDir", func(t *testing.T) {
		args := map[string]interface{}{"dir": ""}
		_, err := handleGitDiff(ctx, args)
		if err == nil || !strings.Contains(err.Error(), "missing or invalid 'dir'") {
			t.Errorf("expected missing dir error, got %v", err)
		}
	})

	t.Run("NonExistentDir", func(t *testing.T) {
		args := map[string]interface{}{"dir": "/does/not/exist/123"}
		_, err := handleGitDiff(ctx, args)
		if err == nil || !strings.Contains(err.Error(), "directory does not exist") {
			t.Errorf("expected directory does not exist error, got %v", err)
		}
	})

	t.Run("NotGitRepo", func(t *testing.T) {
		dir := t.TempDir()
		args := map[string]interface{}{"dir": dir}
		_, err := handleGitDiff(ctx, args)
		if err == nil || !strings.Contains(err.Error(), "not a git repository") {
			t.Errorf("expected not a git repository error, got %v", err)
		}
	})
}

func TestHandleGitCommit(t *testing.T) {
	ctx := context.Background()

	t.Run("ValidCommit", func(t *testing.T) {
		dir := setupTestRepo(t)
		testFile := filepath.Join(dir, "test.txt")
		os.WriteFile(testFile, []byte("hello"), 0644)
		exec.Command("git", "-C", dir, "add", "test.txt").Run()

		args := map[string]interface{}{
			"dir":     dir,
			"message": "Initial commit",
		}
		res, err := handleGitCommitTool(ctx, args)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if res != "Committed successfully" {
			t.Errorf("unexpected success message: %q", res)
		}

		out, _ := exec.Command("git", "-C", dir, "log", "-1", "--pretty=format:%s").Output()
		if string(out) != "Initial commit" {
			t.Errorf("commit message mismatch, got %q", string(out))
		}
	})

	t.Run("MissingDir", func(t *testing.T) {
		args := map[string]interface{}{"message": "test"}
		_, err := handleGitCommitTool(ctx, args)
		if err == nil || !strings.Contains(err.Error(), "missing or invalid 'dir'") {
			t.Errorf("expected missing dir error, got %v", err)
		}
	})

	t.Run("EmptyMessage", func(t *testing.T) {
		dir := setupTestRepo(t)
		args := map[string]interface{}{"dir": dir, "message": ""}
		_, err := handleGitCommitTool(ctx, args)
		if err == nil || !strings.Contains(err.Error(), "missing or invalid 'message'") {
			t.Errorf("expected missing message error, got %v", err)
		}
	})

	t.Run("NotGitRepo", func(t *testing.T) {
		dir := t.TempDir()
		args := map[string]interface{}{"dir": dir, "message": "test"}
		_, err := handleGitCommitTool(ctx, args)
		if err == nil || !strings.Contains(err.Error(), "not a git repository") {
			t.Errorf("expected not a git repo error, got %v", err)
		}
	})
    
    t.Run("DetachedHead", func(t *testing.T) {
        dir := setupTestRepo(t)
        testFile := filepath.Join(dir, "test.txt")
        os.WriteFile(testFile, []byte("first"), 0644)
        exec.Command("git", "-C", dir, "add", ".").Run()
        exec.Command("git", "-C", dir, "commit", "-m", "first").Run()
        
        out, _ := exec.Command("git", "-C", dir, "rev-parse", "HEAD").Output()
        hash := strings.TrimSpace(string(out))
        
        exec.Command("git", "-C", dir, "checkout", hash).Run()
        
        os.WriteFile(testFile, []byte("second"), 0644)
        exec.Command("git", "-C", dir, "add", ".").Run()
        
        args := map[string]interface{}{
			"dir":     dir,
			"message": "Detached commit",
		}
		res, err := handleGitCommitTool(ctx, args)
        if err != nil {
			t.Fatalf("expected commit on detached head to succeed, got %v", err)
		}
        if res != "Committed successfully" {
			t.Errorf("unexpected success message: %q", res)
		}
    })
}

func TestHandleGithubCreatePR_Validation(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name string
		args map[string]interface{}
		want string
	}{
		{"EmptyArgs", map[string]interface{}{}, "missing 'owner'"},
		{"MissingRepo", map[string]interface{}{"owner": "o"}, "missing 'repo'"},
		{"EmptyRepo", map[string]interface{}{"owner": "o", "repo": ""}, "missing 'repo'"},
		{"MissingTitle", map[string]interface{}{"owner": "o", "repo": "r"}, "missing 'title'"},
		{"EmptyTitle", map[string]interface{}{"owner": "o", "repo": "r", "title": ""}, "missing 'title'"},
		{"MissingHead", map[string]interface{}{"owner": "o", "repo": "r", "title": "t"}, "missing 'head'"},
		{"EmptyHead", map[string]interface{}{"owner": "o", "repo": "r", "title": "t", "head": ""}, "missing 'head'"},
		{"MissingBase", map[string]interface{}{"owner": "o", "repo": "r", "title": "t", "head": "h"}, "missing 'base'"},
		{"EmptyBase", map[string]interface{}{"owner": "o", "repo": "r", "title": "t", "head": "h", "base": ""}, "missing 'base'"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := handleGithubCreatePR(ctx, tt.args)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("expected error containing %q, got %v", tt.want, err)
			}
		})
	}
}

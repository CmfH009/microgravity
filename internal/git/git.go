package git

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type WorktreeInfo struct {
	Name   string `json:"name"`
	Branch string `json:"branch"`
	Path   string `json:"path"`
}

// GitDiff returns the unstaged or staged changes in the given repository directory.
func GitDiff(repoDir string, staged bool) (string, error) {
	args := []string{"diff"}
	if staged {
		args = append(args, "--staged")
	}

	return runGitDiff(repoDir, args)
}

// GitDiffStat returns a diffstat summary (like `git diff --stat`) of the working
// tree or staged changes.
func GitDiffStat(repoDir string, staged bool) (string, error) {
	args := []string{"diff", "--stat"}
	if staged {
		args = append(args, "--staged")
	}

	return runGitDiff(repoDir, args)
}

// GitDiffFile returns the diff for a single file path (unstaged or staged).
func GitDiffFile(repoDir, filePath string, staged bool) (string, error) {
	args := []string{"diff"}
	if staged {
		args = append(args, "--staged")
	}
	args = append(args, "--", filePath)

	return runGitDiff(repoDir, args)
}

// runGitDiff executes `git diff <args...>` in repoDir and captures stdout.
func runGitDiff(repoDir string, args []string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = repoDir

	var out bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err != nil {
		return "", fmt.Errorf("git diff failed: %v, stderr: %s", err, stderr.String())
	}

	return out.String(), nil
}

// GitCommit commits the staged changes with the provided message.
func GitCommit(repoDir string, message string) error {
	cmd := exec.Command("git", "commit", "-m", message)
	cmd.Dir = repoDir

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err != nil {
		return fmt.Errorf("git commit failed: %v, stderr: %s", err, stderr.String())
	}

	return nil
}

// CreateWorktree creates an isolated git worktree in .worktrees/<name> with branch feature/<name>.
func CreateWorktree(repoDir string, name string) (string, error) {
	targetDir := filepath.Join(repoDir, ".worktrees", name)
	branchName := "feature/" + name

	os.MkdirAll(filepath.Dir(targetDir), 0755)

	cmd := exec.Command("git", "worktree", "add", "-b", branchName, targetDir)
	cmd.Dir = repoDir

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err != nil {
		return "", fmt.Errorf("git worktree add failed: %v, stderr: %s", err, stderr.String())
	}

	return targetDir, nil
}

// ListWorktrees lists all active git worktrees in the repository.
func ListWorktrees(repoDir string) ([]WorktreeInfo, error) {
	cmd := exec.Command("git", "worktree", "list", "--porcelain")
	cmd.Dir = repoDir

	var out bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err != nil {
		return nil, fmt.Errorf("git worktree list failed: %v, stderr: %s", err, stderr.String())
	}

	var worktrees []WorktreeInfo
	lines := strings.Split(out.String(), "\n")
	var current WorktreeInfo

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "worktree ") {
			if current.Path != "" {
				worktrees = append(worktrees, current)
				current = WorktreeInfo{}
			}
			current.Path = strings.TrimPrefix(line, "worktree ")
			current.Name = filepath.Base(current.Path)
		} else if strings.HasPrefix(line, "branch ") {
			current.Branch = strings.TrimPrefix(line, "branch ")
		}
	}
	if current.Path != "" {
		worktrees = append(worktrees, current)
	}

	return worktrees, nil
}

// RemoveWorktree removes a git worktree safely.
func RemoveWorktree(repoDir string, name string) error {
	targetDir := filepath.Join(repoDir, ".worktrees", name)
	cmd := exec.Command("git", "worktree", "remove", "--force", targetDir)
	cmd.Dir = repoDir

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err != nil {
		return fmt.Errorf("git worktree remove failed: %v, stderr: %s", err, stderr.String())
	}

	return nil
}

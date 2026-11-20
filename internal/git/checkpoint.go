package git

import (
	"bytes"
	"fmt"
	"os/exec"
	"time"
)

// Checkpoint represents a saved filesystem state in git.
type Checkpoint struct {
	ID        string    `json:"id"`
	CommitSHA string    `json:"commit_sha"`
	Timestamp time.Time `json:"timestamp"`
	Message   string    `json:"message"`
	Turn      int       `json:"turn"`
}

// CheckpointManager tracks and restores shadow git checkpoints.
type CheckpointManager struct {
	repoDir     string
	sessionID   string
	checkpoints []Checkpoint
}

// NewCheckpointManager creates a new checkpoint manager for the repository.
func NewCheckpointManager(repoDir, sessionID string) *CheckpointManager {
	if sessionID == "" {
		sessionID = fmt.Sprintf("session-%d", time.Now().Unix())
	}
	return &CheckpointManager{
		repoDir:     repoDir,
		sessionID:   sessionID,
		checkpoints: make([]Checkpoint, 0),
	}
}

// IsGitRepo checks if repoDir is inside a git work tree.
func IsGitRepo(repoDir string) bool {
	cmd := exec.Command("git", "rev-parse", "--is-inside-work-tree")
	cmd.Dir = repoDir
	err := cmd.Run()
	return err == nil
}

// CreateCheckpoint captures a shadow snapshot of the current working directory state.
func (cm *CheckpointManager) CreateCheckpoint(turn int, message string) (*Checkpoint, error) {
	if !IsGitRepo(cm.repoDir) {
		return nil, fmt.Errorf("not a git repository: %s", cm.repoDir)
	}

	var commitSHA string

	// Try git stash create to capture dirty working tree + index without altering working tree
	stashCmd := exec.Command("git", "stash", "create", fmt.Sprintf("mg-checkpoint-%s-%d", cm.sessionID, turn))
	stashCmd.Dir = cm.repoDir
	out, err := stashCmd.Output()
	if err == nil && len(bytes.TrimSpace(out)) > 0 {
		commitSHA = string(bytes.TrimSpace(out))
	} else {
		// Clean working tree: use HEAD commit
		headCmd := exec.Command("git", "rev-parse", "HEAD")
		headCmd.Dir = cm.repoDir
		headOut, headErr := headCmd.Output()
		if headErr != nil {
			return nil, fmt.Errorf("failed to get HEAD commit: %v", headErr)
		}
		commitSHA = string(bytes.TrimSpace(headOut))
	}

	cpID := fmt.Sprintf("cp-%d", turn)
	cp := Checkpoint{
		ID:        cpID,
		CommitSHA: commitSHA,
		Timestamp: time.Now(),
		Message:   message,
		Turn:      turn,
	}

	// Update ref in git for persistence
	refName := fmt.Sprintf("refs/microgravity/checkpoints/%s/%d", cm.sessionID, turn)
	updateCmd := exec.Command("git", "update-ref", refName, commitSHA)
	updateCmd.Dir = cm.repoDir
	_ = updateCmd.Run()

	cm.checkpoints = append(cm.checkpoints, cp)
	return &cp, nil
}

// RestoreCheckpoint restores the working directory and index to the specified commit SHA.
func (cm *CheckpointManager) RestoreCheckpoint(commitSHA string) error {
	if !IsGitRepo(cm.repoDir) {
		return fmt.Errorf("not a git repository: %s", cm.repoDir)
	}

	// 1. Checkout files from target commit
	checkoutCmd := exec.Command("git", "checkout", commitSHA, "--", ".")
	checkoutCmd.Dir = cm.repoDir
	if err := checkoutCmd.Run(); err != nil {
		// Fallback to restore command
		restoreCmd := exec.Command("git", "restore", "--source="+commitSHA, "--worktree", "--staged", ".")
		restoreCmd.Dir = cm.repoDir
		if errRestore := restoreCmd.Run(); errRestore != nil {
			return fmt.Errorf("checkout/restore failed: %v", errRestore)
		}
	}

	// 2. Un-stage any index changes
	resetCmd := exec.Command("git", "reset")
	resetCmd.Dir = cm.repoDir
	_ = resetCmd.Run()

	// 3. Clean any untracked files added after checkpoint
	cleanCmd := exec.Command("git", "clean", "-fd")
	cleanCmd.Dir = cm.repoDir
	_ = cleanCmd.Run()

	return nil
}

// Rewind restores the working tree to the most recent checkpoint and removes it from the stack.
func (cm *CheckpointManager) Rewind() (*Checkpoint, error) {
	if len(cm.checkpoints) == 0 {
		return nil, fmt.Errorf("no checkpoints available to rewind")
	}

	lastIdx := len(cm.checkpoints) - 1
	target := cm.checkpoints[lastIdx]

	if err := cm.RestoreCheckpoint(target.CommitSHA); err != nil {
		return nil, err
	}

	cm.checkpoints = cm.checkpoints[:lastIdx]
	return &target, nil
}

// Latest returns the most recent checkpoint without rewinding.
func (cm *CheckpointManager) Latest() (*Checkpoint, bool) {
	if len(cm.checkpoints) == 0 {
		return nil, false
	}
	return &cm.checkpoints[len(cm.checkpoints)-1], true
}

// Count returns the number of active checkpoints.
func (cm *CheckpointManager) Count() int {
	return len(cm.checkpoints)
}

package e2e_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	mgcontext "github.com/CmfH009/microgravity/internal/context"
	"github.com/CmfH009/microgravity/internal/git"
	"github.com/CmfH009/microgravity/internal/policy"
	"github.com/CmfH009/microgravity/internal/pruner"
	"github.com/CmfH009/microgravity/internal/subagent"
)

// ══════════════════════════════════════════════════════════════════
// TIER 2: BOUNDARY & CORNER CASES
// ══════════════════════════════════════════════════════════════════

func TestTier2_Boundary_EmptyFile(t *testing.T) {
	tempDir := t.TempDir()
	emptyFile := filepath.Join(tempDir, "empty.go")
	if err := os.WriteFile(emptyFile, []byte(""), 0644); err != nil {
		t.Fatalf("failed to create empty file: %v", err)
	}

	pol := policy.New([]string{tempDir}, true, false)
	registry, _ := createTestRegistry(t, pol)

	ctx := context.Background()
	_, err := registry.Execute(ctx, "replace_file_content", map[string]interface{}{
		"TargetFile":         emptyFile,
		"TargetContent":      "func NonExistent() {}",
		"ReplacementContent": "func NewFunc() {}",
	})

	if err == nil {
		t.Fatalf("expected error when replacing target in empty file, got nil")
	}
	if !strings.Contains(err.Error(), "TargetContent not found") {
		t.Fatalf("expected 'TargetContent not found' error, got: %v", err)
	}
}

func TestTier2_Boundary_LargeFileOver500KB(t *testing.T) {
	tempDir := t.TempDir()
	largeFile := filepath.Join(tempDir, "large_data.bin")

	// Create 550 KB file (limit is 500KB)
	largeData := make([]byte, 550*1024)
	for i := range largeData {
		largeData[i] = 'X'
	}
	if err := os.WriteFile(largeFile, largeData, 0644); err != nil {
		t.Fatalf("failed to create large file: %v", err)
	}

	prompt := fmt.Sprintf("Read large file @%s", largeFile)
	cleanPrompt, injectedContext, err := mgcontext.ProcessFileReferences(prompt)
	if err != nil {
		t.Fatalf("ProcessFileReferences failed: %v", err)
	}

	if !strings.Contains(cleanPrompt, fmt.Sprintf("[%s]", largeFile)) {
		t.Fatalf("clean prompt did not reference file properly: %s", cleanPrompt)
	}

	expectedSkipMsg := fmt.Sprintf("[File @%s skipped: exceeds 500KB limit]", largeFile)
	if !strings.Contains(injectedContext, expectedSkipMsg) {
		t.Fatalf("expected skip message for >500KB file, got: %s", injectedContext)
	}
}

func TestTier2_Boundary_MixedCRLFAndLF(t *testing.T) {
	tempDir := t.TempDir()
	mixedFile := filepath.Join(tempDir, "mixed_endings.txt")

	// Mixed file: Header has CRLF, middle has LF, footer has CRLF
	mixedContent := "Line 1 CRLF\r\nLine 2 LF\nLine 3 CRLF\r\n"
	if err := os.WriteFile(mixedFile, []byte(mixedContent), 0644); err != nil {
		t.Fatalf("failed to write mixed file: %v", err)
	}

	pol := policy.New([]string{tempDir}, true, false)
	registry, _ := createTestRegistry(t, pol)

	ctx := context.Background()
	_, err := registry.Execute(ctx, "replace_file_content", map[string]interface{}{
		"TargetFile":         mixedFile,
		"TargetContent":      "Line 2 LF",
		"ReplacementContent": "Line 2 Replaced",
	})
	if err != nil {
		t.Fatalf("replace_file_content on mixed file failed: %v", err)
	}

	// Verify replacement succeeded and file still has CRLF (dominant)
	assertCRLF(t, mixedFile)
	data, _ := os.ReadFile(mixedFile)
	if !strings.Contains(string(data), "Line 2 Replaced") {
		t.Fatalf("replacement text missing: %s", string(data))
	}
}

func TestTier2_Boundary_NestedDirReferences(t *testing.T) {
	tempDir := t.TempDir()
	topDir := filepath.Join(tempDir, "tree_root")
	level1 := filepath.Join(topDir, "level1")
	level2 := filepath.Join(level1, "level2")
	if err := os.MkdirAll(level2, 0755); err != nil {
		t.Fatalf("failed to create nested dirs: %v", err)
	}

	_ = os.WriteFile(filepath.Join(topDir, "root.txt"), []byte("root file"), 0644)
	_ = os.WriteFile(filepath.Join(level1, "mid.txt"), []byte("mid file"), 0644)
	_ = os.WriteFile(filepath.Join(level2, "leaf.txt"), []byte("leaf file"), 0644)

	prompt := fmt.Sprintf("Examine directory tree @%s", topDir)
	cleanPrompt, injectedContext, err := mgcontext.ProcessFileReferences(prompt)
	if err != nil {
		t.Fatalf("ProcessFileReferences failed: %v", err)
	}

	if !strings.Contains(cleanPrompt, fmt.Sprintf("[%s]", topDir)) {
		t.Fatalf("clean prompt did not replace directory token: %s", cleanPrompt)
	}

	if !strings.Contains(injectedContext, "- [DIR]  level1") {
		t.Fatalf("missing directory entry in injected tree: %s", injectedContext)
	}
	if !strings.Contains(injectedContext, "- [FILE] root.txt") {
		t.Fatalf("missing file entry in injected tree: %s", injectedContext)
	}
}

func TestTier2_Boundary_SpecialCharactersInPaths(t *testing.T) {
	tempDir := t.TempDir()
	specialSub := filepath.Join(tempDir, "special-dir_v2.0")
	if err := os.MkdirAll(specialSub, 0755); err != nil {
		t.Fatalf("failed to create special sub dir: %v", err)
	}

	specialFile := filepath.Join(specialSub, "data_item-v1.final.txt")
	content := "special character payload #123!"
	if err := os.WriteFile(specialFile, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write special file: %v", err)
	}

	pol := policy.New([]string{tempDir}, true, false)
	resolved, err := pol.ResolvePath(specialFile)
	if err != nil {
		t.Fatalf("failed to resolve path with dashes, underscores, and dots: %v", err)
	}
	if resolved != filepath.Clean(specialFile) {
		t.Fatalf("mismatch resolved special path: %s", resolved)
	}

	// Tool execution on special path
	registry, _ := createTestRegistry(t, pol)
	ctx := context.Background()
	_, err = registry.Execute(ctx, "replace_file_content", map[string]interface{}{
		"TargetFile":         specialFile,
		"TargetContent":      "payload #123!",
		"ReplacementContent": "payload updated successfully",
	})
	if err != nil {
		t.Fatalf("replace_file_content failed on special path: %v", err)
	}

	data, _ := os.ReadFile(specialFile)
	if !strings.Contains(string(data), "payload updated successfully") {
		t.Fatalf("expected updated content on special path: %s", string(data))
	}
}

func TestTier2_Boundary_DeepWorktrees(t *testing.T) {
	repoDir := initGitRepo(t)
	mgr := subagent.NewManager(nil, t.TempDir())

	deepName := "subagent-tier2-boundary-deep-worktree-alpha-999"
	var createdWorkDir string

	mgr.SetWorktreeProvider(
		func(cwd, name string) (string, error) {
			dir, err := git.CreateWorktree(repoDir, deepName)
			createdWorkDir = dir
			return dir, err
		},
		func(cwd, name string) error {
			return git.RemoveWorktree(repoDir, deepName)
		},
	)

	ctx := context.Background()
	info, err := mgr.Spawn(ctx, "deep-wt", "Deep Worker", subagent.ProfileGeneral, "Deep execution", "", subagent.WorkspaceBranch,
		func(ctx context.Context, sub *subagent.SubagentInfo) (string, error) {
			deepFile := filepath.Join(createdWorkDir, "deep_artifact.txt")
			return "Deep Completed", os.WriteFile(deepFile, []byte("deep payload"), 0644)
		},
	)
	if err != nil {
		t.Fatalf("Spawn deep worktree failed: %v", err)
	}

	snap := waitForSubagent(t, mgr, info.ID, 10*time.Second)
	if snap.Status != "COMPLETED" {
		t.Fatalf("expected deep worktree subagent to complete, got %s", snap.Status)
	}
}

func TestTier2_Boundary_ZeroTokensBudgetPruner(t *testing.T) {
	convo := "User: Task\nAgent: Done."

	// Test boundary with very small budget (1 token, 10 tokens)
	for _, smallBudget := range []int{1, 5, 10, 20} {
		pruned, removed := pruner.PruneContext(convo, smallBudget)
		if removed < 0 {
			t.Fatalf("removed tokens should never be negative for budget %d", smallBudget)
		}
		if len(pruned) == 0 {
			t.Fatalf("pruned text should not be completely empty for budget %d", smallBudget)
		}
	}
}

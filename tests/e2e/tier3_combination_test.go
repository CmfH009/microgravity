package e2e_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	mgcontext "github.com/purav/microgravity/internal/context"
	"github.com/purav/microgravity/internal/git"
	"github.com/purav/microgravity/internal/policy"
	"github.com/purav/microgravity/internal/pruner"
	"github.com/purav/microgravity/internal/subagent"
)

// ══════════════════════════════════════════════════════════════════
// TIER 3: CROSS-FEATURE COMBINATIONS
// ══════════════════════════════════════════════════════════════════

func TestTier3_Combo_SubagentCRLFInWorktree(t *testing.T) {
	repoDir := initGitRepo(t)

	// Create a source code file with CRLF endings in main repo
	sourceFileName := "calculator.go"
	parentSourcePath := filepath.Join(repoDir, sourceFileName)
	parentInitialContent := "package math\r\n\r\nfunc Add(a, b int) int {\r\n\treturn a - b // BUG: subtraction instead of addition\r\n}\r\n"
	if err := os.WriteFile(parentSourcePath, []byte(parentInitialContent), 0644); err != nil {
		t.Fatalf("failed to write parent source file: %v", err)
	}
	assertCRLF(t, parentSourcePath)

	// Commit file to git so worktrees see it
	runGit(t, repoDir, "add", sourceFileName)
	runGit(t, repoDir, "commit", "-m", "feat: add calculator with bug")

	mgr := subagent.NewManager(nil, t.TempDir())
	var worktreeDir string

	mgr.SetWorktreeProvider(
		func(cwd, name string) (string, error) {
			dir, err := git.CreateWorktree(repoDir, name)
			worktreeDir = dir
			return dir, err
		},
		func(cwd, name string) error {
			return git.RemoveWorktree(repoDir, name)
		},
	)

	ctx := context.Background()
	info, err := mgr.Spawn(ctx, "sub-fix-calc", "QA Bugfixer", subagent.ProfileQAAuditor, "Fix addition bug in calculator.go", "", subagent.WorkspaceBranch,
		func(subCtx context.Context, sub *subagent.SubagentInfo) (string, error) {
			// Subagent executes replace_file_content on calculator.go INSIDE worktreeDir
			targetFileInWt := filepath.Join(worktreeDir, sourceFileName)

			// Execute tool via policy scoped to worktreeDir
			pol := policy.New([]string{worktreeDir}, true, false)
			registry, _ := createTestRegistry(t, pol)

			_, execErr := registry.Execute(subCtx, "replace_file_content", map[string]interface{}{
				"TargetFile":         targetFileInWt,
				"TargetContent":      "return a - b // BUG: subtraction instead of addition",
				"ReplacementContent": "return a + b // FIXED: proper addition",
			})
			if execErr != nil {
				return "", fmt.Errorf("tool replace failed: %w", execErr)
			}

			// Verify CRLF is preserved inside the worktree
			assertCRLF(t, targetFileInWt)
			data, readErr := os.ReadFile(targetFileInWt)
			if readErr != nil || !strings.Contains(string(data), "return a + b // FIXED: proper addition") {
				return "", fmt.Errorf("file in worktree was not properly updated")
			}

			return "Successfully fixed addition bug in worktree", nil
		},
	)
	if err != nil {
		t.Fatalf("Spawn subagent failed: %v", err)
	}

	snap := waitForSubagent(t, mgr, info.ID, 10*time.Second)
	if snap.Status != "COMPLETED" {
		t.Fatalf("subagent did not complete cleanly: %s, result: %s", snap.Status, snap.Result)
	}

	// Invariant: Parent repo file MUST remain unmutated (isolation guarantee)
	parentData, _ := os.ReadFile(parentSourcePath)
	if !strings.Contains(string(parentData), "return a - b // BUG") {
		t.Fatalf("parent repository file was mutated prematurely by subagent!")
	}
	assertCRLF(t, parentSourcePath)
}

func TestTier3_Combo_FileRefPlusPolicyEngine(t *testing.T) {
	primaryWorkspace := t.TempDir()
	approvedExternalDir := t.TempDir()
	forbiddenDir := t.TempDir()

	// Create valid file in approved external dir
	approvedFile := filepath.Join(approvedExternalDir, "external_config.yaml")
	_ = os.WriteFile(approvedFile, []byte("env: production\r\ndebug: false\r\n"), 0644)

	// Create file in forbidden dir
	forbiddenFile := filepath.Join(forbiddenDir, "forbidden.key")
	_ = os.WriteFile(forbiddenFile, []byte("SUPER_SECRET_KEY"), 0644)

	// Configure policy with primaryWorkspace and approvedExternalDir (simulating --add-dir)
	pol := policy.New([]string{primaryWorkspace, approvedExternalDir}, true, false)

	// Verify policy resolution
	if _, err := pol.ResolvePath(approvedFile); err != nil {
		t.Fatalf("approvedFile should be permitted by policy: %v", err)
	}
	if _, err := pol.ResolvePath(forbiddenFile); err == nil {
		t.Fatalf("forbiddenFile should be rejected by policy")
	}

	// Verify @file parsing on approved file
	prompt := fmt.Sprintf("Load configuration @%s", approvedFile)
	cleanPrompt, injectedContext, err := mgcontext.ProcessFileReferences(prompt)
	if err != nil {
		t.Fatalf("ProcessFileReferences on approved external file failed: %v", err)
	}

	if !strings.Contains(cleanPrompt, fmt.Sprintf("[%s]", approvedFile)) {
		t.Fatalf("cleanPrompt missing resolved approved file: %s", cleanPrompt)
	}
	if !strings.Contains(injectedContext, "env: production") {
		t.Fatalf("injectedContext missing approved file content: %s", injectedContext)
	}
}

func TestTier3_Combo_Turn0PreservedWithFileContextUnderPruning(t *testing.T) {
	tempDir := t.TempDir()
	specFile := filepath.Join(tempDir, "SPECIFICATION.md")
	specContent := "# Master Architecture Specification\r\nDesign patterns and invariants must be preserved.\r\n"
	_ = os.WriteFile(specFile, []byte(specContent), 0644)

	// Turn 0 has user goal and injected file context
	cleanPrompt, injectedContext, _ := mgcontext.ProcessFileReferences(fmt.Sprintf("Implement system per @%s", specFile))
	turn0 := fmt.Sprintf("User: %s%s\nAgent: Beginning implementation adhering strictly to specification.", cleanPrompt, injectedContext)

	// Build subsequent turns to overflow context
	var conversationTurns []string
	conversationTurns = append(conversationTurns, turn0)
	for i := 1; i <= 30; i++ {
		conversationTurns = append(conversationTurns, fmt.Sprintf("User: Progress update turn %d\nAgent: Telemetry logged for step %d.", i, i))
	}
	fullHistory := strings.Join(conversationTurns, "\n")

	// Trigger pruning with budget that forces pruning of middle turns while allowing Turn 0 content
	budget := 500
	pruned, removed := pruner.PruneContext(fullHistory, budget)

	if removed <= 0 {
		t.Fatalf("expected context pruning to occur")
	}

	// Invariant: Turn 0 root intent and specification header must be preserved
	if !strings.Contains(pruned, "Master Architecture Specification") {
		t.Fatalf("specification context in Turn 0 was evicted during pruning: %s", pruned)
	}
	if !strings.Contains(pruned, "Implement system per") {
		t.Fatalf("Turn 0 user goal was evicted: %s", pruned)
	}
	if !strings.Contains(pruned, "[Earlier conversation context pruned for token efficiency]") {
		t.Fatalf("pruner marker missing in pruned conversation")
	}
	if !strings.Contains(pruned, "Progress update turn 30") {
		t.Fatalf("most recent turn was evicted: %s", pruned)
	}
}

func TestTier3_Combo_PolicySandboxWithSubagentTools(t *testing.T) {
	repoDir := initGitRepo(t)
	mgr := subagent.NewManager(nil, t.TempDir())

	// Policy with host commands disabled (sandbox mode)
	sandboxPolicy := policy.New([]string{repoDir}, false, true)
	registry, _ := createTestRegistry(t, sandboxPolicy)

	// Attempt to run host command within sandboxed policy
	ctx := context.Background()
	_, err := registry.Execute(ctx, "run_command", map[string]interface{}{
		"CommandLine":       "git status",
		"Cwd":               repoDir,
		"WaitMsBeforeAsync": 1000,
	})

	if err == nil {
		t.Fatalf("expected sandboxed run_command to fail due to policy")
	}
	if !strings.Contains(err.Error(), "host command execution is disabled by policy") {
		t.Fatalf("expected sandbox policy error, got: %v", err)
	}

	// But filesystem tools within approved roots must still function
	safeFile := filepath.Join(repoDir, "sandboxed_write.txt")
	_, err = registry.Execute(ctx, "write_to_file", map[string]interface{}{
		"TargetFile":  safeFile,
		"CodeContent": "allowed fs content",
	})
	if err != nil {
		t.Fatalf("filesystem tool within safe root should be permitted in sandbox: %v", err)
	}

	data, _ := os.ReadFile(safeFile)
	if string(data) != "allowed fs content" {
		t.Fatalf("unexpected content: %s", string(data))
	}
	_ = mgr
}

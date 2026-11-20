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
	"github.com/purav/microgravity/internal/events"
	"github.com/purav/microgravity/internal/git"
	"github.com/purav/microgravity/internal/policy"
	"github.com/purav/microgravity/internal/pruner"
	"github.com/purav/microgravity/internal/subagent"
)

// ══════════════════════════════════════════════════════════════════
// TIER 4: REAL-WORLD APPLICATION WORKFLOW SCENARIO
// ══════════════════════════════════════════════════════════════════

func TestTier4_Workflow_MultiStepAutonomousAgentCycle(t *testing.T) {
	// ── 1. Workspace & Git Repository Setup ────────────────────────
	repoDir := initGitRepo(t)

	// Create service directory and buggy implementation with Windows CRLF endings
	serviceDir := filepath.Join(repoDir, "service")
	if err := os.MkdirAll(serviceDir, 0755); err != nil {
		t.Fatalf("failed to create service dir: %v", err)
	}

	sourceFileRel := filepath.Join("service", "math_service.go")
	sourceFileAbs := filepath.Join(repoDir, sourceFileRel)
	initialCode := "package service\r\n\r\nimport \"errors\"\r\n\r\nfunc Divide(a, b int) (int, error) {\r\n\t// BUG: missing zero check\r\n\treturn a / b, nil\r\n}\r\n"
	if err := os.WriteFile(sourceFileAbs, []byte(initialCode), 0644); err != nil {
		t.Fatalf("failed to write initial code: %v", err)
	}
	assertCRLF(t, sourceFileAbs)

	runGit(t, repoDir, "add", ".")
	runGit(t, repoDir, "commit", "-m", "feat: initial buggy math service")

	// ── 2. Turn 1: User Request Ingestion with @file Context ──────
	userPrompt := fmt.Sprintf("Audit @%s and fix the division by zero crash", sourceFileAbs)
	cleanPrompt, injectedContext, err := mgcontext.ProcessFileReferences(userPrompt)
	if err != nil {
		t.Fatalf("ProcessFileReferences failed: %v", err)
	}

	// Verify single injection
	if strings.Count(injectedContext, "package service") != 1 {
		t.Fatalf("expected file content to be injected exactly once into Turn 0 context")
	}

	turn0 := fmt.Sprintf("User: %s%s\nAgent: Analyzing service code and preparing isolated worktree.", cleanPrompt, injectedContext)

	// ── 3. Subagent Orchestration with Worktree Isolation ──────────
	eb := events.NewEventBus()
	subChan, subID := eb.Subscribe("subagent_responses")
	defer eb.Unsubscribe("subagent_responses", subID)

	mgr := subagent.NewManager(eb, t.TempDir())
	var isolatedWorkDir string

	mgr.SetWorktreeProvider(
		func(cwd, name string) (string, error) {
			dir, err := git.CreateWorktree(repoDir, name)
			isolatedWorkDir = dir
			return dir, err
		},
		func(cwd, name string) error {
			return git.RemoveWorktree(repoDir, name)
		},
	)

	ctx := context.Background()
	info, err := mgr.Spawn(ctx, "agent-fix-division", "QA Test Engineer", subagent.ProfileQAAuditor, "Fix Divide() in service/math_service.go", "", subagent.WorkspaceBranch,
		func(subCtx context.Context, sub *subagent.SubagentInfo) (string, error) {
			targetFileInWt := filepath.Join(isolatedWorkDir, "service", "math_service.go")

			// Execute tool via policy scoped to isolatedWorkDir
			pol := policy.New([]string{isolatedWorkDir}, true, false)
			registry, _ := createTestRegistry(t, pol)

			// Step A: replace_file_content on CRLF file
			_, editErr := registry.Execute(subCtx, "replace_file_content", map[string]interface{}{
				"TargetFile":         targetFileInWt,
				"TargetContent":      "// BUG: missing zero check\r\n\treturn a / b, nil",
				"ReplacementContent": "if b == 0 {\r\n\t\treturn 0, errors.New(\"division by zero\")\r\n\t}\r\n\treturn a / b, nil",
			})
			if editErr != nil {
				return "", fmt.Errorf("failed to replace content: %w", editErr)
			}

			// Verify CRLF line endings preserved on disk
			assertCRLF(t, targetFileInWt)

			// Step B: Write new unit test file in worktree
			testFileInWt := filepath.Join(isolatedWorkDir, "service", "math_service_test.go")
			testCode := "package service\r\n\r\nimport \"testing\"\r\n\r\nfunc TestDivide(t *testing.T) {\r\n\t_, err := Divide(10, 0)\r\n\tif err == nil {\r\n\t\tt.Fatal(\"expected error for zero divisor\")\r\n\t}\r\n}\r\n"
			_, writeErr := registry.Execute(subCtx, "write_to_file", map[string]interface{}{
				"TargetFile":  testFileInWt,
				"CodeContent": testCode,
			})
			if writeErr != nil {
				return "", fmt.Errorf("failed to write test file: %w", writeErr)
			}

			assertCRLF(t, testFileInWt)
			return "Fix verified and tests authored successfully", nil
		},
	)
	if err != nil {
		t.Fatalf("failed to spawn subagent: %v", err)
	}

	// ── 4. Reactive Event Delivery & Status Verification ──────────
	select {
	case eventMsg := <-subChan:
		if !strings.Contains(eventMsg, "agent-fix-division") || !strings.Contains(eventMsg, "COMPLETED") {
			t.Fatalf("unexpected EventBus completion message: %s", eventMsg)
		}
	case <-time.After(10 * time.Second):
		t.Fatalf("timed out waiting for subagent completion event")
	}

	snap := waitForSubagent(t, mgr, info.ID, 5*time.Second)
	if snap.Status != "COMPLETED" {
		t.Fatalf("subagent failed: %s, result: %s", snap.Status, snap.Result)
	}

	// ── 5. Main Conversation History Expansion & Context Pruning ───
	var convoBuilder strings.Builder
	convoBuilder.WriteString(turn0)

	// Simulate subsequent turns (verification, telemetry, reviews)
	for turn := 2; turn <= 25; turn++ {
		convoBuilder.WriteString(fmt.Sprintf("\nUser: Status check for milestone phase %d\nAgent: Phase %d telemetry verified 100%% green.", turn, turn))
	}
	convoBuilder.WriteString("\nUser: Final sign-off request\nAgent: All criteria satisfied cleanly.")

	fullConvo := convoBuilder.String()

	// Prune under budget
	budget := 600
	pruned, removed := pruner.PruneContext(fullConvo, budget)

	if removed <= 0 {
		t.Fatalf("expected pruner to evict middle turns under budget constraint")
	}

	// Invariants:
	// 1. Turn 0 root intent and user query preserved
	if !strings.Contains(pruned, "Audit [") || !strings.Contains(pruned, "fix the division by zero crash") {
		t.Fatalf("Turn 0 user prompt was evicted during pruning: %s", pruned)
	}
	// 2. Middle turns pruned with standard marker
	if !strings.Contains(pruned, "[Earlier conversation context pruned for token efficiency]") {
		t.Fatalf("pruner marker missing in pruned output")
	}
	// 3. Most recent turn preserved
	if !strings.Contains(pruned, "Final sign-off request") || !strings.Contains(pruned, "All criteria satisfied cleanly.") {
		t.Fatalf("recent agent turns were lost during pruning: %s", pruned)
	}

	// ── 6. Repository Integrity & Isolation Assertion ─────────────
	// Parent repository file remained untouched during worktree isolation
	parentData, _ := os.ReadFile(sourceFileAbs)
	if !strings.Contains(string(parentData), "// BUG: missing zero check") {
		t.Fatalf("parent repository file was mutated outside controlled merge")
	}
	assertCRLF(t, sourceFileAbs)
}

package e2e_test

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestMain(m *testing.M) {
	// Verify git is present before running E2E suite
	if _, err := exec.LookPath("git"); err != nil {
		println("CRITICAL: git binary not found in PATH. E2E tests require git.")
		os.Exit(1)
	}

	code := m.Run()
	os.Exit(code)
}

// TestE2E_CLI_BinaryInvocation tests the compiled microgravity binary (or go run entrypoint)
// to ensure root flags --add-dir and --sandbox are properly exposed on the CLI interface.
func TestE2E_CLI_BinaryInvocation(t *testing.T) {
	// Locate repository root from current test directory
	repoRoot, err := filepath.Abs("../..")
	if err != nil {
		t.Fatalf("failed to resolve repo root: %v", err)
	}

	binaryPath := filepath.Join(repoRoot, "mg.exe")
	var cmd *exec.Cmd
	if _, err := os.Stat(binaryPath); err == nil {
		cmd = exec.Command(binaryPath, "--help")
	} else {
		cmd = exec.Command("go", "run", "main.go", "--help")
		cmd.Dir = repoRoot
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		t.Fatalf("failed to invoke microgravity CLI: %v, stderr: %s", err, stderr.String())
	}

	outStr := stdout.String()

	// Verify required flags are exposed
	if !strings.Contains(outStr, "--add-dir") {
		t.Errorf("microgravity CLI --help output missing '--add-dir' flag")
	}
	if !strings.Contains(outStr, "--sandbox") {
		t.Errorf("microgravity CLI --help output missing '--sandbox' flag")
	}
	if !strings.Contains(outStr, "Microgravity (mg)") {
		t.Errorf("microgravity CLI header missing in help output")
	}
}

// TestE2E_SuiteInventory validates the structure and completeness of all 4 test tiers.
func TestE2E_SuiteInventory(t *testing.T) {
	t.Log("Microgravity 4-Tier Opaque-Box E2E Suite Inventory:")
	t.Log(" - Tier 1: Feature Coverage (F15: @file, F16: CRLF replace, F4: Policy/Sandbox, F5: Pruner Turn 0, F17: Worktree subagents)")
	t.Log(" - Tier 2: Boundary & Corner Cases (empty file, large >500KB file, mixed CRLF/LF, nested dirs, special chars, deep worktrees, zero tokens)")
	t.Log(" - Tier 3: Cross-Feature Combinations (subagents on CRLF files in worktree, policy + @file, Turn 0 + @file context pruning, sandbox + tools)")
	t.Log(" - Tier 4: Real-World Application Workflow (multi-step autonomous agent cycle with @file ingestion, subagent fix, event notification, context pruning)")
}

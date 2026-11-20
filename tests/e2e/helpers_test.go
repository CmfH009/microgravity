package e2e_test

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/CmfH009/microgravity/internal/events"
	"github.com/CmfH009/microgravity/internal/policy"
	"github.com/CmfH009/microgravity/internal/scheduler"
	"github.com/CmfH009/microgravity/internal/subagent"
	"github.com/CmfH009/microgravity/internal/tools"
)

// initGitRepo initializes an isolated temporary Git repository with an initial commit.
func initGitRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	runGit(t, dir, "init")
	runGit(t, dir, "config", "user.name", "E2E Test Writer")
	runGit(t, dir, "config", "user.email", "test-writer@example.com")
	runGit(t, dir, "config", "commit.gpgsign", "false")

	initFile := filepath.Join(dir, "README.md")
	if err := os.WriteFile(initFile, []byte("# E2E Test Repository\r\nInitial content\r\n"), 0644); err != nil {
		t.Fatalf("failed to write initial file: %v", err)
	}

	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "-m", "chore: initial commit for e2e test")

	return dir
}

// runGit runs a git command in the specified directory and asserts success.
func runGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("git %s failed in %s: %v, stderr: %s", strings.Join(args, " "), dir, err, stderr.String())
	}
	return stdout.String()
}

// assertCRLF asserts that the target file exists and uses Windows CRLF (\r\n) line endings.
func assertCRLF(t *testing.T, filePath string) {
	t.Helper()
	data, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("assertCRLF failed to read %s: %v", filePath, err)
	}
	if !bytes.Contains(data, []byte("\r\n")) {
		t.Fatalf("expected file %s to contain CRLF (\\r\\n), got bytes: %q", filePath, string(data))
	}
}

// assertLF asserts that the target file exists and uses Unix LF (\n) line endings (no CRLF).
func assertLF(t *testing.T, filePath string) {
	t.Helper()
	data, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("assertLF failed to read %s: %v", filePath, err)
	}
	if bytes.Contains(data, []byte("\r\n")) {
		t.Fatalf("expected file %s to contain LF only (no CRLF), got: %q", filePath, string(data))
	}
	if !bytes.Contains(data, []byte("\n")) {
		t.Fatalf("expected file %s to contain LF (\\n), got: %q", filePath, string(data))
	}
}

// createTestRegistry instantiates a tools Registry bound to the specified policy.
func createTestRegistry(t *testing.T, pol *policy.Policy) (*tools.Registry, *events.EventBus) {
	t.Helper()
	r := tools.NewRegistry()
	eb := events.NewEventBus()
	sched := scheduler.New()
	t.Cleanup(func() {
		sched.Stop()
	})
	notifyChan := make(chan string, 16)

	tools.RegisterBuiltinsWithPolicy(r, eb, sched, notifyChan, nil, pol)
	return r, eb
}

// waitForSubagent polls a subagent until its status is no longer RUNNING or timeout occurs.
func waitForSubagent(t *testing.T, mgr *subagent.Manager, id string, timeout time.Duration) *subagent.SubagentInfo {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		snap, ok := mgr.Get(id)
		if ok && snap.Status != "RUNNING" {
			return snap
		}
		time.Sleep(50 * time.Millisecond)
	}
	snap, _ := mgr.Get(id)
	return snap
}

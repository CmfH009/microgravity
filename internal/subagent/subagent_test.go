package subagent

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestSubagentSnapshotIsIndependent(t *testing.T) {
	m := NewManager(nil, t.TempDir())
	info, err := m.Spawn(context.Background(), "snapshot", "worker", ProfileGeneral, "task", "", WorkspaceInherit, nil)
	if err != nil {
		t.Fatal(err)
	}
	list := m.List()
	if len(list) != 1 || list[0] == info {
		t.Fatal("expected List to return an independent snapshot")
	}
}

func TestSubagent_DefaultRunnerAndWorktree(t *testing.T) {
	m := NewManager(nil, t.TempDir())

	var mu sync.Mutex
	createdWT := ""
	removedWT := ""
	runnerCalled := false

	m.SetWorktreeProvider(func(cwd, name string) (string, error) {
		mu.Lock()
		defer mu.Unlock()
		createdWT = name
		return "/mock/worktree/" + name, nil
	}, func(cwd, name string) error {
		mu.Lock()
		defer mu.Unlock()
		removedWT = name
		return nil
	})

	m.SetDefaultRunner(func(ctx context.Context, sub *SubagentInfo, workDir string) (string, error) {
		mu.Lock()
		defer mu.Unlock()
		runnerCalled = true
		if !strings.Contains(workDir, "mock/worktree") {
			t.Errorf("expected workDir to be in mock/worktree, got %s", workDir)
		}
		return "worker success", nil
	})

	info, err := m.Spawn(context.Background(), "test-worktree", "coder", ProfileGeneral, "build feature", "", WorkspaceBranch, nil)
	if err != nil {
		t.Fatalf("Spawn failed: %v", err)
	}

	// Wait briefly for goroutine to finish
	time.Sleep(150 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()

	if !runnerCalled {
		t.Errorf("expected defaultRunner to be called")
	}
	if createdWT != "subagent-test-worktree" {
		t.Errorf("expected created worktree 'subagent-test-worktree', got %s", createdWT)
	}
	if removedWT != "subagent-test-worktree" {
		t.Errorf("expected removed worktree 'subagent-test-worktree', got %s", removedWT)
	}
	latest, ok := m.Get(info.ID)
	if !ok {
		t.Fatalf("subagent not found in manager")
	}
	if latest.GetStatus() != "COMPLETED" {
		t.Errorf("expected COMPLETED status, got %s", latest.GetStatus())
	}
}

package subagent

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
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

func waitForCondition(t *testing.T, timeout time.Duration, desc string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for: %s", desc)
}

func TestSubagentKillAndKillAllIdempotency(t *testing.T) {
	m := NewManager(nil, t.TempDir())

	// 1. Spawn subagent that completes quickly
	_, err := m.Spawn(context.Background(), "completed-1", "worker", ProfileGeneral, "quick", "", WorkspaceInherit, func(ctx context.Context, sub *SubagentInfo) (string, error) {
		return "all done", nil
	})
	if err != nil {
		t.Fatal(err)
	}
	waitForCondition(t, 2*time.Second, "completed-1 completion", func() bool {
		info, ok := m.Get("completed-1")
		return ok && info.GetStatus() == "COMPLETED"
	})

	beforeSnap, _ := m.Get("completed-1")
	if killed := m.Kill("completed-1"); killed {
		t.Fatal("expected Kill on already-completed subagent to return false")
	}
	afterSnap, _ := m.Get("completed-1")
	if afterSnap.GetStatus() != "COMPLETED" || afterSnap.GetState() != StateCompleted || afterSnap.GetResult() != "all done" {
		t.Fatalf("Kill clobbered completed subagent state: %+v", afterSnap)
	}
	if !afterSnap.EndTime.Equal(beforeSnap.EndTime) {
		t.Fatalf("Kill mutated EndTime of completed subagent: %v -> %v", beforeSnap.EndTime, afterSnap.EndTime)
	}

	// 2. Spawn blocking subagent A and kill it twice
	startedA := make(chan struct{})
	releaseA := make(chan struct{})
	_, err = m.Spawn(context.Background(), "kill-a", "worker", ProfileGeneral, "block a", "", WorkspaceInherit, func(ctx context.Context, sub *SubagentInfo) (string, error) {
		close(startedA)
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-releaseA:
			return "late success", nil
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	<-startedA

	if !m.Kill("kill-a") {
		t.Fatal("expected first Kill(kill-a) to return true")
	}
	killedSnap1, _ := m.Get("kill-a")
	if killedSnap1.GetStatus() != "KILLED" || killedSnap1.GetState() != StateKilled {
		t.Fatalf("expected KILLED state, got status=%s state=%s", killedSnap1.GetStatus(), killedSnap1.GetState())
	}

	// Second Kill on already-killed subagent must be idempotent (return false, preserve EndTime)
	time.Sleep(15 * time.Millisecond)
	if m.Kill("kill-a") {
		t.Fatal("expected second Kill(kill-a) to return false")
	}
	killedSnap2, _ := m.Get("kill-a")
	if !killedSnap2.EndTime.Equal(killedSnap1.EndTime) {
		t.Fatalf("second Kill mutated EndTime: %v -> %v", killedSnap1.EndTime, killedSnap2.EndTime)
	}

	// 3. Even if a runner ignores ctx and returns nil after Kill, state must remain KILLED
	startedIg := make(chan struct{})
	releaseIg := make(chan struct{})
	_, err = m.Spawn(context.Background(), "ignore-ctx", "worker", ProfileGeneral, "ignore", "", WorkspaceInherit, func(ctx context.Context, sub *SubagentInfo) (string, error) {
		close(startedIg)
		<-releaseIg
		return "should not overwrite killed", nil
	})
	if err != nil {
		t.Fatal(err)
	}
	<-startedIg
	if !m.Kill("ignore-ctx") {
		t.Fatal("expected Kill(ignore-ctx) to return true")
	}
	close(releaseIg)
	time.Sleep(50 * time.Millisecond)
	igSnap, _ := m.Get("ignore-ctx")
	if igSnap.GetStatus() != "KILLED" || igSnap.GetState() != StateKilled {
		t.Fatalf("runner completion overwrote KILLED state: status=%s state=%s result=%q", igSnap.GetStatus(), igSnap.GetState(), igSnap.GetResult())
	}

	// 4. Spawn running subagent B and verify KillAll idempotency
	startedB := make(chan struct{})
	_, err = m.Spawn(context.Background(), "kill-b", "worker", ProfileGeneral, "block b", "", WorkspaceInherit, func(ctx context.Context, sub *SubagentInfo) (string, error) {
		close(startedB)
		<-ctx.Done()
		return "", ctx.Err()
	})
	if err != nil {
		t.Fatal(err)
	}
	<-startedB

	if count := m.KillAll(); count != 1 {
		t.Fatalf("expected KillAll to kill 1 running subagent, got %d", count)
	}
	if count := m.KillAll(); count != 0 {
		t.Fatalf("expected second KillAll to kill 0 subagents, got %d", count)
	}
	if m.Kill("nonexistent") {
		t.Fatal("expected Kill(nonexistent) to return false")
	}
}

func TestSubagentCancellationPropagationAndNoLeak(t *testing.T) {
	// Warm up runtime and record baseline goroutines
	runtime.GC()
	baseline := runtime.NumGoroutine()

	m := NewManager(nil, t.TempDir())
	const n = 20
	var active atomic.Int64
	var started sync.WaitGroup
	started.Add(n)

	ctx, cancel := context.WithCancel(context.Background())
	for i := 0; i < n; i++ {
		id := fmt.Sprintf("cancel-%d", i)
		_, err := m.Spawn(ctx, id, "worker", ProfileResearcher, "wait", "", WorkspaceInherit, func(subCtx context.Context, sub *SubagentInfo) (string, error) {
			active.Add(1)
			defer active.Add(-1)
			started.Done()
			<-subCtx.Done()
			return "", subCtx.Err()
		})
		if err != nil {
			t.Fatalf("Spawn %s failed: %v", id, err)
		}
	}

	started.Wait()
	cancel()

	waitForCondition(t, 3*time.Second, "all canceled subagents exit", func() bool {
		return active.Load() == 0
	})
	for i := 0; i < n; i++ {
		id := fmt.Sprintf("cancel-%d", i)
		waitForCondition(t, 2*time.Second, id+" state killed", func() bool {
			info, ok := m.Get(id)
			return ok && info.GetState() == StateKilled && info.GetStatus() == "KILLED"
		})
	}

	// Default runner (nil) must also cancel promptly instead of sleeping blindly
	dCtx, dCancel := context.WithCancel(context.Background())
	dCancel()
	if _, err := m.Spawn(dCtx, "default-canceled", "worker", ProfileGeneral, "default", "", WorkspaceInherit, nil); err != nil && !errors.Is(err, context.Canceled) {
		t.Fatalf("unexpected error on pre-canceled spawn: %v", err)
	}

	waitForCondition(t, 2*time.Second, "goroutines return to baseline", func() bool {
		runtime.Gosched()
		return runtime.NumGoroutine() <= baseline+2
	})
}

func TestSubagentConcurrentRaceStressAndSnapshot(t *testing.T) {
	m := NewManager(nil, t.TempDir())
	var wg sync.WaitGroup

	// Concurrently mutate manager configuration while spawning/reading/killing
	for i := 0; i < 30; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			if i%5 == 0 {
				m.SetDefaultRunner(func(ctx context.Context, sub *SubagentInfo, workDir string) (string, error) {
					_ = sub.Status
					_ = sub.State
					select {
					case <-ctx.Done():
						return "", ctx.Err()
					case <-time.After(5 * time.Millisecond):
						return "ok", nil
					}
				})
				m.SetWorktreeProvider(nil, nil)
			}
			id := fmt.Sprintf("race-%d", i)
			snap, err := m.Spawn(context.Background(), id, "stress", ProfileGeneral, "work", "", WorkspaceInherit, nil)
			if err != nil {
				return
			}
			_ = snap.GetStatus()
			_ = snap.GetState()
			_ = snap.GetResult()
			_ = m.SendMessage("main", id, fmt.Sprintf("msg-%d", i))
			_ = m.GetMessages(id)
			_ = m.List()
			if got, ok := m.Get(id); ok {
				_ = got.Snapshot()
			}
			if i%3 == 0 {
				m.Kill(id)
			}
		}()
	}

	wg.Wait()
	m.KillAll()
	waitCtx, waitCancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer waitCancel()
	if err := m.Wait(waitCtx); err != nil {
		t.Fatalf("Wait failed: %v", err)
	}
}

func TestSubagentWaitPanicRecoveryAndBoundedMailbox(t *testing.T) {
	m := NewManager(nil, t.TempDir())

	// 1. Panic inside runner is recovered and transitions subagent to FAILED/StateErrored
	_, err := m.Spawn(context.Background(), "panic-sub", "worker", ProfileGeneral, "boom", "", WorkspaceInherit, func(ctx context.Context, sub *SubagentInfo) (string, error) {
		panic("unexpected failure")
	})
	if err != nil {
		t.Fatal(err)
	}

	// Duplicate running ID check
	blockCh := make(chan struct{})
	_, err = m.Spawn(context.Background(), "dup-id", "worker", ProfileGeneral, "hold", "", WorkspaceInherit, func(ctx context.Context, sub *SubagentInfo) (string, error) {
		<-blockCh
		return "done", nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, dupErr := m.Spawn(context.Background(), "dup-id", "worker", ProfileGeneral, "dup", "", WorkspaceInherit, nil); dupErr == nil {
		t.Fatal("expected duplicate running subagent id to be rejected")
	}

	// 2. Bounded mailbox and ConversationID lookup in GetMessages
	for i := 0; i < maxMailboxMessages+50; i++ {
		if err := m.SendMessage("main", "subagent-dup-id", fmt.Sprintf("m-%d", i)); err != nil {
			t.Fatalf("SendMessage failed: %v", err)
		}
	}
	msgs := m.GetMessages("subagent-dup-id")
	if len(msgs) != maxMailboxMessages {
		t.Fatalf("expected bounded mailbox length %d, got %d", maxMailboxMessages, len(msgs))
	}
	if msgs[0].Content != "m-50" {
		t.Fatalf("expected oldest messages dropped in ring order, got first=%q", msgs[0].Content)
	}
	if secondRead := m.GetMessages("dup-id"); len(secondRead) != 0 {
		t.Fatalf("expected mailbox cleared after consumption, got %d", len(secondRead))
	}

	close(blockCh)

	waitCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := m.Wait(waitCtx); err != nil {
		t.Fatalf("Wait failed: %v", err)
	}

	panicInfo, ok := m.Get("panic-sub")
	if !ok {
		t.Fatal("panic-sub missing")
	}
	if panicInfo.GetStatus() != "FAILED" || panicInfo.GetState() != StateErrored || !strings.Contains(panicInfo.GetResult(), "panic") {
		t.Fatalf("expected FAILED/StateErrored with panic message, got %+v", panicInfo)
	}
}

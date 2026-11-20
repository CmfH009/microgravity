package tasks

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/CmfH009/microgravity/internal/events"
)

func TestTaskManagerSynchronousExecution(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "task_test_*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	eb := events.NewEventBus()
	notifyChan := make(chan string, 10)
	mgr := NewManager(tempDir, eb, notifyChan)

	ctx := context.Background()
	res, err := mgr.Launch(ctx, "echo 'hello tasks'", tempDir, 5000, "test-conv")
	if err != nil {
		t.Fatalf("Launch failed: %v", err)
	}

	if res.IsBackground {
		t.Errorf("Expected synchronous execution, got background")
	}
	if res.ExitCode != 0 {
		t.Errorf("Expected exit code 0, got %d", res.ExitCode)
	}
	if !strings.Contains(res.Output, "hello tasks") {
		t.Errorf("Expected output containing 'hello tasks', got %q", res.Output)
	}

	tasks := mgr.List()
	if len(tasks) != 1 {
		t.Errorf("Expected 1 task, got %d", len(tasks))
	}
}

func TestTaskManagerBackgroundAndKill(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "task_test_bg_*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	eb := events.NewEventBus()
	notifyChan := make(chan string, 10)
	mgr := NewManager(tempDir, eb, notifyChan)

	ctx := context.Background()
	// Launch command that sleeps
	res, err := mgr.Launch(ctx, "Start-Sleep -Seconds 5", tempDir, 200, "test-conv")
	if err != nil {
		t.Fatalf("Launch failed: %v", err)
	}

	if !res.IsBackground {
		t.Fatalf("Expected background task, got synchronous")
	}

	// Check status
	status, err := mgr.GetStatusOutput(res.TaskID)
	if err != nil {
		t.Fatalf("GetStatusOutput failed: %v", err)
	}
	if !strings.Contains(status, "RUNNING") {
		t.Errorf("Expected status to contain RUNNING, got: %s", status)
	}

	// Kill task
	killed := mgr.Kill(res.TaskID)
	if !killed {
		t.Errorf("Expected task to be killed")
	}

	time.Sleep(100 * time.Millisecond)
	task, ok := mgr.Get(res.TaskID)
	if !ok {
		t.Fatal("expected task to remain queryable")
	}
	task.mu.RLock()
	status = string(task.Status)
	task.mu.RUnlock()
	if status != string(StatusKilled) {
		t.Errorf("Expected task status KILLED, got %s", status)
	}
}

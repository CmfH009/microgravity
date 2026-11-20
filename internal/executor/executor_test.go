package executor

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/purav/microgravity/internal/cmdparse"
	"github.com/purav/microgravity/internal/events"
	"github.com/purav/microgravity/internal/plan"
	"github.com/purav/microgravity/internal/scheduler"
	"github.com/purav/microgravity/internal/tools"
)

func TestExecuteToolNative(t *testing.T) {
	registry := tools.NewRegistry()
	eb := events.NewEventBus()
	sched := scheduler.New()
	defer sched.Stop()
	tools.RegisterBuiltins(registry, eb, sched, nil, nil)

	exec := New(registry, nil)

	ctx := context.Background()
	args := map[string]interface{}{"command": "echo hello"}

	startTime := time.Now()
	output, success, duration := exec.ExecuteTool(ctx, "run_terminal_command", args)

	if !success {
		t.Fatalf("Expected native tool execution to succeed, output: %s", output)
	}

	if !strings.Contains(output, "hello") {
		t.Errorf("Expected output to contain 'hello', got: %s", output)
	}

	if duration < 0 {
		t.Errorf("Expected non-negative duration")
	}
	_ = startTime
}

func TestExecuteToolUnknown(t *testing.T) {
	registry := tools.NewRegistry()
	exec := New(registry, nil)

	ctx := context.Background()
	output, success, _ := exec.ExecuteTool(ctx, "non_existent_tool", nil)

	if success {
		t.Errorf("Expected unknown tool execution to fail")
	}

	if !strings.Contains(output, "Unknown tool") {
		t.Errorf("Expected output to contain 'Unknown tool', got: %s", output)
	}
}

func TestExecuteTool_GetCurrentTime(t *testing.T) {
	registry := tools.NewRegistry()
	eb := events.NewEventBus()
	sched := scheduler.New()
	defer sched.Stop()
	tools.RegisterBuiltins(registry, eb, sched, nil, nil)

	exec := New(registry, nil)
	ctx := context.Background()

	output, success, _ := exec.ExecuteTool(ctx, "get_current_time", map[string]interface{}{})
	if !success {
		t.Fatalf("Expected get_current_time execution to succeed, output: %s", output)
	}

	if !strings.Contains(output, "Current Date & Time") {
		t.Errorf("Expected output to contain 'Current Date & Time', got: %s", output)
	}
}

func TestHandleToolCall_Success(t *testing.T) {
	registry := tools.NewRegistry()
	eb := events.NewEventBus()
	sched := scheduler.New()
	defer sched.Stop()
	tools.RegisterBuiltins(registry, eb, sched, nil, nil)

	exec := New(registry, nil)
	ctx := context.Background()

	out, ok := exec.HandleToolCall(ctx, "get_current_time", map[string]interface{}{}, ".", nil)
	if !ok {
		t.Fatalf("Expected HandleToolCall to succeed")
	}
	if !strings.Contains(out, "Current Date & Time") {
		t.Errorf("Expected output to contain 'Current Date & Time', got: %s", out)
	}
}

func TestExecuteToolCalls_WithActivePlan(t *testing.T) {
	registry := tools.NewRegistry()
	eb := events.NewEventBus()
	sched := scheduler.New()
	defer sched.Stop()
	tools.RegisterBuiltins(registry, eb, sched, nil, nil)

	exec := New(registry, nil)
	ctx := context.Background()

	p := plan.NewPlan("Test Plan")
	p.AddTask("Step 1: Check time")
	p.AddTask("Step 2: Run invalid tool")

	calls := []cmdparse.ToolCall{
		{ToolCall: "get_current_time", Arguments: map[string]interface{}{}},
		{ToolCall: "non_existent_tool", Arguments: map[string]interface{}{}},
	}

	_, allSuccess := exec.ExecuteToolCalls(ctx, calls, ".", p)
	if allSuccess {
		t.Errorf("Expected allSuccess to be false due to non_existent_tool")
	}

	tasks := p.GetTasks()
	if len(tasks) != 2 {
		t.Fatalf("Expected 2 tasks, got %d", len(tasks))
	}

	if tasks[0].Status != plan.StatusCompleted {
		t.Errorf("Expected task 0 to be StatusCompleted, got %v", tasks[0].Status)
	}

	if tasks[1].Status != plan.StatusFailed {
		t.Errorf("Expected task 1 to be StatusFailed, got %v", tasks[1].Status)
	}
}


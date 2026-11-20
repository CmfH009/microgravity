package executor

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/purav/microgravity/internal/cmdparse"
	"github.com/purav/microgravity/internal/mcp"
	"github.com/purav/microgravity/internal/plan"
	"github.com/purav/microgravity/internal/tools"
	"github.com/purav/microgravity/internal/ui"
)

type ToolExecutionResult struct {
	ToolName  string
	Arguments map[string]interface{}
	Output    string
	Success   bool
	Duration  time.Duration
}

type Executor struct {
	Registry    *tools.Registry
	MCPRouter   map[string]*mcp.Client
	maxParallel int
}

func New(registry *tools.Registry, mcpRouter map[string]*mcp.Client) *Executor {
	if mcpRouter == nil {
		mcpRouter = make(map[string]*mcp.Client)
	}
	return &Executor{
		Registry:    registry,
		MCPRouter:   mcpRouter,
		maxParallel: maxParallelFromEnv(),
	}
}

// maxParallelFromEnv reads MG_MAX_PARALLEL_TOOLS (default 1 = sequential).
func maxParallelFromEnv() int {
	if v := os.Getenv("MG_MAX_PARALLEL_TOOLS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	return 1
}

// SetMaxParallel sets the maximum number of concurrent tool executions.
func (e *Executor) SetMaxParallel(n int) {
	if n < 1 {
		n = 1
	}
	e.maxParallel = n
}

// MaxParallel returns the current concurrency limit.
func (e *Executor) MaxParallel() int {
	return e.maxParallel
}

// ExecuteTool executes a native or MCP tool, measures duration, and returns the raw output.
func (e *Executor) ExecuteTool(ctx context.Context, toolName string, args map[string]interface{}) (output string, success bool, duration time.Duration) {
	startTime := time.Now()
	success = true

	if e.Registry.IsNative(toolName) {
		out, err := e.Registry.Execute(ctx, toolName, args)
		if err != nil {
			output = fmt.Sprintf("Error calling native tool: %v", err)
			success = false
		} else {
			output = out
		}
	} else if client, ok := e.MCPRouter[toolName]; ok {
		content, err := client.CallTool(toolName, args)
		if err != nil {
			output = fmt.Sprintf("Error calling MCP tool: %v", err)
			success = false
		} else {
			for _, c := range content {
				output += c.Text + "\n"
			}
		}
	} else {
		output = fmt.Sprintf("Error: Unknown tool %s and no native/MCP provider found.", toolName)
		success = false
	}

	duration = time.Since(startTime)
	return output, success, duration
}

// HandleToolCall executes a single tool, renders TUI cards, runs doubt-driven verifiers, and updates active plans.
func (e *Executor) HandleToolCall(ctx context.Context, toolName string, args map[string]interface{}, cwd string, activePlan *plan.Plan) (toolOutput string, success bool) {
	output, success, duration := e.ExecuteTool(ctx, toolName, args)

	// Render LipGloss Tool Call Card
	toolCard := ui.RenderToolCallCard(toolName, args, duration, success, output)
	fmt.Println()
	fmt.Println(toolCard)

	return output, success
}

// ExecuteToolCalls executes a batch of tool calls. When maxParallel > 1,
// independent calls run concurrently; results are aggregated in call order.
func (e *Executor) ExecuteToolCalls(ctx context.Context, calls []cmdparse.ToolCall, cwd string, activePlan *plan.Plan) (combinedOutput string, allSuccess bool) {
	if len(calls) == 0 {
		return "", true
	}

	if e.maxParallel <= 1 || len(calls) == 1 {
		return e.executeToolCallsSequential(ctx, calls, cwd, activePlan)
	}
	return e.executeToolCallsParallel(ctx, calls, cwd, activePlan)
}

type toolResult struct {
	index   int
	tool    string
	output  string
	success bool
}

// executeToolCallsSequential runs calls one at a time (original behavior).
func (e *Executor) executeToolCallsSequential(ctx context.Context, calls []cmdparse.ToolCall, cwd string, activePlan *plan.Plan) (combinedOutput string, allSuccess bool) {
	allSuccess = true
	var sb strings.Builder

	for i, call := range calls {
		if activePlan != nil {
			activePlan.MarkNextPending(plan.StatusInProgress)
		}

		output, success, duration := e.ExecuteTool(ctx, call.ToolCall, call.Arguments)
		if !success {
			allSuccess = false
		}

		toolCard := ui.RenderToolCallCard(call.ToolCall, call.Arguments, duration, success, output)
		fmt.Println()
		fmt.Println(toolCard)

		if len(calls) > 1 {
			sb.WriteString(fmt.Sprintf("── [Tool Call %d/%d: %s] ──\n%s\n\n", i+1, len(calls), call.ToolCall, strings.TrimSpace(output)))
		} else {
			sb.WriteString(strings.TrimSpace(output))
		}

		if activePlan != nil {
			if success {
				activePlan.MarkNextPending(plan.StatusCompleted)
			} else {
				activePlan.MarkNextPending(plan.StatusFailed)
			}
		}
	}

	return strings.TrimSpace(sb.String()), allSuccess
}

// executeToolCallsParallel runs calls concurrently (up to maxParallel),
// serializing TUI rendering and plan updates. Results are aggregated in order.
func (e *Executor) executeToolCallsParallel(ctx context.Context, calls []cmdparse.ToolCall, cwd string, activePlan *plan.Plan) (combinedOutput string, allSuccess bool) {
	n := len(calls)
	results := make([]toolResult, n)

	sem := make(chan struct{}, e.maxParallel)
	var wg sync.WaitGroup
	var renderMu sync.Mutex

	for i, call := range calls {
		wg.Add(1)
		go func(idx int, c cmdparse.ToolCall) {
			defer wg.Done()

			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				results[idx] = toolResult{index: idx, tool: c.ToolCall, output: "context cancelled", success: false}
				return
			}
			defer func() { <-sem }()

			if ctx.Err() != nil {
				results[idx] = toolResult{index: idx, tool: c.ToolCall, output: "context cancelled", success: false}
				return
			}

			output, success, duration := e.ExecuteTool(ctx, c.ToolCall, c.Arguments)

			// Serialize TUI rendering and plan updates.
			renderMu.Lock()
			toolCard := ui.RenderToolCallCard(c.ToolCall, c.Arguments, duration, success, output)
			fmt.Println()
			fmt.Println(toolCard)
			if activePlan != nil {
				if success {
					activePlan.MarkNextPending(plan.StatusCompleted)
				} else {
					activePlan.MarkNextPending(plan.StatusFailed)
				}
			}
			renderMu.Unlock()

			if !success {
				allSuccess = false
			}
			results[idx] = toolResult{index: idx, tool: c.ToolCall, output: output, success: success}
		}(i, call)
	}

	wg.Wait()

	// Aggregate results in original call order.
	allSuccess = true
	var sb strings.Builder
	for i := 0; i < n; i++ {
		r := results[i]
		if !r.success {
			allSuccess = false
		}
		if n > 1 {
			sb.WriteString(fmt.Sprintf("── [Tool Call %d/%d: %s] ──\n%s\n\n", i+1, n, r.tool, strings.TrimSpace(r.output)))
		} else {
			sb.WriteString(strings.TrimSpace(r.output))
		}
	}

	return strings.TrimSpace(sb.String()), allSuccess
}

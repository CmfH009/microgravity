# Native Tools Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement built-in `run_terminal_command`, `read_file`, and `write_file` native tools directly inside `microgravity`.

**Architecture:** Create an `internal/tools` registry that defines and maps native tools to their executions. Update `cmd/run.go` and `cmd/chat.go` to append these native tools to the system prompt and intercept their execution before falling back to MCP.

**Tech Stack:** Go, `os`, `os/exec`, `internal/mcp`

---

### Task 1: The Native Tools Registry Definition

**Files:**
- Create: `internal/tools/registry.go`
- Create: `internal/tools/registry_test.go`

- [ ] **Step 1: Write the failing test**

```go
package tools

import (
	"testing"
)

func TestRegistryIsNative(t *testing.T) {
	if !IsNative("read_file") {
		t.Errorf("read_file should be native")
	}
	if IsNative("unknown_tool") {
		t.Errorf("unknown_tool should not be native")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/tools/...`
Expected: FAIL due to missing package `tools` and undefined functions.

- [ ] **Step 3: Write minimal implementation**

Create `internal/tools/registry.go`:

```go
package tools

import (
	"fmt"
	"github.com/purav/microgravity/internal/mcp"
)

type ToolHandler func(args map[string]interface{}) (string, error)

var registry = map[string]ToolHandler{}
var nativeSchemas = []mcp.Tool{}

// Register adds a tool to the native registry.
func Register(schema mcp.Tool, handler ToolHandler) {
	registry[schema.Name] = handler
	nativeSchemas = append(nativeSchemas, schema)
}

// GetNativeTools returns the list of native tool schemas.
func GetNativeTools() []mcp.Tool {
	return nativeSchemas
}

// IsNative checks if a tool exists in the native registry.
func IsNative(name string) bool {
	_, exists := registry[name]
	return exists
}

// Execute runs a native tool by name.
func Execute(name string, args map[string]interface{}) (string, error) {
	handler, exists := registry[name]
	if !exists {
		return "", fmt.Errorf("native tool %s not found", name)
	}
	return handler(args)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/tools/...`
Expected: PASS (Wait, it will fail because `read_file` is not registered yet in `registry_test.go`. Let's fix the test to explicitly register a mock first.)

*Correction for Step 1 & 3: We should add a `init()` mock in the test, or rely on `builtins.go` which we haven't written yet. So let's make Step 1 test a mock tool.*

Update `registry_test.go`:
```go
package tools

import (
	"testing"
	"github.com/purav/microgravity/internal/mcp"
)

func TestRegistryIsNative(t *testing.T) {
	Register(mcp.Tool{Name: "mock_tool"}, func(args map[string]interface{}) (string, error) { return "mock", nil })
	if !IsNative("mock_tool") {
		t.Errorf("mock_tool should be native")
	}
	if IsNative("unknown_tool") {
		t.Errorf("unknown_tool should not be native")
	}
}
```

- [ ] **Step 5: Run tests to verify pass**

Run: `go test ./internal/tools/...`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/tools/registry.go internal/tools/registry_test.go
git commit -m "feat(tools): add native tools registry"
```

---

### Task 2: Implement Built-In Tools

**Files:**
- Create: `internal/tools/builtins.go`

- [ ] **Step 1: Write the failing tests**

Create `internal/tools/builtins_test.go`:
```go
package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuiltins(t *testing.T) {
	// Test write_file
	tmp := filepath.Join(os.TempDir(), "test.txt")
	defer os.Remove(tmp)

	out, err := Execute("write_file", map[string]interface{}{"path": tmp, "content": "hello tools"})
	if err != nil {
		t.Fatal(err)
	}

	// Test read_file
	out, err = Execute("read_file", map[string]interface{}{"path": tmp})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "hello tools") {
		t.Errorf("expected hello tools, got %s", out)
	}

	// Test run_terminal_command
	out, err = Execute("run_terminal_command", map[string]interface{}{"command": "echo test_cmd"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "test_cmd") {
		t.Errorf("expected test_cmd, got %s", out)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/tools/...`
Expected: FAIL due to missing tool registrations.

- [ ] **Step 3: Write minimal implementation**

Create `internal/tools/builtins.go`:

```go
package tools

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"github.com/purav/microgravity/internal/mcp"
)

func init() {
	Register(mcp.Tool{
		Name:        "run_terminal_command",
		Description: "Run a terminal command on the host system",
		InputSchema: mcp.Schema{
			Type: "object",
			Properties: map[string]interface{}{
				"command": map[string]interface{}{"type": "string"},
			},
			Required: []string{"command"},
		},
	}, func(args map[string]interface{}) (string, error) {
		cmdStr, ok := args["command"].(string)
		if !ok {
			return "", fmt.Errorf("invalid command argument")
		}

		var cmd *exec.Cmd
		if runtime.GOOS == "windows" {
			cmd = exec.Command("cmd", "/c", cmdStr)
		} else {
			cmd = exec.Command("sh", "-c", cmdStr)
		}

		out, err := cmd.CombinedOutput()
		if err != nil {
			return string(out), fmt.Errorf("command failed: %v, output: %s", err, string(out))
		}
		return string(out), nil
	})

	Register(mcp.Tool{
		Name:        "read_file",
		Description: "Read contents of a file",
		InputSchema: mcp.Schema{
			Type: "object",
			Properties: map[string]interface{}{
				"path": map[string]interface{}{"type": "string"},
			},
			Required: []string{"path"},
		},
	}, func(args map[string]interface{}) (string, error) {
		path, ok := args["path"].(string)
		if !ok {
			return "", fmt.Errorf("invalid path argument")
		}

		content, err := os.ReadFile(path)
		if err != nil {
			return "", err
		}
		return string(content), nil
	})

	Register(mcp.Tool{
		Name:        "write_file",
		Description: "Write contents to a file",
		InputSchema: mcp.Schema{
			Type: "object",
			Properties: map[string]interface{}{
				"path":    map[string]interface{}{"type": "string"},
				"content": map[string]interface{}{"type": "string"},
			},
			Required: []string{"path", "content"},
		},
	}, func(args map[string]interface{}) (string, error) {
		path, ok := args["path"].(string)
		if !ok {
			return "", fmt.Errorf("invalid path argument")
		}
		content, ok := args["content"].(string)
		if !ok {
			return "", fmt.Errorf("invalid content argument")
		}

		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			return "", err
		}
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			return "", err
		}
		return fmt.Sprintf("Successfully wrote %d bytes to %s", len(content), path), nil
	})
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/tools/...`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/tools/builtins.go internal/tools/builtins_test.go
git commit -m "feat(tools): implement native builtins (run_terminal_command, read_file, write_file)"
```

---

### Task 3: Inject Native Tools in `run.go`

**Files:**
- Modify: `cmd/run.go`

- [ ] **Step 1: Append native tools to `availableTools`**

In `cmd/run.go`, around line 56, right after checking `mcpServer != ""`, import `"github.com/purav/microgravity/internal/tools"` and add:

```go
		// Inject native tools
		availableTools = append(availableTools, tools.GetNativeTools()...)
```

- [ ] **Step 2: Intercept Tool Execution**

Around line 144, update the execution block to route to native tools if applicable.

Modify from:
```go
			if len(matches) > 1 && mcpClient != nil {
				jsonBlock := matches[1]
				var req ToolCallRequest
				if err := json.Unmarshal([]byte(jsonBlock), &req); err == nil && req.ToolCall != "" {
					fmt.Printf("\n[Agent] Executing tool: %s\n", req.ToolCall)
					content, err := mcpClient.CallTool(req.ToolCall, req.Arguments)
```

To:
```go
			if len(matches) > 1 {
				jsonBlock := matches[1]
				var req ToolCallRequest
				if err := json.Unmarshal([]byte(jsonBlock), &req); err == nil && req.ToolCall != "" {
					
					var toolOutput string
					fmt.Printf("\n[Agent] Executing tool: %s\n", req.ToolCall)
					
					if tools.IsNative(req.ToolCall) {
						out, err := tools.Execute(req.ToolCall, req.Arguments)
						if err != nil {
							toolOutput = fmt.Sprintf("Error calling native tool: %v", err)
						} else {
							toolOutput = out
						}
					} else if mcpClient != nil {
						content, err := mcpClient.CallTool(req.ToolCall, req.Arguments)
						if err != nil {
							toolOutput = fmt.Sprintf("Error calling MCP tool: %v", err)
						} else {
							for _, c := range content {
								toolOutput += c.Text + "\n"
							}
						}
					} else {
						toolOutput = fmt.Sprintf("Error: Unknown tool %s and no MCP server configured.", req.ToolCall)
					}

					fmt.Println("---------------------------------------------------------")
					fmt.Printf("🛠️  Agent Executing Tool: %s\n", req.ToolCall)
					toolOutputStr, _ := renderer.Render(fmt.Sprintf("```json\n%s\n```", toolOutput))
					fmt.Print(toolOutputStr)

					convo.WriteString(fmt.Sprintf("\nTool Output:\n%s\nAgent: ", toolOutput))
					continue // Trigger LLM again
				}
			}
```

- [ ] **Step 3: Run project tests**

Run: `go test ./...`
Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add cmd/run.go
git commit -m "feat(cmd): integrate native tools in run command"
```

---

### Task 4: Inject Native Tools in `chat.go`

**Files:**
- Modify: `cmd/chat.go`

- [ ] **Step 1: Mirror the injection logic from `run.go` into `chat.go`**

In `cmd/chat.go`, import `"github.com/purav/microgravity/internal/tools"`.
Append native tools to the `availableTools` slice just like in Task 3.

- [ ] **Step 2: Mirror the Tool Execution block**

Update the execution block in `cmd/chat.go` inside the interactive loop to match the `tools.IsNative` vs `mcpClient` logic introduced in Task 3.

- [ ] **Step 3: Run project tests**

Run: `go test ./...`
Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add cmd/chat.go
git commit -m "feat(cmd): integrate native tools in chat command"
```

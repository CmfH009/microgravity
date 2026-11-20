# Gemma CLI Native Tools Design

## Purpose
The current `microgravity` relies completely on an external Model Context Protocol (MCP) server for tool execution. This design introduces native built-in tools (`run_terminal_command`, `read_file`, `write_file`) that execute directly within the Go binary, providing faster execution and less reliance on external dependencies.

## Architecture & Integration Approach
We will use a **Native Tool Registry**.
- **`internal/tools` Package:** A new package responsible for defining, registering, and executing native tools.
- **Registry:** Maps tool names to their respective execution functions.
- **MCP Compatibility:** Native tools will share the same JSON schema structure (`mcp.Tool`) so they can seamlessly integrate into the system prompt builder alongside MCP tools.

## Detailed Component Design

### 1. `internal/tools/registry.go`
Defines the registry and execution logic:
- `var registry map[string]ToolHandler`
- `func GetNativeTools() []mcp.Tool`: Returns the schemas for `run_terminal_command`, `read_file`, and `write_file`.
- `func IsNative(name string) bool`: Checks if a tool is native.
- `func Execute(name string, args map[string]interface{}) (string, error)`: Unmarshals args and routes to the correct handler.

### 2. `internal/tools/builtins.go`
Implements the handlers for the specific tools:
- **`run_terminal_command`**: 
  - **Schema:** Requires `command` (string).
  - **Logic:** Detects OS. On Windows, uses `exec.Command("cmd", "/c", cmdStr)`. On unix-like systems, uses `exec.Command("sh", "-c", cmdStr)`. Returns stdout/stderr combined.
- **`read_file`**: 
  - **Schema:** Requires `path` (string).
  - **Logic:** Validates path and returns string contents using `os.ReadFile`.
- **`write_file`**: 
  - **Schema:** Requires `path` (string) and `content` (string).
  - **Logic:** Writes string contents to the file using `os.WriteFile` (with permissions `0644`). Creates intermediate directories if needed using `os.MkdirAll`.

### 3. Integration (`cmd/run.go` & `cmd/chat.go`)
- **Initialization:** 
  `availableTools = append(availableTools, tools.GetNativeTools()...)`
- **Execution Loop:**
  ```go
  if tools.IsNative(req.ToolCall) {
      // Execute via the native registry
      output, err = tools.Execute(req.ToolCall, req.Arguments)
  } else if mcpClient != nil {
      // Execute via MCP
      output, err = mcpClient.CallTool(req.ToolCall, req.Arguments)
  } else {
      err = fmt.Errorf("Unknown tool: %s", req.ToolCall)
  }
  ```

## Error Handling & Trade-offs
- File operations will return explicit file not found or permission denied errors.
- Terminal commands might hang. We should consider a context with timeout for `run_terminal_command` (e.g., 30s timeout) to prevent the LLM from getting stuck forever.
- No direct user confirmation for native tool execution is added right now; we assume the LLM acts autonomously in `run` and `chat`.

## Testing
- Ensure standard unit tests for `read_file` and `write_file`.
- Mock out `run_terminal_command` behavior if necessary or use basic `echo` commands to test execution on target platforms.

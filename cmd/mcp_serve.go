package cmd

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/CmfH009/microgravity/internal/events"
	"github.com/CmfH009/microgravity/internal/mcp"
	"github.com/CmfH009/microgravity/internal/policy"
	"github.com/CmfH009/microgravity/internal/scheduler"
	"github.com/CmfH009/microgravity/internal/subagent"
	"github.com/CmfH009/microgravity/internal/tools"
	"github.com/CmfH009/microgravity/internal/web"
	"github.com/spf13/cobra"
)

var mcpServeCmd = &cobra.Command{
	Use:   "serve",
	Short: "Run Microgravity as an MCP server (stdio JSON-RPC)",
	Long: `Run Microgravity as an MCP server over stdio using JSON-RPC.

This exposes Microgravity's native tools (view_file, write_to_file, replace_file_content,
grep_search, find_by_name, list_dir, run_command, manage_task, invoke_subagent,
schedule, search_web, read_url_content, and more) as MCP tools that other agents,
IDEs, and scripts can connect to.

Usage:
  mg mcp serve

The server reads JSON-RPC requests from stdin and writes responses to stdout.
Supported methods: initialize, tools/list, tools/call.`,
	Run: func(cmd *cobra.Command, args []string) {
		runMCPServe()
	},
}

func runMCPServe() {
	cwd, err := os.Getwd()
	if err != nil {
		cwd = "."
	}

	// Dependency infrastructure mirroring BuildEngineSession (minus full engine session)
	eb := events.NewEventBus()
	sched := scheduler.New()
	sched.SetEventBus(eb)
	notifyChan := make(chan string, 16)
	go func() {
		for range notifyChan {
			// drain
		}
	}()
	subMgr := subagent.GetManager()
	subMgr.SetEventBus(eb)

	roots := []string{cwd, os.TempDir()}
	pol := policy.New(roots, true, false)

	// Build registry with all native builtins
	registry := tools.NewRegistry()
	tools.RegisterBuiltinsWithPolicy(registry, eb, sched, notifyChan, nil, pol)
	tools.RegisterMCPResourceTools(registry, map[string]*mcp.Client{})
	web.RegisterWebTools(registry)

	fmt.Fprintf(os.Stderr, "[mcp] Microgravity MCP server ready on stdio (%d tools)\n", len(registry.GetNativeTools()))

	serveStdio(registry)
}

// serveStdio reads JSON-RPC requests from stdin (one per line) and writes
// responses to stdout (one per line).
func serveStdio(registry *tools.Registry) {
	var writeMu sync.Mutex
	reader := bufio.NewReader(os.Stdin)
	writer := bufio.NewWriter(os.Stdout)
	defer writer.Flush()

	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		var req mcp.Request
		if err := json.Unmarshal([]byte(line), &req); err != nil {
			resp := &mcp.Response{
				JSONRPC: "2.0",
				ID:      0,
				Error:   &mcp.Error{Code: -32700, Message: "Parse error"},
			}
			writeResponse(writer, &writeMu, resp)
			continue
		}

		resp := handleRequest(&req, registry)
		writeResponse(writer, &writeMu, resp)
	}
}

// handleRequest dispatches a JSON-RPC request to the appropriate MCP handler.
func handleRequest(req *mcp.Request, registry *tools.Registry) *mcp.Response {
	resp := &mcp.Response{
		JSONRPC: "2.0",
		ID:      req.ID,
	}

	switch req.Method {
	case "initialize":
		result := map[string]interface{}{
			"protocolVersion": "2024-11-05",
			"capabilities": map[string]interface{}{
				"tools": map[string]interface{}{},
			},
			"serverInfo": map[string]interface{}{
				"name":    "microgravity",
				"version": "1.1.0",
			},
		}
		data, _ := json.Marshal(result)
		resp.Result = data

	case "tools/list":
		toolsList := registry.GetNativeTools()
		result := mcp.ListToolsResult{Tools: toolsList}
		data, _ := json.Marshal(result)
		resp.Result = data

	case "tools/call":
		paramsMap, ok := req.Params.(map[string]interface{})
		if !ok {
			resp.Error = &mcp.Error{Code: -32602, Message: "Invalid params"}
			return resp
		}
		name, _ := paramsMap["name"].(string)
		args, _ := paramsMap["arguments"].(map[string]interface{})
		if args == nil {
			args = map[string]interface{}{}
		}
		if name == "" {
			resp.Error = &mcp.Error{Code: -32602, Message: "Tool name is required"}
			return resp
		}

		result, err := registry.Execute(context.Background(), name, args)
		if err != nil {
			resp.Error = &mcp.Error{Code: -32603, Message: err.Error()}
			return resp
		}
		callResult := mcp.CallToolResult{
			Content: []mcp.Content{{Type: "text", Text: result}},
		}
		data, _ := json.Marshal(callResult)
		resp.Result = data

	default:
		resp.Error = &mcp.Error{Code: -32601, Message: fmt.Sprintf("Method not found: %s", req.Method)}
	}

	return resp
}

func writeResponse(writer *bufio.Writer, mu *sync.Mutex, resp *mcp.Response) {
	mu.Lock()
	defer mu.Unlock()
	data, _ := json.Marshal(resp)
	writer.Write(data)
	writer.WriteByte('\n')
	writer.Flush()
}

func init() {
	mcpCmd.AddCommand(mcpServeCmd)
}

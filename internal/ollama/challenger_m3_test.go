package ollama

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/CmfH009/microgravity/internal/mcp"
)

// TestAdversarial_OllamaSchemaMapping stresses MCPToOllamaTools with edge-case
// schema definitions: empty tools, empty input schema, missing type, missing properties.
func TestAdversarial_OllamaSchemaMapping(t *testing.T) {
	testCases := []struct {
		name     string
		tools    []mcp.Tool
		validate func(t *testing.T, res []OllamaTool)
	}{
		{
			name:  "Empty tools slice",
			tools: []mcp.Tool{},
			validate: func(t *testing.T, res []OllamaTool) {
				if len(res) != 0 {
					t.Errorf("expected 0 tools, got %d", len(res))
				}
			},
		},
		{
			name: "Tool with empty InputSchema",
			tools: []mcp.Tool{
				{
					Name:        "empty_schema_tool",
					Description: "No schema fields provided",
					InputSchema: mcp.Schema{}, // Type: "", Properties: nil
				},
			},
			validate: func(t *testing.T, res []OllamaTool) {
				if len(res) != 1 {
					t.Fatalf("expected 1 tool, got %d", len(res))
				}
				fn := res[0].Function
				if res[0].Type != "function" {
					t.Errorf("expected type 'function', got %q", res[0].Type)
				}
				schema, ok := fn.Parameters.(mcp.Schema)
				if !ok {
					t.Fatalf("expected parameters to be mcp.Schema, got %T", fn.Parameters)
				}
				if schema.Type != "object" {
					t.Errorf("expected schema.Type to default to 'object', got %q", schema.Type)
				}
				if schema.Properties == nil {
					t.Errorf("expected schema.Properties to be initialized, got nil")
				}
			},
		},
		{
			name: "Tool with array without items",
			tools: []mcp.Tool{
				{
					Name: "array_tool",
					InputSchema: mcp.Schema{
						Type: "object",
						Properties: map[string]interface{}{
							"items_list": map[string]interface{}{"type": "array"},
						},
					},
				},
			},
			validate: func(t *testing.T, res []OllamaTool) {
				if len(res) != 1 {
					t.Fatalf("expected 1 tool, got %d", len(res))
				}
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("PANIC during MCPToOllamaTools: %v", r)
				}
			}()
			res := MCPToOllamaTools(tc.tools)
			tc.validate(t, res)
		})
	}
}

// TestAdversarial_OllamaFragmentedStreaming stresses Client.GenerateWithTools
// under ndjson streaming when tool call arguments arrive across fragmented chunks.
func TestAdversarial_OllamaFragmentedStreaming(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-ndjson")
		// Chunk 1: Function name arrives alone
		w.Write([]byte(`{"message": {"role": "assistant", "tool_calls": [{"function": {"name": "run_command"}}]}, "done": false}` + "\n"))
		// Chunk 2: Arguments arrive in subsequent chunk without name
		w.Write([]byte(`{"message": {"role": "assistant", "tool_calls": [{"function": {"arguments": {"CommandLine": "ls -la"}}}]}, "done": true}` + "\n"))
	}))
	defer server.Close()

	client := NewClient(server.URL)
	tools := []mcp.Tool{
		{
			Name:        "run_command",
			Description: "Run shell command",
			InputSchema: mcp.Schema{Type: "object"},
		},
	}

	var out bytes.Buffer
	res, err := client.GenerateWithTools(context.Background(), "gemma4:e2b", "list files", nil, tools, &out)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(res.ToolCalls) != 1 {
		t.Fatalf("expected exactly 1 tool call, got %d: %+v", len(res.ToolCalls), res.ToolCalls)
	}

	if res.ToolCalls[0].ToolCall != "run_command" {
		t.Errorf("expected tool_call 'run_command', got %q", res.ToolCalls[0].ToolCall)
	}

	if res.ToolCalls[0].Arguments["CommandLine"] != "ls -la" {
		t.Errorf("expected CommandLine 'ls -la', got %v (all args: %+v)", res.ToolCalls[0].Arguments["CommandLine"], res.ToolCalls[0].Arguments)
	}
}

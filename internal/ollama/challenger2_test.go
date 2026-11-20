package ollama

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/purav/microgravity/internal/mcp"
)

// TestChallenger2_OllamaSchema_EdgeCases stresses MCPToOllamaTools with
// boundary schema conditions: empty schemas, nil properties, nested arrays,
// empty descriptions, and special character names.
func TestChallenger2_OllamaSchema_EdgeCases(t *testing.T) {
	testCases := []struct {
		name     string
		tools    []mcp.Tool
		validate func(t *testing.T, res []OllamaTool)
	}{
		{
			name: "Completely empty input schema",
			tools: []mcp.Tool{
				{
					Name:        "empty_schema",
					Description: "",
					InputSchema: mcp.Schema{},
				},
			},
			validate: func(t *testing.T, res []OllamaTool) {
				if len(res) != 1 {
					t.Fatalf("expected 1 tool, got %d", len(res))
				}
				fn := res[0].Function
				schema, ok := fn.Parameters.(mcp.Schema)
				if !ok {
					t.Fatalf("expected mcp.Schema parameters, got %T", fn.Parameters)
				}
				if schema.Type != "object" {
					t.Errorf("expected type 'object', got %q", schema.Type)
				}
				if schema.Properties == nil {
					t.Errorf("expected non-nil Properties")
				}
			},
		},
		{
			name: "Multiple tools with nil properties and empty descriptions",
			tools: []mcp.Tool{
				{
					Name:        "tool1",
					Description: "",
					InputSchema: mcp.Schema{Type: "", Properties: nil},
				},
				{
					Name:        "tool2",
					Description: "Tool 2",
					InputSchema: mcp.Schema{Type: "object", Properties: nil},
				},
			},
			validate: func(t *testing.T, res []OllamaTool) {
				if len(res) != 2 {
					t.Fatalf("expected 2 tools, got %d", len(res))
				}
				for i, r := range res {
					s, ok := r.Function.Parameters.(mcp.Schema)
					if !ok || s.Properties == nil || s.Type != "object" {
						t.Errorf("tool %d improperly initialized: %+v", i, s)
					}
				}
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("CRITICAL PANIC during MCPToOllamaTools for %q: %v", tc.name, r)
				}
			}()
			res := MCPToOllamaTools(tc.tools)
			tc.validate(t, res)
		})
	}
}

// TestChallenger2_OllamaStreaming_MultiChunkAccumulation stresses
// argument accumulation across 6 fragmented ndjson streaming chunks.
func TestChallenger2_OllamaStreaming_MultiChunkAccumulation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-ndjson")
		// Chunk 1: Function name arrives alone
		w.Write([]byte(`{"message": {"role": "assistant", "tool_calls": [{"function": {"name": "run_command"}}]}, "done": false}` + "\n"))
		// Chunk 2: CommandLine argument (no function name in chunk)
		w.Write([]byte(`{"message": {"role": "assistant", "tool_calls": [{"function": {"arguments": {"CommandLine": "go test -v -count=1 ./..."}}}]}, "done": false}` + "\n"))
		// Chunk 3: Cwd argument
		w.Write([]byte(`{"message": {"role": "assistant", "tool_calls": [{"function": {"arguments": {"Cwd": "A:\\projects\\microgravity"}}}]}, "done": false}` + "\n"))
		// Chunk 4: WaitMsBeforeAsync argument
		w.Write([]byte(`{"message": {"role": "assistant", "tool_calls": [{"function": {"arguments": {"WaitMsBeforeAsync": 10000}}}]}, "done": false}` + "\n"))
		// Chunk 5: IsDaemon argument
		w.Write([]byte(`{"message": {"role": "assistant", "tool_calls": [{"function": {"arguments": {"IsDaemon": false}}}]}, "done": false}` + "\n"))
		// Chunk 6: Text content with done = true
		w.Write([]byte(`{"message": {"role": "assistant", "content": "Running tests now..."}, "done": true}` + "\n"))
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
	res, err := client.GenerateWithTools(context.Background(), "gemma4:e2b", "run test command", nil, tools, &out)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(res.ToolCalls) != 1 {
		t.Fatalf("expected exactly 1 tool call, got %d: %+v", len(res.ToolCalls), res.ToolCalls)
	}

	tc := res.ToolCalls[0]
	if tc.ToolCall != "run_command" {
		t.Errorf("expected 'run_command', got %q", tc.ToolCall)
	}

	expectedArgs := map[string]interface{}{
		"CommandLine":       "go test -v -count=1 ./...",
		"Cwd":               "A:\\projects\\microgravity",
		"WaitMsBeforeAsync": float64(10000),
		"IsDaemon":          false,
	}

	for k, v := range expectedArgs {
		if tc.Arguments[k] != v {
			t.Errorf("argument %q mismatch: expected %v, got %v (all args: %+v)", k, v, tc.Arguments[k], tc.Arguments)
		}
	}
}

// TestChallenger2_OllamaStreaming_SequentialToolCalls verifies that distinct
// tool calls in an ndjson stream are preserved as separate ToolCalls.
func TestChallenger2_OllamaStreaming_SequentialToolCalls(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-ndjson")
		// Tool 1: view_file across 2 chunks
		w.Write([]byte(`{"message": {"role": "assistant", "tool_calls": [{"function": {"name": "view_file"}}]}, "done": false}` + "\n"))
		w.Write([]byte(`{"message": {"role": "assistant", "tool_calls": [{"function": {"arguments": {"AbsolutePath": "main.go"}}}]}, "done": false}` + "\n"))

		// Tool 2: run_command in 1 chunk
		w.Write([]byte(`{"message": {"role": "assistant", "tool_calls": [{"function": {"name": "run_command", "arguments": {"CommandLine": "ls"}}}]}, "done": false}` + "\n"))

		// Tool 3: write_to_file in 1 chunk
		w.Write([]byte(`{"message": {"role": "assistant", "tool_calls": [{"function": {"name": "write_to_file", "arguments": {"TargetFile": "out.txt"}}}]}, "done": true}` + "\n"))
	}))
	defer server.Close()

	client := NewClient(server.URL)
	tools := []mcp.Tool{
		{Name: "view_file", InputSchema: mcp.Schema{Type: "object"}},
		{Name: "run_command", InputSchema: mcp.Schema{Type: "object"}},
		{Name: "write_to_file", InputSchema: mcp.Schema{Type: "object"}},
	}

	var out bytes.Buffer
	res, err := client.GenerateWithTools(context.Background(), "gemma4:e2b", "run multi", nil, tools, &out)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(res.ToolCalls) != 3 {
		t.Fatalf("expected 3 tool calls, got %d: %+v", len(res.ToolCalls), res.ToolCalls)
	}

	if res.ToolCalls[0].ToolCall != "view_file" || res.ToolCalls[0].Arguments["AbsolutePath"] != "main.go" {
		t.Errorf("call 0 mismatch: %+v", res.ToolCalls[0])
	}
	if res.ToolCalls[1].ToolCall != "run_command" || res.ToolCalls[1].Arguments["CommandLine"] != "ls" {
		t.Errorf("call 1 mismatch: %+v", res.ToolCalls[1])
	}
	if res.ToolCalls[2].ToolCall != "write_to_file" || res.ToolCalls[2].Arguments["TargetFile"] != "out.txt" {
		t.Errorf("call 2 mismatch: %+v", res.ToolCalls[2])
	}
}

// TestChallenger2_OllamaStreaming_RepeatedNameCollision verifies that when the
// model calls the same tool twice with different argument values, it produces
// two distinct tool calls instead of overwriting.
func TestChallenger2_OllamaStreaming_RepeatedNameCollision(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-ndjson")
		// First read_file call
		w.Write([]byte(`{"message": {"role": "assistant", "tool_calls": [{"function": {"name": "read_file", "arguments": {"path": "fileA.txt"}}}]}, "done": false}` + "\n"))
		// Second read_file call with conflicting path
		w.Write([]byte(`{"message": {"role": "assistant", "tool_calls": [{"function": {"name": "read_file", "arguments": {"path": "fileB.txt"}}}]}, "done": true}` + "\n"))
	}))
	defer server.Close()

	client := NewClient(server.URL)
	tools := []mcp.Tool{{Name: "read_file", InputSchema: mcp.Schema{Type: "object"}}}

	var out bytes.Buffer
	res, err := client.GenerateWithTools(context.Background(), "gemma4:e2b", "read two files", nil, tools, &out)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(res.ToolCalls) != 2 {
		t.Fatalf("expected 2 distinct tool calls due to argument collision, got %d: %+v", len(res.ToolCalls), res.ToolCalls)
	}

	if res.ToolCalls[0].Arguments["path"] != "fileA.txt" {
		t.Errorf("call 0 path mismatch: %v", res.ToolCalls[0].Arguments["path"])
	}
	if res.ToolCalls[1].Arguments["path"] != "fileB.txt" {
		t.Errorf("call 1 path mismatch: %v", res.ToolCalls[1].Arguments["path"])
	}
}

// TestChallenger2_OllamaStreaming_NilAndEmptyArguments verifies handling of chunks
// with nil or empty arguments map without panicking.
func TestChallenger2_OllamaStreaming_NilAndEmptyArguments(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-ndjson")
		w.Write([]byte(`{"message": {"role": "assistant", "tool_calls": [{"function": {"name": "get_status", "arguments": null}}]}, "done": false}` + "\n"))
		w.Write([]byte(`{"message": {"role": "assistant", "tool_calls": [{"function": {"arguments": {}}}]}, "done": false}` + "\n"))
		w.Write([]byte(`{"message": {"role": "assistant", "tool_calls": [{"function": {"arguments": {"active": true}}}]}, "done": true}` + "\n"))
	}))
	defer server.Close()

	client := NewClient(server.URL)
	tools := []mcp.Tool{{Name: "get_status", InputSchema: mcp.Schema{Type: "object"}}}

	var out bytes.Buffer
	res, err := client.GenerateWithTools(context.Background(), "gemma4:e2b", "get status", nil, tools, &out)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(res.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d: %+v", len(res.ToolCalls), res.ToolCalls)
	}
	if res.ToolCalls[0].Arguments["active"] != true {
		t.Errorf("expected active=true, got %v", res.ToolCalls[0].Arguments["active"])
	}
}

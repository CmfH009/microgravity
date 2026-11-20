package ollama

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/CmfH009/microgravity/internal/mcp"
)

func TestClient_Generate(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-ndjson")
		w.Write([]byte(`{"message": {"role": "assistant", "content": "Hello "}, "done": false}` + "\n"))
		w.Write([]byte(`{"message": {"role": "assistant", "content": "from Gemma!"}, "done": true}` + "\n"))
	}))
	defer server.Close()

	client := NewClient(server.URL)
	var out bytes.Buffer
	response, err := client.Generate(context.Background(), "gemma2", "Hello", nil, &out)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := "Hello from Gemma!"
	if response != expected {
		t.Errorf("expected %q, got %q", expected, response)
	}
	if out.String() != expected+"\n" {
		t.Errorf("expected out %q, got %q", expected+"\n", out.String())
	}
}

func TestClient_Pull(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := NewClient(server.URL)
	err := client.Pull(context.Background(), "gemma2", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestMCPToOllamaTools(t *testing.T) {
	tools := []mcp.Tool{
		{
			Name:        "run_command",
			Description: "Run shell command",
			InputSchema: mcp.Schema{
				Type: "object",
				Properties: map[string]interface{}{
					"CommandLine": map[string]interface{}{"type": "string"},
				},
				Required: []string{"CommandLine"},
			},
		},
	}

	ollamaTools := MCPToOllamaTools(tools)
	if len(ollamaTools) != 1 {
		t.Fatalf("expected 1 OllamaTool, got %d", len(ollamaTools))
	}
	if ollamaTools[0].Type != "function" {
		t.Errorf("expected type function, got %s", ollamaTools[0].Type)
	}
	if ollamaTools[0].Function.Name != "run_command" {
		t.Errorf("expected name run_command, got %s", ollamaTools[0].Function.Name)
	}
}

func TestClient_GenerateWithTools_OllamaToolCall(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-ndjson")
		chunk1 := `{"message": {"role": "assistant", "content": "Running command..."}, "done": false}` + "\n"
		chunk2 := `{"message": {"role": "assistant", "content": "", "tool_calls": [{"function": {"name": "run_command", "arguments": {"CommandLine": "dir"}}}]}, "done": true}` + "\n"
		w.Write([]byte(chunk1))
		w.Write([]byte(chunk2))
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

	if res.Text != "Running command..." {
		t.Errorf("expected text 'Running command...', got %q", res.Text)
	}
	if len(res.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(res.ToolCalls))
	}
	if res.ToolCalls[0].ToolCall != "run_command" {
		t.Errorf("expected tool_call 'run_command', got %q", res.ToolCalls[0].ToolCall)
	}
	if res.ToolCalls[0].Arguments["CommandLine"] != "dir" {
		t.Errorf("expected CommandLine 'dir', got %v", res.ToolCalls[0].Arguments["CommandLine"])
	}
}


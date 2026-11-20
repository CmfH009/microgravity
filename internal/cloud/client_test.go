package cloud

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/CmfH009/microgravity/internal/authutil"
	"github.com/CmfH009/microgravity/internal/config"
	"github.com/CmfH009/microgravity/internal/mcp"
)

func TestClient_Generate(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Write([]byte(`data: {"candidates": [{"content": {"parts": [{"text": "Hello "}]}}]}` + "\n\n"))
		w.Write([]byte(`data: {"candidates": [{"content": {"parts": [{"text": "Cloud!"}]}}]}` + "\n\n"))
	}))
	defer server.Close()

	client := NewClient()
	client.APIKey = "test-key"
	client.BaseURL = server.URL

	var out bytes.Buffer
	response, err := client.Generate(context.Background(), "gemma", "Hello", nil, &out)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := "Hello Cloud!"
	if response != expected {
		t.Errorf("expected %q, got %q", expected, response)
	}
}

func TestResolveCloudModel(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"gemini-3.8-flash", "gemini-3.8-flash"},
		{"gemini-3.7-flash", "gemini-3.7-flash"},
		{"gemini-3.1-pro", "gemini-3.1-pro-preview"},
		{"gemini-3.6-flash", "gemini-3.6-flash"},
		{"gemini-3.5-flash-lite", "gemini-2.0-flash-lite"},
		{"gemma-2-9b-it", "gemma-2-9b-it"},
		{"gemini-1.5-pro", "gemini-1.5-pro"},
		{"gemini-1.5-flash", "gemini-1.5-flash"},
		{"gemini-2.0-flash", "gemini-2.0-flash"},
		{"pro", "gemini-3.1-pro-preview"},
		{"flash", "gemini-3.8-flash"},
		{"flash-lite", "gemini-2.0-flash-lite"},
		{"thinking", "gemini-2.0-flash-thinking-exp-01-21"},
	}

	for _, tt := range tests {
		resolved := ResolveCloudModel(tt.input)
		if resolved != tt.expected {
			t.Errorf("ResolveCloudModel(%q) = %q; expected %q", tt.input, resolved, tt.expected)
		}
	}
}


func TestClient_Generate_404FallbackChain(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts < 3 {
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte(`{"error": {"code": 404, "message": "model not found"}}`))
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Write([]byte(`data: {"candidates": [{"content": {"parts": [{"text": "Fallback chain success!"}]}}]}` + "\n\n"))
	}))
	defer server.Close()

	client := NewClient()
	client.APIKey = "test-key"
	client.BaseURL = server.URL

	var out bytes.Buffer
	resp, err := client.Generate(context.Background(), "invalid-model-name", "Test prompt", nil, &out)
	if err != nil {
		t.Fatalf("Expected fallback chain to succeed, got error: %v", err)
	}

	if resp != "Fallback chain success!" {
		t.Errorf("Expected 'Fallback chain success!', got %q", resp)
	}

	if attempts != 3 {
		t.Errorf("Expected 3 HTTP attempts in fallback chain, got %d", attempts)
	}
}


func TestClient_Generate_TransientRetry(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts == 1 {
			w.Header().Set("Connection", "close")
			w.WriteHeader(http.StatusServiceUnavailable) // 503
			return
		}
		w.Header().Set("Connection", "close")
		w.Header().Set("Content-Type", "text/event-stream")
		w.Write([]byte(`data: {"candidates": [{"content": {"parts": [{"text": "Recovered after 503!"}]}}]}` + "\n\n"))
	}))
	defer server.Close()

	client := NewClient()
	client.APIKey = "test-key"
	client.BaseURL = server.URL

	var out bytes.Buffer
	resp, err := client.Generate(context.Background(), "gemini-2.5-flash", "Test prompt", nil, &out)
	if err != nil {
		t.Fatalf("Expected retry to succeed after transient 503, got error: %v", err)
	}

	if resp != "Recovered after 503!" {
		t.Errorf("Expected 'Recovered after 503!', got %q", resp)
	}

	if attempts < 2 {
		t.Errorf("Expected at least 2 attempts (initial 503 + retry), got %d", attempts)
	}
}

func TestClient_Generate_429QuotaFallbackChain(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts < 3 {
			w.WriteHeader(http.StatusTooManyRequests) // 429 Quota Exceeded
			w.Write([]byte(`{"error": {"code": 429, "message": "Quota exceeded for model"}}`))
			return
		}
		w.Header().Set("Connection", "close")
		w.Header().Set("Content-Type", "text/event-stream")
		w.Write([]byte(`data: {"candidates": [{"content": {"parts": [{"text": "Succeeded on fallback model after 429!"}]}}]}` + "\n\n"))
	}))
	defer server.Close()

	client := NewClient()
	client.APIKey = "test-key"
	client.BaseURL = server.URL

	var out bytes.Buffer
	resp, err := client.Generate(context.Background(), "gemini-2.0-flash", "What is the time?", nil, &out)
	if err != nil {
		t.Fatalf("Expected 429 fallback chain to succeed, got error: %v", err)
	}

	if resp != "Succeeded on fallback model after 429!" {
		t.Errorf("Expected fallback success, got %q", resp)
	}

	if attempts != 3 {
		t.Errorf("Expected 3 attempts across candidate models after 429 errors, got %d", attempts)
	}
}

func TestClient_Generate_VertexEndpoints(t *testing.T) {
	// Temporarily set vertex mode in config
	tempHome := t.TempDir()
	os.Setenv("USERPROFILE", tempHome)
	os.Setenv("HOME", tempHome)
	config.SetVertexConfig("vertex-express", "", "us-central1", "vertex-express-test-key")

	endpoint := authutil.BuildVertexEndpoint("express", "us-central1", "", "gemini-2.5-flash")
	if endpoint != "https://aiplatform.googleapis.com/v1beta/publishers/google/models/gemini-2.5-flash:streamGenerateContent?alt=sse" {
		t.Errorf("unexpected express endpoint: %s", endpoint)
	}

	// Test endpoint formatting for standard mode
	stdEndpoint := authutil.BuildVertexEndpoint("standard", "europe-west4", "my-project-123", "gemini-2.5-pro")
	expectedStd := "https://europe-west4-aiplatform.googleapis.com/v1/projects/my-project-123/locations/europe-west4/publishers/google/models/gemini-2.5-pro:streamGenerateContent?alt=sse"
	if stdEndpoint != expectedStd {
		t.Errorf("unexpected standard endpoint: got %s, want %s", stdEndpoint, expectedStd)
	}
}

func TestMCPToGeminiTools(t *testing.T) {
	tools := []mcp.Tool{
		{
			Name:        "write_to_file",
			Description: "Write content to a file",
			InputSchema: mcp.Schema{
				Type: "object",
				Properties: map[string]interface{}{
					"TargetFile": map[string]interface{}{
						"type":        "string",
						"description": "Path to file",
					},
					"Overwrite": map[string]interface{}{
						"type":        "boolean",
						"description": "Whether to overwrite",
					},
					"Tags": map[string]interface{}{
						"type":        "array",
						"description": "Tags list without items defined",
					},
				},
				Required: []string{"TargetFile"},
			},
		},
	}

	geminiTools := MCPToGeminiTools(tools)
	if len(geminiTools) != 1 {
		t.Fatalf("expected 1 GeminiTool, got %d", len(geminiTools))
	}
	if len(geminiTools[0].FunctionDeclarations) != 1 {
		t.Fatalf("expected 1 FunctionDeclaration, got %d", len(geminiTools[0].FunctionDeclarations))
	}

	decl := geminiTools[0].FunctionDeclarations[0]
	if decl.Name != "write_to_file" {
		t.Errorf("expected name write_to_file, got %s", decl.Name)
	}
	if decl.Parameters == nil || decl.Parameters.Type != "OBJECT" {
		t.Errorf("expected OBJECT parameters, got %v", decl.Parameters)
	}
	if len(decl.Parameters.Properties) != 3 {
		t.Errorf("expected 3 properties, got %d", len(decl.Parameters.Properties))
	}
	// Verify uppercase type mapping
	if decl.Parameters.Properties["TargetFile"].Type != "STRING" {
		t.Errorf("expected TargetFile type STRING, got %s", decl.Parameters.Properties["TargetFile"].Type)
	}
	if decl.Parameters.Properties["Overwrite"].Type != "BOOLEAN" {
		t.Errorf("expected Overwrite type BOOLEAN, got %s", decl.Parameters.Properties["Overwrite"].Type)
	}
	// Verify default items on ARRAY
	tagsProp := decl.Parameters.Properties["Tags"]
	if tagsProp.Type != "ARRAY" {
		t.Errorf("expected Tags type ARRAY, got %s", tagsProp.Type)
	}
	if tagsProp.Items == nil || tagsProp.Items.Type != "STRING" {
		t.Errorf("expected Tags Items to default to STRING, got %v", tagsProp.Items)
	}
}

func TestClient_GenerateWithTools_NativeFunctionCall(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		// SSE stream emitting thought text first, then a function call
		w.Write([]byte(`data: {"candidates": [{"content": {"parts": [{"text": "I will create the file."}]}}]}` + "\n\n"))
		w.Write([]byte(`data: {"candidates": [{"content": {"parts": [{"functionCall": {"name": "write_to_file", "args": {"TargetFile": "test.txt", "CodeContent": "hello"}}}]}}]}` + "\n\n"))
	}))
	defer server.Close()

	client := NewClient()
	client.APIKey = "test-key"
	client.BaseURL = server.URL

	tools := []mcp.Tool{
		{
			Name:        "write_to_file",
			Description: "Write content to a file",
			InputSchema: mcp.Schema{Type: "object"},
		},
	}

	var out bytes.Buffer
	result, err := client.GenerateWithTools(context.Background(), "gemini-3.8-flash", "Please create test.txt", nil, tools, &out)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Text != "I will create the file." {
		t.Errorf("expected text 'I will create the file.', got %q", result.Text)
	}
	if len(result.ToolCalls) != 1 {
		t.Fatalf("expected 1 ToolCall, got %d", len(result.ToolCalls))
	}
	if result.ToolCalls[0].ToolCall != "write_to_file" {
		t.Errorf("expected tool_call 'write_to_file', got %q", result.ToolCalls[0].ToolCall)
	}
	if result.ToolCalls[0].Arguments["TargetFile"] != "test.txt" {
		t.Errorf("expected TargetFile 'test.txt', got %v", result.ToolCalls[0].Arguments["TargetFile"])
	}
}






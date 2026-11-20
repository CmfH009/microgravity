package cloud

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/purav/microgravity/internal/mcp"
)

// TestAdversarial_SchemaMappingEdgeCases stresses MCPToGeminiTools with adversarial
// schema structures: arrays missing items, nested objects without properties,
// empty descriptions, empty required lists, and empty top-level type.
func TestAdversarial_SchemaMappingEdgeCases(t *testing.T) {
	validGeminiTypes := map[string]bool{
		"STRING":  true,
		"INTEGER": true,
		"NUMBER":  true,
		"BOOLEAN": true,
		"ARRAY":   true,
		"OBJECT":  true,
	}

	var validateSchema func(path string, s *GeminiSchema)
	validateSchema = func(path string, s *GeminiSchema) {
		if s == nil {
			t.Fatalf("nil schema encountered at %s", path)
		}
		if !validGeminiTypes[s.Type] {
			t.Errorf("invalid OpenAPI Gemini type at %s: got %q (must be one of STRING, INTEGER, NUMBER, BOOLEAN, ARRAY, OBJECT)", path, s.Type)
		}
		if s.Type == "ARRAY" {
			if s.Items == nil {
				t.Errorf("ARRAY at %s has nil Items; Gemini requires non-nil Items", path)
			} else {
				validateSchema(path+".items", s.Items)
			}
		}
		if s.Properties != nil {
			for k, prop := range s.Properties {
				validateSchema(fmt.Sprintf("%s.properties[%s]", path, k), prop)
			}
		}
	}

	testCases := []struct {
		name     string
		tools    []mcp.Tool
		validate func(t *testing.T, res []GeminiTool)
	}{
		{
			name:  "Empty tools slice",
			tools: []mcp.Tool{},
			validate: func(t *testing.T, res []GeminiTool) {
				if res != nil {
					t.Errorf("expected nil result for empty tools, got %v", res)
				}
			},
		},
		{
			name: "Array schema without items (ask_question / tags)",
			tools: []mcp.Tool{
				{
					Name:        "ask_question",
					Description: "", // empty description
					InputSchema: mcp.Schema{
						Type: "object",
						Properties: map[string]interface{}{
							"options": map[string]interface{}{
								"type": "array",
								// items completely missing
							},
						},
						Required: []string{}, // empty required
					},
				},
			},
			validate: func(t *testing.T, res []GeminiTool) {
				if len(res) != 1 || len(res[0].FunctionDeclarations) != 1 {
					t.Fatalf("unexpected result shape: %v", res)
				}
				decl := res[0].FunctionDeclarations[0]
				validateSchema("ask_question.parameters", decl.Parameters)

				optProp := decl.Parameters.Properties["options"]
				if optProp == nil {
					t.Fatalf("missing 'options' property")
				}
				if optProp.Type != "ARRAY" {
					t.Errorf("expected ARRAY, got %s", optProp.Type)
				}
				if optProp.Items == nil || optProp.Items.Type != "STRING" {
					t.Errorf("expected Items to default to STRING, got %v", optProp.Items)
				}
			},
		},
		{
			name: "Nested objects and objects without properties",
			tools: []mcp.Tool{
				{
					Name:        "nested_tool",
					Description: "Tool with deep nesting and empty objects",
					InputSchema: mcp.Schema{
						Type: "object",
						Properties: map[string]interface{}{
							"empty_obj": map[string]interface{}{
								"type": "object",
								// properties missing
							},
							"deep_obj": map[string]interface{}{
								"type": "object",
								"properties": map[string]interface{}{
									"level2": map[string]interface{}{
										"type": "object",
										"properties": map[string]interface{}{
											"val": map[string]interface{}{
												"type": "int",
											},
										},
									},
								},
							},
						},
					},
				},
			},
			validate: func(t *testing.T, res []GeminiTool) {
				decl := res[0].FunctionDeclarations[0]
				validateSchema("nested_tool.parameters", decl.Parameters)

				emptyObj := decl.Parameters.Properties["empty_obj"]
				if emptyObj.Type != "OBJECT" {
					t.Errorf("expected OBJECT for empty_obj, got %s", emptyObj.Type)
				}

				deepObj := decl.Parameters.Properties["deep_obj"]
				if deepObj.Type != "OBJECT" {
					t.Errorf("expected OBJECT for deep_obj, got %s", deepObj.Type)
				}
				lvl2 := deepObj.Properties["level2"]
				if lvl2.Type != "OBJECT" {
					t.Errorf("expected OBJECT for level2, got %s", lvl2.Type)
				}
				val := lvl2.Properties["val"]
				if val.Type != "INTEGER" {
					t.Errorf("expected INTEGER for val, got %s", val.Type)
				}
			},
		},
		{
			name: "Missing top-level Type in InputSchema",
			tools: []mcp.Tool{
				{
					Name: "no_type_tool",
					InputSchema: mcp.Schema{
						// Type: "" (unspecified)
						Properties: map[string]interface{}{
							"flag": map[string]interface{}{"type": "bool"},
						},
					},
				},
			},
			validate: func(t *testing.T, res []GeminiTool) {
				decl := res[0].FunctionDeclarations[0]
				// Root parameters schema for Gemini FunctionDeclaration MUST be OBJECT
				if decl.Parameters.Type != "OBJECT" {
					t.Errorf("expected root parameters Type to default to OBJECT when unspecified, got %q", decl.Parameters.Type)
				}
				validateSchema("no_type_tool.parameters", decl.Parameters)
			},
		},
		{
			name: "Array of arrays with missing inner items",
			tools: []mcp.Tool{
				{
					Name: "matrix_tool",
					InputSchema: mcp.Schema{
						Type: "object",
						Properties: map[string]interface{}{
							"matrix": map[string]interface{}{
								"type": "array",
								"items": map[string]interface{}{
									"type": "array",
									// missing inner items
								},
							},
						},
					},
				},
			},
			validate: func(t *testing.T, res []GeminiTool) {
				decl := res[0].FunctionDeclarations[0]
				validateSchema("matrix_tool.parameters", decl.Parameters)

				matrix := decl.Parameters.Properties["matrix"]
				if matrix.Items == nil || matrix.Items.Type != "ARRAY" {
					t.Fatalf("expected matrix.items to be ARRAY, got %v", matrix.Items)
				}
				if matrix.Items.Items == nil || matrix.Items.Items.Type != "STRING" {
					t.Errorf("expected inner matrix.items.items to default to STRING, got %v", matrix.Items.Items)
				}
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("PANIC during MCPToGeminiTools: %v", r)
				}
			}()
			res := MCPToGeminiTools(tc.tools)
			tc.validate(t, res)
		})
	}
}

// TestAdversarial_FragmentedStreamingFunctionCall stresses Client.GenerateWithTools
// under SSE chunk fragmentation where function name arrives in chunk 1 and arguments
// are split across 5+ subsequent chunks.
func TestAdversarial_FragmentedStreamingFunctionCall(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		// Chunk 1: Function name arrives alone
		w.Write([]byte(`data: {"candidates": [{"content": {"parts": [{"functionCall": {"name": "write_to_file"}}]}}]}` + "\n\n"))
		// Chunk 2: TargetFile argument
		w.Write([]byte(`data: {"candidates": [{"content": {"parts": [{"functionCall": {"args": {"TargetFile": "app/main.go"}}}]}}]}` + "\n\n"))
		// Chunk 3: CodeContent argument
		w.Write([]byte(`data: {"candidates": [{"content": {"parts": [{"functionCall": {"args": {"CodeContent": "package main\n\nfunc main() {}\n"}}}]}}]}` + "\n\n"))
		// Chunk 4: Description argument
		w.Write([]byte(`data: {"candidates": [{"content": {"parts": [{"functionCall": {"args": {"Description": "entrypoint"}}}]}}]}` + "\n\n"))
		// Chunk 5: Overwrite argument
		w.Write([]byte(`data: {"candidates": [{"content": {"parts": [{"functionCall": {"args": {"Overwrite": true}}}]}}]}` + "\n\n"))
		// Chunk 6: AllowMultiple argument
		w.Write([]byte(`data: {"candidates": [{"content": {"parts": [{"functionCall": {"args": {"AllowMultiple": false}}}]}}]}` + "\n\n"))
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
	result, err := client.GenerateWithTools(context.Background(), "gemini-3.8-flash", "Create main.go", nil, tools, &out)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.ToolCalls) != 1 {
		t.Fatalf("expected exactly 1 assembled ToolCall, got %d tool calls: %+v", len(result.ToolCalls), result.ToolCalls)
	}

	tc := result.ToolCalls[0]
	if tc.ToolCall != "write_to_file" {
		t.Errorf("expected tool_call 'write_to_file', got %q", tc.ToolCall)
	}

	expectedArgs := map[string]interface{}{
		"TargetFile":    "app/main.go",
		"CodeContent":   "package main\n\nfunc main() {}\n",
		"Description":   "entrypoint",
		"Overwrite":     true,
		"AllowMultiple": false,
	}

	for k, v := range expectedArgs {
		if tc.Arguments[k] != v {
			t.Errorf("argument %q mismatch: expected %v, got %v (all args: %+v)", k, v, tc.Arguments[k], tc.Arguments)
		}
	}
}


// TestAdversarial_FragmentedStreaming_RepeatedName tests when SSE chunks repeat the
// function name while streaming fragmented arguments across chunks.
func TestAdversarial_FragmentedStreaming_RepeatedName(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Write([]byte(`data: {"candidates": [{"content": {"parts": [{"functionCall": {"name": "write_to_file", "args": {"TargetFile": "app/main.go"}}}]}}]}` + "\n\n"))
		w.Write([]byte(`data: {"candidates": [{"content": {"parts": [{"functionCall": {"name": "write_to_file", "args": {"CodeContent": "package main\n"}}}]}}]}` + "\n\n"))
		w.Write([]byte(`data: {"candidates": [{"content": {"parts": [{"functionCall": {"name": "write_to_file", "args": {"Description": "entrypoint"}}}]}}]}` + "\n\n"))
		w.Write([]byte(`data: {"candidates": [{"content": {"parts": [{"functionCall": {"name": "write_to_file", "args": {"Overwrite": true}}}]}}]}` + "\n\n"))
		w.Write([]byte(`data: {"candidates": [{"content": {"parts": [{"functionCall": {"name": "write_to_file", "args": {"AllowMultiple": false}}}]}}]}` + "\n\n"))
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
	result, err := client.GenerateWithTools(context.Background(), "gemini-3.8-flash", "Create main.go", nil, tools, &out)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.ToolCalls) != 1 {
		t.Fatalf("expected 1 assembled ToolCall, got %d tool calls: %+v", len(result.ToolCalls), result.ToolCalls)
	}
}

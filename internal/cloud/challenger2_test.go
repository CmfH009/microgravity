package cloud

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/CmfH009/microgravity/internal/mcp"
)

// TestChallenger2_SchemaMapping_AdversarialSuite performs exhaustive stress testing
// of schema mapping against edge cases:
// - Root schemas without type
// - Nested arrays without items (at multiple levels of depth)
// - Empty parameter descriptions
// - Malformed property maps (nil, non-map types)
// - Enums of varying and empty types
// - Verification of strict OpenAPI uppercase types and zero nil dereferences.
func TestChallenger2_SchemaMapping_AdversarialSuite(t *testing.T) {
	validGeminiTypes := map[string]bool{
		"STRING":  true,
		"INTEGER": true,
		"NUMBER":  true,
		"BOOLEAN": true,
		"ARRAY":   true,
		"OBJECT":  true,
	}

	var validateSchemaTree func(path string, s *GeminiSchema)
	validateSchemaTree = func(path string, s *GeminiSchema) {
		if s == nil {
			t.Fatalf("CRITICAL: nil *GeminiSchema encountered at path %s", path)
		}
		if !validGeminiTypes[s.Type] {
			t.Errorf("CRITICAL: invalid Gemini OpenAPI type at path %s: got %q (must be one of STRING, INTEGER, NUMBER, BOOLEAN, ARRAY, OBJECT)", path, s.Type)
		}
		if s.Type == "ARRAY" {
			if s.Items == nil {
				t.Fatalf("CRITICAL: ARRAY at %s has nil Items; Gemini API requires non-nil Items schema", path)
			}
			validateSchemaTree(path+".items", s.Items)
		}
		if s.Type == "OBJECT" {
			if s.Properties == nil {
				t.Errorf("WARNING: OBJECT at %s has nil Properties map", path)
			}
			for k, prop := range s.Properties {
				if prop == nil {
					t.Fatalf("CRITICAL: nil property schema found for key %q at %s", k, path)
				}
				validateSchemaTree(fmt.Sprintf("%s.properties[%s]", path, k), prop)
			}
		}
	}

	testCases := []struct {
		name     string
		tools    []mcp.Tool
		validate func(t *testing.T, res []GeminiTool)
	}{
		{
			name: "Root schema with empty string type and nil properties",
			tools: []mcp.Tool{
				{
					Name:        "empty_type_nil_props",
					Description: "",
					InputSchema: mcp.Schema{
						Type:       "",
						Properties: nil,
						Required:   nil,
					},
				},
			},
			validate: func(t *testing.T, res []GeminiTool) {
				if len(res) != 1 || len(res[0].FunctionDeclarations) != 1 {
					t.Fatalf("expected 1 function declaration, got %v", res)
				}
				decl := res[0].FunctionDeclarations[0]
				if decl.Parameters == nil {
					t.Fatalf("expected non-nil Parameters")
				}
				if decl.Parameters.Type != "OBJECT" {
					t.Errorf("expected root schema Type to be OBJECT, got %q", decl.Parameters.Type)
				}
				validateSchemaTree("decl.parameters", decl.Parameters)
			},
		},
		{
			name: "Case variations in root and property types",
			tools: []mcp.Tool{
				{
					Name:        "case_variations",
					Description: "Mixed case types",
					InputSchema: mcp.Schema{
						Type: "Object",
						Properties: map[string]interface{}{
							"str_field": map[string]interface{}{"type": "String", "description": ""},
							"int_field": map[string]interface{}{"type": "INT"},
							"num_field": map[string]interface{}{"type": "Number"},
							"flt_field": map[string]interface{}{"type": "Float"},
							"bln_field": map[string]interface{}{"type": "Bool"},
							"arr_field": map[string]interface{}{"type": "Array"},
						},
					},
				},
			},
			validate: func(t *testing.T, res []GeminiTool) {
				decl := res[0].FunctionDeclarations[0]
				validateSchemaTree("case_variations.parameters", decl.Parameters)

				props := decl.Parameters.Properties
				expected := map[string]string{
					"str_field": "STRING",
					"int_field": "INTEGER",
					"num_field": "NUMBER",
					"flt_field": "NUMBER",
					"bln_field": "BOOLEAN",
					"arr_field": "ARRAY",
				}
				for field, expectedType := range expected {
					p, ok := props[field]
					if !ok || p == nil {
						t.Fatalf("missing property %q", field)
					}
					if p.Type != expectedType {
						t.Errorf("field %q: expected %s, got %s", field, expectedType, p.Type)
					}
				}
			},
		},
		{
			name: "5-level deep nested arrays without items",
			tools: []mcp.Tool{
				{
					Name:        "deep_tensor",
					Description: "Deeply nested arrays missing items at leaf",
					InputSchema: mcp.Schema{
						Type: "object",
						Properties: map[string]interface{}{
							"tensor": map[string]interface{}{
								"type": "array",
								"items": map[string]interface{}{
									"type": "array",
									"items": map[string]interface{}{
										"type": "array",
										"items": map[string]interface{}{
											"type": "array",
											"items": map[string]interface{}{
												"type": "array",
												// leaf has no items
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
				validateSchemaTree("deep_tensor.parameters", decl.Parameters)

				curr := decl.Parameters.Properties["tensor"]
				for i := 1; i <= 5; i++ {
					if curr == nil || curr.Type != "ARRAY" {
						t.Fatalf("level %d: expected ARRAY, got %v", i, curr)
					}
					if curr.Items == nil {
						t.Fatalf("level %d: Items is nil", i)
					}
					curr = curr.Items
				}
				// Leaf fallback must be STRING
				if curr.Type != "STRING" {
					t.Errorf("expected leaf fallback to STRING, got %s", curr.Type)
				}
			},
		},
		{
			name: "Malformed properties (non-map property values and nil elements)",
			tools: []mcp.Tool{
				{
					Name:        "malformed_props",
					Description: "Properties with non-map elements",
					InputSchema: mcp.Schema{
						Type: "object",
						Properties: map[string]interface{}{
							"nil_prop":    nil,
							"string_prop": "not-a-map",
							"int_prop":    42,
							"valid_prop": map[string]interface{}{
								"type":        "string",
								"description": "",
							},
						},
					},
				},
			},
			validate: func(t *testing.T, res []GeminiTool) {
				decl := res[0].FunctionDeclarations[0]
				validateSchemaTree("malformed_props.parameters", decl.Parameters)

				// Non-map properties must be ignored without panic
				if _, ok := decl.Parameters.Properties["nil_prop"]; ok {
					t.Errorf("expected nil_prop to be omitted")
				}
				if _, ok := decl.Parameters.Properties["string_prop"]; ok {
					t.Errorf("expected string_prop to be omitted")
				}
				if decl.Parameters.Properties["valid_prop"] == nil {
					t.Errorf("expected valid_prop to be present")
				}
			},
		},
		{
			name: "Property with empty descriptions and various enum configurations",
			tools: []mcp.Tool{
				{
					Name:        "enum_variations",
					Description: "",
					InputSchema: mcp.Schema{
						Type: "object",
						Properties: map[string]interface{}{
							"string_enum": map[string]interface{}{
								"type": "string",
								"enum": []string{"optA", "optB"},
							},
							"interface_enum": map[string]interface{}{
								"type": "string",
								"enum": []interface{}{"alpha", "beta", "gamma"},
							},
							"empty_enum": map[string]interface{}{
								"type": "string",
								"enum": []string{},
							},
						},
					},
				},
			},
			validate: func(t *testing.T, res []GeminiTool) {
				decl := res[0].FunctionDeclarations[0]
				validateSchemaTree("enum_variations.parameters", decl.Parameters)

				strEnum := decl.Parameters.Properties["string_enum"]
				if len(strEnum.Enum) != 2 || strEnum.Enum[0] != "optA" {
					t.Errorf("string_enum mismatch: %v", strEnum.Enum)
				}
				ifaceEnum := decl.Parameters.Properties["interface_enum"]
				if len(ifaceEnum.Enum) != 3 || ifaceEnum.Enum[2] != "gamma" {
					t.Errorf("interface_enum mismatch: %v", ifaceEnum.Enum)
				}
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("CRITICAL PANIC during MCPToGeminiTools for %q: %v", tc.name, r)
				}
			}()
			res := MCPToGeminiTools(tc.tools)
			tc.validate(t, res)
		})
	}
}

// TestChallenger2_FragmentedSSE_SingleTokenParameterStream stresses Client.GenerateWithTools
// where every single argument arrives in its own separate SSE chunk.
func TestChallenger2_FragmentedSSE_SingleTokenParameterStream(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		// Chunk 1: Function name only
		w.Write([]byte(`data: {"candidates": [{"content": {"parts": [{"functionCall": {"name": "replace_file_content"}}]}}]}` + "\n\n"))
		// Chunk 2: TargetFile
		w.Write([]byte(`data: {"candidates": [{"content": {"parts": [{"functionCall": {"args": {"TargetFile": "A:/projects/microgravity/main.go"}}}]}}]}` + "\n\n"))
		// Chunk 3: Instruction
		w.Write([]byte(`data: {"candidates": [{"content": {"parts": [{"functionCall": {"args": {"Instruction": "Add health check endpoint"}}}]}}]}` + "\n\n"))
		// Chunk 4: Description
		w.Write([]byte(`data: {"candidates": [{"content": {"parts": [{"functionCall": {"args": {"Description": "Implements /healthz"}}}]}}]}` + "\n\n"))
		// Chunk 5: StartLine
		w.Write([]byte(`data: {"candidates": [{"content": {"parts": [{"functionCall": {"args": {"StartLine": 45}}}]}}]}` + "\n\n"))
		// Chunk 6: EndLine
		w.Write([]byte(`data: {"candidates": [{"content": {"parts": [{"functionCall": {"args": {"EndLine": 52}}}]}}]}` + "\n\n"))
		// Chunk 7: TargetContent
		w.Write([]byte(`data: {"candidates": [{"content": {"parts": [{"functionCall": {"args": {"TargetContent": "func main() {}"}}}]}}]}` + "\n\n"))
		// Chunk 8: ReplacementContent
		w.Write([]byte(`data: {"candidates": [{"content": {"parts": [{"functionCall": {"args": {"ReplacementContent": "func main() {\n\thttp.HandleFunc(\"/healthz\", healthHandler)\n}"}}}]}}]}` + "\n\n"))
		// Chunk 9: AllowMultiple
		w.Write([]byte(`data: {"candidates": [{"content": {"parts": [{"functionCall": {"args": {"AllowMultiple": false}}}]}}]}` + "\n\n"))
	}))
	defer server.Close()

	client := NewClient()
	client.APIKey = "test-key"
	client.BaseURL = server.URL

	tools := []mcp.Tool{
		{
			Name:        "replace_file_content",
			Description: "Replace content in a file",
			InputSchema: mcp.Schema{Type: "object"},
		},
	}

	var out bytes.Buffer
	result, err := client.GenerateWithTools(context.Background(), "gemini-3.8-flash", "Fix main.go", nil, tools, &out)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.ToolCalls) != 1 {
		t.Fatalf("expected exactly 1 tool call, got %d", len(result.ToolCalls))
	}

	tc := result.ToolCalls[0]
	if tc.ToolCall != "replace_file_content" {
		t.Errorf("expected tool_call 'replace_file_content', got %q", tc.ToolCall)
	}

	expectedArgs := map[string]interface{}{
		"TargetFile":         "A:/projects/microgravity/main.go",
		"Instruction":        "Add health check endpoint",
		"Description":        "Implements /healthz",
		"StartLine":          float64(45), // json unmarshals numbers as float64
		"EndLine":            float64(52),
		"TargetContent":      "func main() {}",
		"ReplacementContent": "func main() {\n\thttp.HandleFunc(\"/healthz\", healthHandler)\n}",
		"AllowMultiple":      false,
	}

	for k, v := range expectedArgs {
		if tc.Arguments[k] != v {
			t.Errorf("argument %q mismatch: expected %v, got %v", k, v, tc.Arguments[k])
		}
	}
}

// TestChallenger2_FragmentedSSE_MultipleSequentialToolCalls tests when a single SSE
// stream contains multiple different tool calls executed in sequence across chunks.
func TestChallenger2_FragmentedSSE_MultipleSequentialToolCalls(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		// Tool 1: view_file across 2 chunks
		w.Write([]byte(`data: {"candidates": [{"content": {"parts": [{"functionCall": {"name": "view_file", "args": {"AbsolutePath": "foo.txt"}}}]}}]}` + "\n\n"))
		w.Write([]byte(`data: {"candidates": [{"content": {"parts": [{"functionCall": {"args": {"StartLine": 1, "EndLine": 10}}}]}}]}` + "\n\n"))

		// Tool 2: run_command across 3 chunks
		w.Write([]byte(`data: {"candidates": [{"content": {"parts": [{"functionCall": {"name": "run_command"}}]}}]}` + "\n\n"))
		w.Write([]byte(`data: {"candidates": [{"content": {"parts": [{"functionCall": {"args": {"CommandLine": "go test ./..."}}}]}}]}` + "\n\n"))
		w.Write([]byte(`data: {"candidates": [{"content": {"parts": [{"functionCall": {"args": {"Cwd": "A:/projects"}}}]}}]}` + "\n\n"))

		// Tool 3: write_to_file in 1 chunk
		w.Write([]byte(`data: {"candidates": [{"content": {"parts": [{"functionCall": {"name": "write_to_file", "args": {"TargetFile": "bar.txt", "CodeContent": "pass"}}}]}}]}` + "\n\n"))
	}))
	defer server.Close()

	client := NewClient()
	client.APIKey = "test-key"
	client.BaseURL = server.URL

	tools := []mcp.Tool{
		{Name: "view_file", InputSchema: mcp.Schema{Type: "object"}},
		{Name: "run_command", InputSchema: mcp.Schema{Type: "object"}},
		{Name: "write_to_file", InputSchema: mcp.Schema{Type: "object"}},
	}

	var out bytes.Buffer
	result, err := client.GenerateWithTools(context.Background(), "gemini-3.8-flash", "Execute sequence", nil, tools, &out)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.ToolCalls) != 3 {
		t.Fatalf("expected 3 sequential tool calls, got %d: %+v", len(result.ToolCalls), result.ToolCalls)
	}

	// Verify Tool 1
	if result.ToolCalls[0].ToolCall != "view_file" {
		t.Errorf("call 0: expected 'view_file', got %q", result.ToolCalls[0].ToolCall)
	}
	if result.ToolCalls[0].Arguments["AbsolutePath"] != "foo.txt" || result.ToolCalls[0].Arguments["StartLine"] != float64(1) {
		t.Errorf("call 0 args mismatch: %+v", result.ToolCalls[0].Arguments)
	}

	// Verify Tool 2
	if result.ToolCalls[1].ToolCall != "run_command" {
		t.Errorf("call 1: expected 'run_command', got %q", result.ToolCalls[1].ToolCall)
	}
	if result.ToolCalls[1].Arguments["CommandLine"] != "go test ./..." || result.ToolCalls[1].Arguments["Cwd"] != "A:/projects" {
		t.Errorf("call 1 args mismatch: %+v", result.ToolCalls[1].Arguments)
	}

	// Verify Tool 3
	if result.ToolCalls[2].ToolCall != "write_to_file" {
		t.Errorf("call 2: expected 'write_to_file', got %q", result.ToolCalls[2].ToolCall)
	}
	if result.ToolCalls[2].Arguments["TargetFile"] != "bar.txt" || result.ToolCalls[2].Arguments["CodeContent"] != "pass" {
		t.Errorf("call 2 args mismatch: %+v", result.ToolCalls[2].Arguments)
	}
}

// TestChallenger2_FragmentedSSE_InterleavedTextAndHeartbeats tests robustness
// against SSE keep-alive comments, malformed chunks, and interleaved text deltas.
func TestChallenger2_FragmentedSSE_InterleavedTextAndHeartbeats(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Write([]byte(`: heartbeat ping` + "\n\n"))
		w.Write([]byte(`data: {"candidates": [{"content": {"parts": [{"text": "Executing plan...\n"}]}}]}` + "\n\n"))
		w.Write([]byte(`data: not-valid-json` + "\n\n"))
		w.Write([]byte(`data: {"candidates": [{"content": {"parts": [{"functionCall": {"name": "run_command", "args": {"CommandLine": "git diff"}}}]}}]}` + "\n\n"))
		w.Write([]byte(`data: [DONE]` + "\n\n"))
	}))
	defer server.Close()

	client := NewClient()
	client.APIKey = "test-key"
	client.BaseURL = server.URL

	tools := []mcp.Tool{{Name: "run_command", InputSchema: mcp.Schema{Type: "object"}}}

	var out bytes.Buffer
	result, err := client.GenerateWithTools(context.Background(), "gemini-3.8-flash", "Check git diff", nil, tools, &out)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !bytes.Contains(out.Bytes(), []byte("Executing plan...\n")) {
		t.Errorf("expected output to contain text, got %q", out.String())
	}
	if len(result.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(result.ToolCalls))
	}
	if result.ToolCalls[0].ToolCall != "run_command" {
		t.Errorf("expected 'run_command', got %q", result.ToolCalls[0].ToolCall)
	}
	if result.ToolCalls[0].Arguments["CommandLine"] != "git diff" {
		t.Errorf("expected CommandLine 'git diff', got %v", result.ToolCalls[0].Arguments["CommandLine"])
	}
}

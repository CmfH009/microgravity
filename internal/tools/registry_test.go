package tools

import (
	"context"
	"fmt"
	"testing"

	"github.com/CmfH009/microgravity/internal/mcp"
)

func TestRegistry(t *testing.T) {
	registry := NewRegistry()

	mockHandler := func(ctx context.Context, args map[string]interface{}) (string, error) {
		if val, ok := args["fail"]; ok && val == true {
			return "", fmt.Errorf("mock error")
		}
		return "mock result", nil
	}

	mockTool := mcp.Tool{Name: "mock_tool"}

	// Test Register
	err := registry.Register(mockTool, mockHandler)
	if err != nil {
		t.Fatalf("failed to register tool: %v", err)
	}

	// Test IsNative
	if !registry.IsNative("mock_tool") {
		t.Errorf("mock_tool should be native")
	}
	if registry.IsNative("unknown_tool") {
		t.Errorf("unknown_tool should not be native")
	}

	// Test duplicate Register (should silently skip)
	err = registry.Register(mockTool, mockHandler)
	if err != nil {
		t.Errorf("expected nil when re-registering tool, got error: %v", err)
	}

	// Test GetNativeTools
	tools := registry.GetNativeTools()
	if len(tools) != 1 {
		t.Errorf("expected 1 tool, got %d", len(tools))
	} else if tools[0].Name != "mock_tool" {
		t.Errorf("expected tool name mock_tool, got %s", tools[0].Name)
	}

	// Ensure GetNativeTools returns a copy
	tools[0].Name = "mutated"
	tools2 := registry.GetNativeTools()
	if tools2[0].Name == "mutated" {
		t.Errorf("GetNativeTools should return a copy to prevent mutation")
	}

	// Test Execute success
	res, err := registry.Execute(context.Background(), "mock_tool", map[string]interface{}{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res != "mock result" {
		t.Errorf("expected 'mock result', got %q", res)
	}

	// Test Execute returning error
	_, err = registry.Execute(context.Background(), "mock_tool", map[string]interface{}{"fail": true})
	if err == nil {
		t.Errorf("expected error from handler, got nil")
	}

	// Test Execute non-existent tool
	_, err = registry.Execute(context.Background(), "unknown_tool", map[string]interface{}{})
	if err == nil {
		t.Errorf("expected error executing unknown tool, got nil")
	}
}

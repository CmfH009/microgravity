package cmd

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/CmfH009/microgravity/internal/mcp"
	"github.com/CmfH009/microgravity/internal/tools"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newMinimalRegistry(t *testing.T) *tools.Registry {
	t.Helper()
	registry := tools.NewRegistry()

	// Register a few representative tools to exercise handleRequest paths
	registry.Register(mcp.Tool{
		Name:        "get_current_time",
		Description: "Get current date and time",
		InputSchema: mcp.Schema{
			Type:       "object",
			Properties: map[string]interface{}{},
		},
	}, func(ctx context.Context, args map[string]interface{}) (string, error) {
		return "2026-10-19T00:00:00Z", nil
	})

	registry.Register(mcp.Tool{
		Name:        "list_dir",
		Description: "List directory contents",
		InputSchema: mcp.Schema{
			Type: "object",
			Properties: map[string]interface{}{
				"path": map[string]interface{}{"type": "string"},
			},
			Required: []string{"path"},
		},
	}, func(ctx context.Context, args map[string]interface{}) (string, error) {
		path, _ := args["path"].(string)
		return "contents of " + path, nil
	})

	return registry
}

func TestHandleRequest_Initialize(t *testing.T) {
	registry := newMinimalRegistry(t)
	req := &mcp.Request{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "initialize",
		Params: map[string]interface{}{
			"clientInfo": map[string]interface{}{
				"name":    "test-client",
				"version": "1.0",
			},
		},
	}

	resp := handleRequest(req, registry)

	require.NotNil(t, resp)
	assert.Equal(t, "2.0", resp.JSONRPC)
	assert.Equal(t, 1, resp.ID)
	assert.Nil(t, resp.Error)
	require.NotNil(t, resp.Result)

	var result map[string]interface{}
	require.NoError(t, json.Unmarshal(resp.Result, &result))
	assert.Equal(t, "2024-11-05", result["protocolVersion"])
	assert.Contains(t, result, "capabilities")
	assert.Contains(t, result, "serverInfo")
}

func TestHandleRequest_ToolsList(t *testing.T) {
	registry := newMinimalRegistry(t)
	req := &mcp.Request{
		JSONRPC: "2.0",
		ID:      2,
		Method:  "tools/list",
	}

	resp := handleRequest(req, registry)

	require.NotNil(t, resp)
	assert.Equal(t, 2, resp.ID)
	assert.Nil(t, resp.Error)
	require.NotNil(t, resp.Result)

	var result mcp.ListToolsResult
	require.NoError(t, json.Unmarshal(resp.Result, &result))
	assert.Greater(t, len(result.Tools), 0)

	toolNames := make(map[string]bool)
	for _, tool := range result.Tools {
		toolNames[tool.Name] = true
	}
	assert.True(t, toolNames["get_current_time"], "get_current_time should be exposed")
	assert.True(t, toolNames["list_dir"], "list_dir should be exposed")
}

func TestHandleRequest_ToolsCall_Success(t *testing.T) {
	registry := newMinimalRegistry(t)
	req := &mcp.Request{
		JSONRPC: "2.0",
		ID:      3,
		Method:  "tools/call",
		Params: map[string]interface{}{
			"name":      "get_current_time",
			"arguments": map[string]interface{}{},
		},
	}

	resp := handleRequest(req, registry)

	require.NotNil(t, resp)
	assert.Equal(t, 3, resp.ID)
	assert.Nil(t, resp.Error)
	require.NotNil(t, resp.Result)

	var result mcp.CallToolResult
	require.NoError(t, json.Unmarshal(resp.Result, &result))
	assert.Greater(t, len(result.Content), 0)
	assert.False(t, result.IsError)
	assert.NotEmpty(t, result.Content[0].Text)
}

func TestHandleRequest_ToolsCall_UnknownTool(t *testing.T) {
	registry := newMinimalRegistry(t)
	req := &mcp.Request{
		JSONRPC: "2.0",
		ID:      4,
		Method:  "tools/call",
		Params: map[string]interface{}{
			"name":      "nonexistent_tool",
			"arguments": map[string]interface{}{},
		},
	}

	resp := handleRequest(req, registry)

	require.NotNil(t, resp)
	assert.Equal(t, 4, resp.ID)
	assert.Nil(t, resp.Result)
	require.NotNil(t, resp.Error)
	assert.Equal(t, -32603, resp.Error.Code)
	assert.Contains(t, resp.Error.Message, "not found")
}

func TestHandleRequest_UnknownMethod(t *testing.T) {
	registry := newMinimalRegistry(t)
	req := &mcp.Request{
		JSONRPC: "2.0",
		ID:      5,
		Method:  "notifications/initialized",
	}

	resp := handleRequest(req, registry)

	require.NotNil(t, resp)
	assert.Equal(t, 5, resp.ID)
	assert.Nil(t, resp.Result)
	require.NotNil(t, resp.Error)
	assert.Equal(t, -32601, resp.Error.Code)
	assert.Contains(t, resp.Error.Message, "Method not found")
}

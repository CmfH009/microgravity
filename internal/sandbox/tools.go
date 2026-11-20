package sandbox

import (
	"context"
	"fmt"
	"time"

	"github.com/purav/microgravity/internal/mcp"
	"github.com/purav/microgravity/internal/tools"
)

func RegisterSandboxTools(r *tools.Registry) {
	r.Register(mcp.Tool{
		Name:        "run_python",
		Description: "Execute a Python script securely in a sandboxed Docker container",
		InputSchema: mcp.Schema{
			Type: "object",
			Properties: map[string]interface{}{
				"code": map[string]interface{}{"type": "string"},
			},
			Required: []string{"code"},
		},
	}, func(ctx context.Context, args map[string]interface{}) (string, error) {
		code, ok := args["code"].(string)
		if !ok {
			return "", fmt.Errorf("invalid code argument")
		}

		ctxTimeout, cancel := context.WithTimeout(ctx, 60*time.Second) // Increased timeout to 60s
		defer cancel()
		
		return RunPython(ctxTimeout, code)
	})

	r.Register(mcp.Tool{
		Name:        "run_nodejs",
		Description: "Execute a Node.js script securely in a sandboxed Docker container",
		InputSchema: mcp.Schema{
			Type: "object",
			Properties: map[string]interface{}{
				"code": map[string]interface{}{"type": "string"},
			},
			Required: []string{"code"},
		},
	}, func(ctx context.Context, args map[string]interface{}) (string, error) {
		code, ok := args["code"].(string)
		if !ok {
			return "", fmt.Errorf("invalid code argument")
		}
		
		ctxTimeout, cancel := context.WithTimeout(ctx, 60*time.Second) // Increased timeout to 60s
		defer cancel()

		return RunNodeJS(ctxTimeout, code)
	})
}

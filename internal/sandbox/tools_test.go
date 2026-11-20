package sandbox

import (
	"context"
	"testing"

	"github.com/purav/microgravity/internal/tools"
)

func TestRegisterSandboxTools(t *testing.T) {
	registry := tools.NewRegistry()
	RegisterSandboxTools(registry)

	if !registry.IsNative("run_python") {
		t.Error("Expected run_python tool to be registered")
	}

	if !registry.IsNative("run_nodejs") {
		t.Error("Expected run_nodejs tool to be registered")
	}

	// Verify we can retrieve them in the schemas
	schemas := registry.GetNativeTools()
	foundPython := false
	foundNode := false
	for _, schema := range schemas {
		if schema.Name == "run_python" {
			foundPython = true
		}
		if schema.Name == "run_nodejs" {
			foundNode = true
		}
	}

	if !foundPython {
		t.Error("Expected run_python in schemas")
	}
	if !foundNode {
		t.Error("Expected run_nodejs in schemas")
	}
}

func TestSandboxToolsExecutionErrors(t *testing.T) {
	registry := tools.NewRegistry()
	RegisterSandboxTools(registry)
	ctx := context.Background()

	// Test missing code argument
	_, err := registry.Execute(ctx, "run_python", map[string]interface{}{})
	if err == nil {
		t.Error("Expected error when code is missing for run_python")
	}

	_, err = registry.Execute(ctx, "run_nodejs", map[string]interface{}{})
	if err == nil {
		t.Error("Expected error when code is missing for run_nodejs")
	}

	// We won't test successful execution here as it's covered by TestRunPython and TestRunNodeJS
	// which require docker to be available. We just want to ensure the handler is correctly unwrapping.
}

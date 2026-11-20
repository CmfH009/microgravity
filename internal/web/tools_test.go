package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/purav/microgravity/internal/tools"
)

func TestRegisterWebTools(t *testing.T) {
	registry := tools.NewRegistry()
	err := RegisterWebTools(registry)
	if err != nil {
		t.Fatalf("Failed to register web tools: %v", err)
	}

	if !registry.IsNative("read_url") {
		t.Errorf("read_url tool was not registered")
	}

	// search_web is registered via builtins.go, not via RegisterWebTools
}

func TestReadUrlToolExecution_EdgeCases(t *testing.T) {
	registry := tools.NewRegistry()
	_ = RegisterWebTools(registry)

	tests := []struct {
		name        string
		args        map[string]interface{}
		expectError bool
		errorMsg    string
	}{
		{
			name:        "Missing URL",
			args:        map[string]interface{}{},
			expectError: true,
			errorMsg:    "url is required and must be a string",
		},
		{
			name: "Invalid URL Type",
			args: map[string]interface{}{
				"url": 123,
			},
			expectError: true,
			errorMsg:    "url is required and must be a string",
		},
		{
			name: "Empty URL string",
			args: map[string]interface{}{
				"url": "   ",
			},
			expectError: true,
			errorMsg:    "url cannot be empty",
		},
		{
			name: "Invalid URL format",
			args: map[string]interface{}{
				"url": "not-a-url",
			},
			expectError: true,
			errorMsg:    "invalid url provided",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := registry.Execute(context.Background(), "read_url", tt.args)
			if !tt.expectError && err != nil {
				t.Errorf("Unexpected error: %v", err)
			}
			if tt.expectError {
				if err == nil {
					t.Errorf("Expected error but got nil")
				} else if !strings.Contains(err.Error(), tt.errorMsg) {
					t.Errorf("Expected error containing %q, got %q", tt.errorMsg, err.Error())
				}
			}
		})
	}
}

func TestReadUrlToolExecution_HappyPath(t *testing.T) {
	mockHTML := `<html><body><h1>Test</h1></body></html>`
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(mockHTML))
	}))
	defer ts.Close()

	registry := tools.NewRegistry()
	_ = RegisterWebTools(registry)

	args := map[string]interface{}{
		"url": ts.URL,
	}

	result, err := registry.Execute(context.Background(), "read_url", args)
	if err != nil {
		t.Fatalf("Failed to execute read_url: %v", err)
	}

	if result == "" || !strings.Contains(result, "Test") {
		t.Errorf("Expected valid markdown output, got %q", result)
	}
}

func TestReadUrlToolExecution_Timeout(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(100 * time.Millisecond)
		w.Write([]byte(`<html><body>Slow</body></html>`))
	}))
	defer ts.Close()

	registry := tools.NewRegistry()
	_ = RegisterWebTools(registry)

	args := map[string]interface{}{
		"url": ts.URL,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	_, err := registry.Execute(ctx, "read_url", args)
	if err == nil {
		t.Errorf("Expected context deadline exceeded error")
	} else if !strings.Contains(err.Error(), "context deadline exceeded") {
        t.Errorf("Expected context deadline error, got: %v", err)
    }
}



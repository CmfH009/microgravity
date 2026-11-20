package hooks

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestHooksExecutionHTTP(t *testing.T) {
	received := make(chan bool, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received <- true
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	mgr := NewManager()
	mgr.RegisterHook(Hook{
		Event: EventPreToolCall,
		URL:   server.URL,
	})

	err := mgr.Execute(context.Background(), Payload{
		Event:    EventPreToolCall,
		ToolName: "test_tool",
	})
	if err != nil {
		t.Fatalf("hook execution failed: %v", err)
	}

	select {
	case <-received:
		// success
	default:
		t.Errorf("expected HTTP hook to be called")
	}
}

func TestHooksLoadConfigFile(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "hooks.json")
	content := `{
		"hooks": [
			{
				"event": "pre_turn",
				"command": "echo",
				"args": ["hello"]
			}
		]
	}`
	if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write test hooks config: %v", err)
	}

	mgr := NewManager()
	if err := mgr.LoadConfig(configPath); err != nil {
		t.Fatalf("failed to load hooks config: %v", err)
	}

	if len(mgr.hooks) != 1 {
		t.Fatalf("expected 1 hook loaded, got %d", len(mgr.hooks))
	}
	if mgr.hooks[0].Event != EventPreTurn {
		t.Errorf("expected event pre_turn, got %s", mgr.hooks[0].Event)
	}
}

package engine_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/CmfH009/microgravity/internal/engine"
	"github.com/CmfH009/microgravity/internal/mcp"
	"github.com/CmfH009/microgravity/internal/tools"
)

// TestAdversarial_DualModeExecutionAndFallback stresses turn.go execution across:
// 1. Native ToolCalls directly from provider.
// 2. Raw text JSON tool call block regex fallback when native tool calls are absent.
// 3. Provider returns BOTH native tool call and commentary text.
// 4. Provider returns BOTH native tool call AND a JSON block in commentary text (asserting NO duplicate execution).
func TestAdversarial_DualModeExecutionAndFallback(t *testing.T) {
	type executionRecord struct {
		name string
		args map[string]interface{}
	}

	setupSession := func(t *testing.T, ssePayloads []string) (*engine.Session, *[]executionRecord) {
		isolateEngineTestEnv(t)
		var execMu sync.Mutex
		var executed []executionRecord
		var requestCount int32

		r := tools.NewRegistry()
		r.Register(mcp.Tool{
			Name:        "test_counter",
			Description: "Test counting tool",
			InputSchema: mcp.Schema{
				Type: "object",
				Properties: map[string]interface{}{
					"step": map[string]interface{}{"type": "integer"},
				},
			},
		}, func(ctx context.Context, args map[string]interface{}) (string, error) {
			execMu.Lock()
			defer execMu.Unlock()
			executed = append(executed, executionRecord{
				name: "test_counter",
				args: args,
			})
			return fmt.Sprintf("step %v executed successfully", args["step"]), nil
		})

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")
			reqNum := atomic.AddInt32(&requestCount, 1)
			if reqNum == 1 {
				for _, p := range ssePayloads {
					w.Write([]byte(p + "\n\n"))
				}
			} else {
				// Final completion response on subsequent turns
				w.Write([]byte(`data: {"candidates": [{"content": {"parts": [{"text": "Task finished."}]}}]}` + "\n\n"))
			}
		}))
		t.Cleanup(server.Close)

		cfg := engine.SessionConfig{
			SessionName:      "test-dual-mode",
			Registry:         r,
			ExplicitProvider: "cloud",
			CloudModelName:   "gemini-3.8-flash",
			ActiveModel:      "gemini-3.8-flash",
			AutoApprove:      true,
			CoreMemory:       "<core_memory></core_memory>",
		}

		sess, err := engine.NewSession(cfg)
		if err != nil {
			t.Fatalf("NewSession failed: %v", err)
		}
		t.Cleanup(func() { sess.Close() })

		sess.CloudClient.BaseURL = server.URL
		sess.CloudClient.APIKey = "dummy-test-key"

		return sess, &executed
	}

	t.Run("Mode 1: Native ToolCalls directly", func(t *testing.T) {
		sse := []string{
			`data: {"candidates": [{"content": {"parts": [{"functionCall": {"name": "test_counter", "args": {"step": 1}}}]}}]}`,
		}
		sess, records := setupSession(t, sse)
		resp, err := sess.ExecuteTurn(context.Background(), "run step 1", nil, nil)
		if err != nil {
			t.Fatalf("ExecuteTurn failed: %v", err)
		}
		if resp != "Task finished." {
			t.Errorf("expected final response 'Task finished.', got %q", resp)
		}

		if len(*records) != 1 {
			t.Fatalf("expected exactly 1 tool execution, got %d", len(*records))
		}
		if (*records)[0].name != "test_counter" {
			t.Errorf("expected tool test_counter, got %s", (*records)[0].name)
		}
	})

	t.Run("Mode 2: Raw text JSON tool call regex fallback", func(t *testing.T) {
		jsonBlock := "I will run step 2.\n```json\n{\n  \"tool\": \"test_counter\",\n  \"arguments\": {\n    \"step\": 2\n  }\n}\n```"
		sse := []string{
			fmt.Sprintf(`data: {"candidates": [{"content": {"parts": [{"text": %q}]}}]}`, jsonBlock),
		}
		sess, records := setupSession(t, sse)
		resp, err := sess.ExecuteTurn(context.Background(), "run step 2 via text", nil, nil)
		if err != nil {
			t.Fatalf("ExecuteTurn failed: %v", err)
		}
		if resp != "Task finished." {
			t.Errorf("expected final response 'Task finished.', got %q", resp)
		}

		if len(*records) != 1 {
			t.Fatalf("expected exactly 1 tool execution via fallback, got %d", len(*records))
		}
		if (*records)[0].name != "test_counter" {
			t.Errorf("expected tool test_counter, got %s", (*records)[0].name)
		}
	})

	t.Run("Mode 3a: Native tool call with commentary text", func(t *testing.T) {
		sse := []string{
			`data: {"candidates": [{"content": {"parts": [{"text": "Executing step 3 now."}]}}]}`,
			`data: {"candidates": [{"content": {"parts": [{"functionCall": {"name": "test_counter", "args": {"step": 3}}}]}}]}`,
		}
		sess, records := setupSession(t, sse)
		resp, err := sess.ExecuteTurn(context.Background(), "run step 3", nil, nil)
		if err != nil {
			t.Fatalf("ExecuteTurn failed: %v", err)
		}
		if resp != "Task finished." {
			t.Errorf("expected final response 'Task finished.', got %q", resp)
		}

		if len(*records) != 1 {
			t.Fatalf("expected exactly 1 tool execution, got %d", len(*records))
		}
		if (*records)[0].name != "test_counter" {
			t.Errorf("expected tool test_counter, got %s", (*records)[0].name)
		}
	})

	t.Run("Mode 3b: BOTH native tool call AND text JSON block (prevent duplicate execution)", func(t *testing.T) {
		// Model emits BOTH text containing JSON tool block AND native tool call
		jsonBlock := "Duplicate test:\n```json\n{\n  \"tool\": \"test_counter\",\n  \"arguments\": {\n    \"step\": 4\n  }\n}\n```"
		sse := []string{
			fmt.Sprintf(`data: {"candidates": [{"content": {"parts": [{"text": %q}, {"functionCall": {"name": "test_counter", "args": {"step": 4}}}]}}]}`, jsonBlock),
		}
		sess, records := setupSession(t, sse)
		resp, err := sess.ExecuteTurn(context.Background(), "run step 4 with duplicate block", nil, nil)
		if err != nil {
			t.Fatalf("ExecuteTurn failed: %v", err)
		}
		if resp != "Task finished." {
			t.Errorf("expected final response 'Task finished.', got %q", resp)
		}

		// Critical assertion: MUST NOT execute twice!
		if len(*records) != 1 {
			t.Fatalf("DUPLICATE EXECUTION DETECTED: expected 1 tool execution, got %d: %+v", len(*records), *records)
		}
		if (*records)[0].name != "test_counter" {
			t.Errorf("expected tool test_counter, got %s", (*records)[0].name)
		}
	})
}

package tools

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/CmfH009/microgravity/internal/sidecar"
)

func TestBrowserToolsRegistration(t *testing.T) {
	registry := NewRegistry()
	RegisterBrowserTools(registry)

	expectedTools := []string{
		"browser_navigate",
		"browser_interact",
		"browser_extract",
		"browser_screenshot",
		"browser_execute_plan",
	}

	for _, name := range expectedTools {
		if !registry.IsNative(name) {
			t.Errorf("expected browser tool %s to be registered", name)
		}
	}
}

func TestBrowserToolsExecution(t *testing.T) {
	registry := NewRegistry()
	RegisterBrowserTools(registry)
	ctx := context.Background()

	t.Run("browser_navigate_missing_url", func(t *testing.T) {
		_, err := registry.Execute(ctx, "browser_navigate", map[string]interface{}{})
		if err == nil {
			t.Error("expected error for missing url")
		}
	})

	t.Run("browser_navigate_and_extract", func(t *testing.T) {
		navOut, err := registry.Execute(ctx, "browser_navigate", map[string]interface{}{
			"url": "https://example.com",
		})
		if err != nil {
			t.Fatalf("browser_navigate failed: %v", err)
		}
		if !strings.Contains(navOut, "Example Domain") && !strings.Contains(navOut, "success") {
			t.Errorf("unexpected navigate output: %s", navOut)
		}

		extractOut, err := registry.Execute(ctx, "browser_extract", map[string]interface{}{
			"url": "https://example.com",
		})
		if err != nil {
			t.Fatalf("browser_extract failed: %v", err)
		}
		if !strings.Contains(extractOut, "Example Domain") {
			t.Errorf("unexpected extract output: %s", extractOut)
		}
	})

	t.Run("browser_interact_search_page", func(t *testing.T) {
		searchOut, err := registry.Execute(ctx, "browser_interact", map[string]interface{}{
			"action":  "search_page",
			"pattern": "Example",
		})
		if err != nil {
			t.Fatalf("browser_interact search_page failed: %v", err)
		}
		if !strings.Contains(searchOut, "search_page") {
			t.Errorf("unexpected search_page output: %s", searchOut)
		}
	})

	t.Run("browser_screenshot", func(t *testing.T) {
		screenOut, err := registry.Execute(ctx, "browser_screenshot", map[string]interface{}{
			"url":       "https://example.com",
			"file_name": "test_domain_screenshot.png",
		})
		if err != nil {
			t.Fatalf("browser_screenshot failed: %v", err)
		}
		if !strings.Contains(screenOut, "Screenshot captured") {
			t.Errorf("unexpected screenshot output: %s", screenOut)
		}
	})

	t.Run("browser_execute_plan", func(t *testing.T) {
		planOut, err := registry.Execute(ctx, "browser_execute_plan", map[string]interface{}{
			"actions": []interface{}{
				map[string]interface{}{"action": "navigate", "url": "https://example.com"},
				map[string]interface{}{"action": "search_page", "pattern": "Domain"},
			},
		})
		if err != nil {
			t.Fatalf("browser_execute_plan failed: %v", err)
		}
		if !strings.Contains(planOut, "steps_completed") || !strings.Contains(planOut, "success") {
			t.Errorf("unexpected plan output: %s", planOut)
		}
	})
}

func TestBrowserToolsDeterministicWithFakeRunner(t *testing.T) {
	t.Setenv("PATH", "")

	var lastReq map[string]interface{}
	var fakeResp map[string]interface{}
	var fakeErr error
	var fakeStderr string

	restore := sidecar.SetDefaultRunner(&sidecar.Runner{
		Command: sidecar.CommandRunnerFunc(func(ctx context.Context, name string, args []string, stdin []byte) ([]byte, []byte, error) {
			if err := json.Unmarshal(stdin, &lastReq); err != nil {
				t.Fatalf("invalid stdin JSON: %v", err)
			}
			if fakeErr != nil {
				return nil, []byte(fakeStderr), fakeErr
			}
			out, _ := json.Marshal(fakeResp)
			return out, nil, nil
		}),
		FindPython: func() string { return "fake-python" },
		FindScript: func(relPath, fallbackName string) string { return fallbackName },
	})
	defer restore()

	registry := NewRegistry()
	RegisterBrowserTools(registry)
	ctx := context.Background()

	// 1. Navigate
	fakeResp = map[string]interface{}{"status": "ok", "title": "Fake Page", "url": "https://fake.local"}
	out, err := registry.Execute(ctx, "browser_navigate", map[string]interface{}{
		"url":     "https://fake.local",
		"new_tab": true,
	})
	if err != nil {
		t.Fatalf("browser_navigate failed: %v", err)
	}
	if lastReq["action"] != "navigate" || lastReq["url"] != "https://fake.local" || lastReq["new_tab"] != true {
		t.Fatalf("unexpected navigate request: %+v", lastReq)
	}
	if !strings.Contains(out, "Fake Page") {
		t.Fatalf("unexpected navigate output: %s", out)
	}

	// 2. Extract with markdown formatting
	fakeResp = map[string]interface{}{
		"status":   "ok",
		"title":    "Docs",
		"url":      "https://fake.local/docs",
		"markdown": "## Heading\nBody text",
	}
	extOut, err := registry.Execute(ctx, "browser_extract", map[string]interface{}{
		"url": "https://fake.local/docs",
	})
	if err != nil {
		t.Fatalf("browser_extract failed: %v", err)
	}
	if !strings.Contains(extOut, "# Docs\nURL: https://fake.local/docs\n\n## Heading") {
		t.Fatalf("unexpected markdown extract output: %s", extOut)
	}

	// 3. Sidecar status=="error" unpacking
	fakeResp = map[string]interface{}{"status": "error", "error": "element not clickable"}
	_, err = registry.Execute(ctx, "browser_interact", map[string]interface{}{
		"action":   "click",
		"selector": "#missing",
	})
	if err == nil || !strings.Contains(err.Error(), "element not clickable") {
		t.Fatalf("expected unpacked sidecar error, got %v", err)
	}

	// 4. Process error with stderr capture
	fakeErr = errors.New("exit status 2")
	fakeStderr = "browser crash trace"
	_, err = RunBrowserTool(ctx, map[string]interface{}{"action": "navigate", "url": "https://fake.local"})
	if err == nil || !strings.Contains(err.Error(), "browser crash trace") {
		t.Fatalf("expected stderr in error, got %v", err)
	}
}

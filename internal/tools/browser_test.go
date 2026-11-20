package tools

import (
	"context"
	"strings"
	"testing"
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

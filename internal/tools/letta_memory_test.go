package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/CmfH009/microgravity/internal/sidecar"
)

func TestLettaMemoryToolsRegistration(t *testing.T) {
	registry := NewRegistry()
	RegisterLettaMemoryTools(registry)

	expectedTools := []string{
		"letta_memory_core",
		"letta_memory_episodic",
		"letta_memory_archival",
		"letta_memory_stats",
	}

	for _, name := range expectedTools {
		if !registry.IsNative(name) {
			t.Errorf("expected Letta memory tool %s to be registered", name)
		}
	}
}

func TestLettaMemoryCoreOperations(t *testing.T) {
	registry := NewRegistry()
	RegisterLettaMemoryTools(registry)
	ctx := context.Background()

	t.Run("core_missing_op", func(t *testing.T) {
		_, err := registry.Execute(ctx, "letta_memory_core", map[string]interface{}{})
		if err == nil {
			t.Error("expected error for missing op")
		}
	})

	t.Run("core_set_and_get", func(t *testing.T) {
		setOut, err := registry.Execute(ctx, "letta_memory_core", map[string]interface{}{
			"op":      "set",
			"block":   "scratchpad",
			"content": "Testing Go bridge for Letta core memory",
		})
		if err != nil {
			t.Fatalf("core set failed: %v", err)
		}
		if !strings.Contains(setOut, "Testing Go bridge") {
			t.Errorf("unexpected set output: %s", setOut)
		}

		getOut, err := registry.Execute(ctx, "letta_memory_core", map[string]interface{}{
			"op":    "get",
			"block": "scratchpad",
		})
		if err != nil {
			t.Fatalf("core get failed: %v", err)
		}
		if !strings.Contains(getOut, "Testing Go bridge") {
			t.Errorf("unexpected get output: %s", getOut)
		}
	})

	t.Run("core_append", func(t *testing.T) {
		appendOut, err := registry.Execute(ctx, "letta_memory_core", map[string]interface{}{
			"op":      "append",
			"block":   "scratchpad",
			"content": "Appended second line.",
		})
		if err != nil {
			t.Fatalf("core append failed: %v", err)
		}
		if !strings.Contains(appendOut, "Appended second line") {
			t.Errorf("unexpected append output: %s", appendOut)
		}
	})

	t.Run("core_compile", func(t *testing.T) {
		compileOut, err := registry.Execute(ctx, "letta_memory_core", map[string]interface{}{
			"op": "compile",
		})
		if err != nil {
			t.Fatalf("core compile failed: %v", err)
		}
		if !strings.Contains(compileOut, "<core_memory>") || !strings.Contains(compileOut, "</core_memory>") {
			t.Errorf("expected XML core memory formatting, got: %s", compileOut)
		}
	})
}

func TestLettaMemoryEpisodicOperations(t *testing.T) {
	registry := NewRegistry()
	RegisterLettaMemoryTools(registry)
	ctx := context.Background()

	t.Run("episodic_log_and_query", func(t *testing.T) {
		logOut, err := registry.Execute(ctx, "letta_memory_episodic", map[string]interface{}{
			"op":         "log",
			"role":       "user",
			"content":    "Autonomous Letta memory test event",
			"tags":       []string{"test", "integration"},
			"importance": float64(4.5),
			"session_id": "test_session_1",
		})
		if err != nil {
			t.Fatalf("episodic log failed: %v", err)
		}
		if !strings.Contains(logOut, "Autonomous Letta memory test event") {
			t.Errorf("unexpected log output: %s", logOut)
		}

		queryOut, err := registry.Execute(ctx, "letta_memory_episodic", map[string]interface{}{
			"op":         "query",
			"query":      "Autonomous Letta",
			"session_id": "test_session_1",
		})
		if err != nil {
			t.Fatalf("episodic query failed: %v", err)
		}
		if !strings.Contains(queryOut, "Autonomous Letta memory test event") {
			t.Errorf("unexpected query output: %s", queryOut)
		}
	})

	t.Run("episodic_recent", func(t *testing.T) {
		recentOut, err := registry.Execute(ctx, "letta_memory_episodic", map[string]interface{}{
			"op":    "recent",
			"limit": float64(5),
		})
		if err != nil {
			t.Fatalf("episodic recent failed: %v", err)
		}
		if !strings.Contains(recentOut, "events") {
			t.Errorf("unexpected recent output: %s", recentOut)
		}
	})
}

func TestLettaMemoryArchivalOperations(t *testing.T) {
	registry := NewRegistry()
	RegisterLettaMemoryTools(registry)
	ctx := context.Background()

	t.Run("archival_insert_and_search", func(t *testing.T) {
		insOut, err := registry.Execute(ctx, "letta_memory_archival", map[string]interface{}{
			"op":      "insert",
			"content": "Microgravity uses Gemma LLM routing with subagent swarms and Letta memory.",
			"tags":    []string{"architecture", "gemma"},
			"source":  "integration_test",
		})
		if err != nil {
			t.Fatalf("archival insert failed: %v", err)
		}
		if !strings.Contains(insOut, "arch_") {
			t.Errorf("unexpected insert output: %s", insOut)
		}

		searchOut, err := registry.Execute(ctx, "letta_memory_archival", map[string]interface{}{
			"op":    "search",
			"query": "subagent swarms Letta",
			"top_k": float64(3),
		})
		if err != nil {
			t.Fatalf("archival search failed: %v", err)
		}
		if !strings.Contains(searchOut, "Microgravity uses Gemma LLM") {
			t.Errorf("unexpected search output: %s", searchOut)
		}
	})

	t.Run("letta_memory_stats", func(t *testing.T) {
		statsOut, err := registry.Execute(ctx, "letta_memory_stats", map[string]interface{}{})
		if err != nil {
			t.Fatalf("letta_memory_stats failed: %v", err)
		}
		if !strings.Contains(statsOut, "core_memory") || !strings.Contains(statsOut, "episodic_memory") {
			t.Errorf("unexpected stats output: %s", statsOut)
		}
	})
}

func TestGetCompiledLettaMemory(t *testing.T) {
	ctx := context.Background()
	compiled := GetCompiledLettaMemory(ctx)
	if compiled == "" {
		t.Fatal("expected non-empty compiled Letta memory")
	}
	if !strings.HasPrefix(strings.TrimSpace(compiled), "<core_memory>") {
		t.Errorf("expected <core_memory> root tag, got: %s", compiled)
	}
	if !strings.Contains(compiled, "<human>") || !strings.Contains(compiled, "<persona>") || !strings.Contains(compiled, "<scratchpad>") {
		t.Errorf("expected standard core blocks inside compiled memory, got: %s", compiled)
	}
	if !strings.HasSuffix(strings.TrimSpace(compiled), "</core_memory>") {
		t.Errorf("expected </core_memory> closing tag, got: %s", compiled)
	}
}

func TestLettaMemoryDeterministicWithFakeRunner(t *testing.T) {
	t.Setenv("PATH", "")

	var lastReq map[string]interface{}
	var fakeResp map[string]interface{}

	restore := sidecar.SetDefaultRunner(&sidecar.Runner{
		Command: sidecar.CommandRunnerFunc(func(ctx context.Context, name string, args []string, stdin []byte) ([]byte, []byte, error) {
			if err := json.Unmarshal(stdin, &lastReq); err != nil {
				t.Fatalf("invalid stdin JSON: %v", err)
			}
			out, _ := json.Marshal(fakeResp)
			return out, nil, nil
		}),
		FindPython: func() string { return "fake-python" },
		FindScript: func(relPath, fallbackName string) string { return fallbackName },
	})
	defer restore()

	registry := NewRegistry()
	RegisterLettaMemoryTools(registry)

	// 1. GetCompiledLettaMemory (including nil ctx safety)
	fakeResp = map[string]interface{}{
		"status":   "ok",
		"compiled": "<core_memory><human>Alice</human></core_memory>",
	}
	got := GetCompiledLettaMemory(nil)
	if got != "<core_memory><human>Alice</human></core_memory>" {
		t.Fatalf("GetCompiledLettaMemory(nil) = %q", got)
	}
	if lastReq["op"] != "core_compile" {
		t.Fatalf("expected op=core_compile, got %+v", lastReq)
	}

	// 2. GetCompiledLettaMemory returns empty string on sidecar error
	fakeResp = map[string]interface{}{
		"status": "error",
		"error":  "database locked",
	}
	if gotErr := GetCompiledLettaMemory(context.Background()); gotErr != "" {
		t.Fatalf("expected empty string on error, got %q", gotErr)
	}

	// 3. Episodic and archival tool framing
	fakeResp = map[string]interface{}{"status": "ok", "count": 1}
	_, err := registry.Execute(context.Background(), "letta_memory_episodic", map[string]interface{}{
		"op":      "log",
		"role":    "user",
		"content": "hello",
	})
	if err != nil {
		t.Fatalf("letta_memory_episodic failed: %v", err)
	}
	if lastReq["op"] != "episodic_log" || lastReq["content"] != "hello" {
		t.Fatalf("unexpected episodic request: %+v", lastReq)
	}
}



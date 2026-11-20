package models

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/CmfH009/microgravity/internal/config"
	"github.com/CmfH009/microgravity/internal/huggingface"
	"github.com/CmfH009/microgravity/internal/ollama"
	tea "github.com/charmbracelet/bubbletea"
)

func isolateModelsTestEnv(t *testing.T) {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("USERPROFILE", tmp)
	t.Setenv("MG_MODELS_DIR", filepath.Join(tmp, "models"))
}

func TestScanner_SingleTagsCallAndStatusMapping(t *testing.T) {
	isolateModelsTestEnv(t)

	var tagCalls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/tags" {
			atomic.AddInt32(&tagCalls, 1)
			_, _ = w.Write([]byte(`{"models":[{"name":"gemma4:e2b"},{"name":"gemma:2b"}]}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	scanner := NewScannerWithClient(ollama.NewClient(srv.URL))
	catalog, err := scanner.Scan(context.Background())
	if err != nil {
		t.Fatalf("Scan failed: %v", err)
	}
	if len(catalog) == 0 {
		t.Fatal("expected non-empty catalog")
	}

	if got := atomic.LoadInt32(&tagCalls); got != 1 {
		t.Fatalf("expected exactly 1 /api/tags call per Scan, got %d", got)
	}

	e2b, ok := GetModelByID(catalog, "gemma4:e2b")
	if !ok || !strings.Contains(e2b.Status, "Downloaded") {
		t.Fatalf("expected gemma4:e2b Downloaded status, got %+v (ok=%v)", e2b, ok)
	}
}

func TestScanner_DynamicOllamaAndLocalGGUF(t *testing.T) {
	isolateModelsTestEnv(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/tags" {
			_, _ = w.Write([]byte(`{"models":[
				{"name":"gemma4:e2b"},
				{"name":"qwen2.5-coder:7b"},
				{"name":"custom-finetune:latest"},
				{"name":"hf.co/Qwen/Qwen2.5-Coder-7B-Instruct-GGUF:Q4_K_M"}
			]}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	localDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(localDir, "my-coder-Q4_K_M.gguf"), []byte("gguf-bytes-1"), 0644); err != nil {
		t.Fatalf("write local gguf: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(localDir, "nested"), 0755); err != nil {
		t.Fatalf("mkdir nested: %v", err)
	}
	if err := os.WriteFile(filepath.Join(localDir, "nested", "phi4-Q8_0.gguf"), []byte("gguf-bytes-2"), 0644); err != nil {
		t.Fatalf("write nested gguf: %v", err)
	}
	// .part incomplete files must be ignored
	if err := os.WriteFile(filepath.Join(localDir, "incomplete.gguf.part"), []byte("partial"), 0644); err != nil {
		t.Fatalf("write .part file: %v", err)
	}

	scanner := NewScannerWithClient(ollama.NewClient(srv.URL))
	scanner.SetLocalModelsDir(localDir)

	catalog, err := scanner.Scan(context.Background())
	if err != nil {
		t.Fatalf("Scan failed: %v", err)
	}

	// Known model should appear exactly once and be marked Downloaded
	qwenCount := 0
	for _, m := range catalog {
		if m.ID == "qwen2.5-coder:7b" {
			qwenCount++
			if !strings.Contains(m.Status, "Downloaded") {
				t.Errorf("expected qwen2.5-coder:7b Downloaded, got %q", m.Status)
			}
		}
	}
	if qwenCount != 1 {
		t.Fatalf("expected qwen2.5-coder:7b once in catalog, got %d", qwenCount)
	}

	// Extra custom Ollama models appended under Custom & Downloaded Local Models
	custom, ok := GetModelByID(catalog, "custom-finetune:latest")
	if !ok || custom.Category != customModelsCategory || !strings.Contains(custom.Status, "Downloaded") {
		t.Fatalf("expected custom-finetune:latest in %q, got %+v (ok=%v)", customModelsCategory, custom, ok)
	}

	hfOllama, ok := GetModelByID(catalog, "hf.co/Qwen/Qwen2.5-Coder-7B-Instruct-GGUF:Q4_K_M")
	if !ok || hfOllama.Category != customModelsCategory || !hfOllama.Offline {
		t.Fatalf("expected hf.co model in %q, got %+v (ok=%v)", customModelsCategory, hfOllama, ok)
	}

	// Local GGUF files discovered
	gguf1, ok := GetModelByID(catalog, "my-coder-Q4_K_M.gguf")
	if !ok || gguf1.Provider != "gguf" || gguf1.Quantization != "Q4_K_M" || gguf1.Category != customModelsCategory {
		t.Fatalf("expected my-coder-Q4_K_M.gguf in catalog, got %+v (ok=%v)", gguf1, ok)
	}
	gguf2, ok := GetModelByID(catalog, "nested/phi4-Q8_0.gguf")
	if !ok || gguf2.Provider != "gguf" || gguf2.Quantization != "Q8_0" {
		t.Fatalf("expected nested/phi4-Q8_0.gguf in catalog, got %+v (ok=%v)", gguf2, ok)
	}
	if _, hasPart := GetModelByID(catalog, "incomplete.gguf.part"); hasPart {
		t.Fatalf(".part file must not be listed in catalog")
	}
}

func TestScanner_ContextCancelled(t *testing.T) {
	isolateModelsTestEnv(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"models":[]}`))
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	scanner := NewScannerWithClient(ollama.NewClient(srv.URL))
	_, err := scanner.Scan(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got: %v", err)
	}
}

func TestScanner_ConcurrentScan_Race(t *testing.T) {
	isolateModelsTestEnv(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"models":[{"name":"gemma4:e2b"}]}`))
	}))
	defer srv.Close()

	scanner := NewScannerWithClient(ollama.NewClient(srv.URL))
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			cat, err := scanner.Scan(context.Background())
			if err != nil || len(cat) == 0 {
				t.Errorf("concurrent Scan failed: len=%d err=%v", len(cat), err)
			}
		}()
	}
	wg.Wait()
}

func TestTUI_BoundsAndSelection(t *testing.T) {
	isolateModelsTestEnv(t)

	// 1. Empty catalog must not corrupt cursor on up/down/enter.
	empty := model{catalog: nil, cursor: 0}
	for _, k := range []string{"up", "down", "enter"} {
		var next tea.Model
		if k == "up" {
			next, _ = empty.Update(tea.KeyMsg{Type: tea.KeyUp})
		} else if k == "down" {
			next, _ = empty.Update(tea.KeyMsg{Type: tea.KeyDown})
		} else if k == "enter" {
			next, _ = empty.Update(tea.KeyMsg{Type: tea.KeyEnter})
		}
		m := next.(model)
		if m.cursor < 0 {
			t.Fatalf("empty catalog %s resulted in negative cursor %d", k, m.cursor)
		}
	}

	// 2. Populated catalog persists active model via config.
	m := model{
		catalog: []ModelInfo{
			{ID: "gemma4:e2b", Name: "Gemma 4 e2B", Tier: "LOCAL", Status: "Downloaded"},
			{ID: "gemini-3.8-flash", Name: "Gemini 3.8 Flash", Tier: "FLASH", Status: "Available"},
		},
		cursor: 0,
	}
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
	next, cmd := next.(model).Update(tea.KeyMsg{Type: tea.KeyEnter})
	finalM := next.(model)
	if cmd == nil {
		t.Fatal("expected tea.Quit cmd on enter")
	}
	if finalM.selected.ID != "gemini-3.8-flash" {
		t.Fatalf("expected gemini-3.8-flash selected, got %q", finalM.selected.ID)
	}
	if finalM.err != nil {
		t.Fatalf("unexpected error saving active model: %v", finalM.err)
	}
	if got := config.GetActiveModel(); got != "gemini-3.8-flash" {
		t.Fatalf("expected persisted active model gemini-3.8-flash, got %q", got)
	}
}

func TestTUI_FilterTabsSearchViewportAndHF(t *testing.T) {
	isolateModelsTestEnv(t)

	localDir := t.TempDir()
	const ggufBytes = "GGUF_TEST_BINARY_WEIGHTS"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/models":
			_, _ = w.Write([]byte(`[{"id":"Qwen/Qwen2.5-Coder-7B-Instruct-GGUF","downloads":99999,"likes":500}]`))
		case "/api/models/Qwen/Qwen2.5-Coder-7B-Instruct-GGUF":
			_, _ = w.Write([]byte(`{
				"id":"Qwen/Qwen2.5-Coder-7B-Instruct-GGUF",
				"siblings":[
					{"rfilename":"qwen2.5-coder-7b-q4_k_m.gguf","size":4000000000},
					{"rfilename":"qwen2.5-coder-7b-q8_0.gguf","size":8000000000}
				]
			}`))
		case "/Qwen/Qwen2.5-Coder-7B-Instruct-GGUF/resolve/main/qwen2.5-coder-7b-q4_k_m.gguf":
			_, _ = w.Write([]byte(ggufBytes))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	catalog := []ModelInfo{
		{ID: "gemini-3.8-flash", Name: "Gemini 3.8 Flash", Provider: "cloud", Tier: "FLASH", Category: "Cloud", Offline: false, Status: "Available Cloud"},
		{ID: "qwen2.5-coder:7b", Name: "Qwen 2.5 Coder 7B", Provider: "ollama", Tier: "LOCAL", Category: "Qwen", Offline: true, Status: "Downloaded (Ready Offline)"},
		{ID: "deepseek-r1:7b", Name: "DeepSeek R1 7B", Provider: "ollama", Tier: "LOCAL", Category: "DeepSeek", Offline: true, Status: "Available Offline"},
		{ID: "llama3.2:3b", Name: "Llama 3.2 3B", Provider: "ollama", Tier: "LOCAL", Category: "Llama", Offline: true, Status: "Available Offline"},
		{ID: "phi4:14b", Name: "Phi-4 14B", Provider: "ollama", Tier: "LOCAL", Category: "Phi", Offline: true, Status: "Available Offline"},
	}

	m := model{
		catalog:        catalog,
		hfClient:       huggingface.NewClient(srv.URL),
		localModelsDir: localDir,
		availableRAMGB: 6.5, // Fits Q4_K_M (~4.2 GB est) but not Q8_0 (~8.0 GB est)
	}

	// 1. WindowSizeMsg + Viewport bounds
	next, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 20})
	m = next.(model)
	view := m.View()
	if !strings.Contains(view, "more below") {
		t.Fatalf("expected viewport pagination indicator in view, got:\n%s", view)
	}

	// 2. Tab cycling: All -> Local -> Downloaded -> Cloud -> Hugging Face -> All
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = next.(model)
	if m.filterTab != filterLocal || len(m.visibleModels()) != 4 {
		t.Fatalf("expected Local tab with 4 items, got tab=%v len=%d", m.filterTab, len(m.visibleModels()))
	}
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = next.(model)
	if m.filterTab != filterDownloaded || len(m.visibleModels()) != 1 || m.visibleModels()[0].ID != "qwen2.5-coder:7b" {
		t.Fatalf("expected Downloaded tab with qwen2.5-coder:7b, got %+v", m.visibleModels())
	}
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = next.(model)
	if m.filterTab != filterCloud || len(m.visibleModels()) != 1 || m.visibleModels()[0].ID != "gemini-3.8-flash" {
		t.Fatalf("expected Cloud tab with gemini-3.8-flash, got %+v", m.visibleModels())
	}
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyShiftTab})            // back to Downloaded
	next, _ = next.(model).Update(tea.KeyMsg{Type: tea.KeyShiftTab}) // back to Local
	next, _ = next.(model).Update(tea.KeyMsg{Type: tea.KeyShiftTab}) // back to All
	m = next.(model)
	if m.filterTab != filterAll {
		t.Fatalf("expected All tab after Shift+Tab, got %v", m.filterTab)
	}

	// 3. Live '/' search filter
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/")})
	next, _ = next.(model).Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("deep")})
	next, _ = next.(model).Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(model)
	if vis := m.visibleModels(); len(vis) != 1 || vis[0].ID != "deepseek-r1:7b" {
		t.Fatalf("expected live search 'deep' to isolate deepseek-r1:7b, got %+v", vis)
	}

	// Clear search via Esc
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = next.(model)
	if m.searchQuery != "" {
		t.Fatalf("expected searchQuery cleared on Esc, got %q", m.searchQuery)
	}

	// 4. 'h' Hugging Face search + 'd' one-key auto-select/download
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("h")})
	next, _ = next.(model).Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("qwen")})
	next, cmd := next.(model).Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(model)
	if cmd == nil {
		t.Fatal("expected tea.Cmd for HF search")
	}
	msg := cmd()
	next, _ = m.Update(msg)
	m = next.(model)
	if m.filterTab != filterHuggingFace || len(m.hfModels) != 1 || m.hfModels[0].ID != "Qwen/Qwen2.5-Coder-7B-Instruct-GGUF" {
		t.Fatalf("unexpected HF search state: tab=%v hfModels=%+v status=%q", m.filterTab, m.hfModels, m.statusMsg)
	}

	// Trigger one-key auto-select/download ('d')
	next, dlCmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("d")})
	m = next.(model)
	if dlCmd == nil {
		t.Fatal("expected tea.Cmd for HF auto-download")
	}
	dlMsg := dlCmd()
	next, _ = m.Update(dlMsg)
	m = next.(model)
	if m.err != nil {
		t.Fatalf("unexpected download error: %v", m.err)
	}
	expectedPath := filepath.Join(localDir, "qwen2.5-coder-7b-q4_k_m.gguf")
	data, err := os.ReadFile(expectedPath)
	if err != nil || string(data) != ggufBytes {
		t.Fatalf("expected downloaded GGUF at %s, err=%v data=%q", expectedPath, err, string(data))
	}
	if got := config.GetActiveModel(); got != "qwen2.5-coder-7b-q4_k_m.gguf" {
		t.Fatalf("expected active model set to downloaded GGUF, got %q", got)
	}
}

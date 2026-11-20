package models

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/CmfH009/microgravity/internal/config"
	"github.com/CmfH009/microgravity/internal/ollama"
)

func isolateModelsTestEnv(t *testing.T) {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("USERPROFILE", tmp)
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
		next, _ := empty.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)})
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

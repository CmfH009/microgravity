package models

import (
	"context"
	"strings"
	"testing"
)

func TestGetCatalogGemini3Series(t *testing.T) {
	scanner := NewScanner()
	catalog, err := scanner.Scan(context.Background())
	if err != nil {
		t.Fatalf("Scan failed: %v", err)
	}

	cloudModels := make(map[string]bool)
	for _, m := range catalog {
		if !m.Offline {
			cloudModels[m.ID] = true
		}
	}

	expectedGemini3 := []string{
		"gemini-3.8-flash",
		"gemini-3.7-flash",
		"gemini-3.1-pro",
		"gemini-3.6-flash",
		"gemini-3.5-flash-lite",
	}

	for _, id := range expectedGemini3 {
		if !cloudModels[id] {
			t.Errorf("Expected Gemini 3 series model %s in catalog", id)
		}
	}
}

func TestGetModelByID(t *testing.T) {
	scanner := NewScanner()
	catalog, _ := scanner.Scan(context.Background())

	m, found := GetModelByID(catalog, "gemini-3.1-pro")
	if !found || m.Tier != "PRO" {
		t.Errorf("Expected gemini-3.1-pro with PRO tier, got %+v (found=%v)", m, found)
	}

	mFlash, foundFlash := GetModelByID(catalog, "gemini-3.6-flash")
	if !foundFlash || mFlash.Tier != "FLASH" {
		t.Errorf("Expected gemini-3.6-flash with FLASH tier, got %+v (found=%v)", mFlash, foundFlash)
	}

	_, notFound := GetModelByID(catalog, "non-existent-model")
	if notFound {
		t.Errorf("Expected false for non-existent model")
	}
}

func TestModelContextWindows(t *testing.T) {
	scanner := NewScanner()
	catalog, err := scanner.Scan(context.Background())
	if err != nil {
		t.Fatalf("Scan failed: %v", err)
	}

	for _, m := range catalog {
		if m.ContextWindow <= 0 {
			t.Errorf("Model %s has invalid ContextWindow: %d", m.ID, m.ContextWindow)
		}
	}

	proModel, found := GetModelByID(catalog, "gemini-3.1-pro")
	if !found || proModel.ContextWindow != 2097152 {
		t.Errorf("Expected gemini-3.1-pro ContextWindow 2097152, got %d", proModel.ContextWindow)
	}

	flashModel, found := GetModelByID(catalog, "gemini-3.6-flash")
	if !found || flashModel.ContextWindow != 1048576 {
		t.Errorf("Expected gemini-3.6-flash ContextWindow 1048576, got %d", flashModel.ContextWindow)
	}

	gemmaModel, found := GetModelByID(catalog, "gemma:2b")
	if !found || gemmaModel.ContextWindow != 8192 {
		t.Errorf("Expected gemma:2b ContextWindow 8192, got %d", gemmaModel.ContextWindow)
	}

	orbitModel, found := GetModelByID(catalog, "orbit-coder:latest")
	if !found || orbitModel.ContextWindow != 8192 {
		t.Errorf("Expected orbit-coder:latest ContextWindow 8192, got %d", orbitModel.ContextWindow)
	}
}

func TestCatalogOpenLocalModelsAndBadges(t *testing.T) {
	scanner := NewScanner()
	catalog, err := scanner.Scan(context.Background())
	if err != nil {
		t.Fatalf("Scan failed: %v", err)
	}

	requiredLocal := []string{
		"qwen2.5-coder:3b",
		"qwen2.5-coder:7b",
		"qwen2.5-coder:14b",
		"qwen2.5-coder:32b",
		"qwen3:8b",
		"qwen3:14b",
		"deepseek-r1:7b",
		"deepseek-r1:14b",
		"deepseek-r1:32b",
		"deepseek-coder-v2:16b",
		"llama3.2:3b",
		"llama3.3:70b",
		"mistral:7b",
		"devstral:24b",
		"phi4:14b",
		"gpt-oss:20b",
	}

	for _, id := range requiredLocal {
		m, ok := GetModelByID(catalog, id)
		if !ok {
			t.Errorf("Expected open local model %q in catalog", id)
			continue
		}
		if !m.Offline || m.Provider != "ollama" || m.Tier != "LOCAL" {
			t.Errorf("Model %q expected offline ollama LOCAL, got offline=%v provider=%q tier=%q", id, m.Offline, m.Provider, m.Tier)
		}
		if m.ContextWindow < 16000 {
			t.Errorf("Model %q expected ContextWindow >= 16000, got %d", id, m.ContextWindow)
		}
	}

	if badge := (ModelInfo{ID: "qwen2.5-coder:7b", Provider: "ollama", Tier: "LOCAL"}).RenderBadge(); !strings.Contains(badge, "LOCAL MODEL") {
		t.Errorf("expected LOCAL MODEL badge for qwen2.5-coder:7b, got %q", badge)
	}
	if badge := (ModelInfo{ID: "gemma:2b", Provider: "ollama", Tier: "LOCAL"}).RenderBadge(); !strings.Contains(badge, "LOCAL GEMMA") {
		t.Errorf("expected LOCAL GEMMA badge for gemma:2b, got %q", badge)
	}
	if badge := (ModelInfo{ID: "model.gguf", Provider: "gguf", Tier: "LOCAL"}).RenderBadge(); !strings.Contains(badge, "LOCAL GGUF") {
		t.Errorf("expected LOCAL GGUF badge for gguf model, got %q", badge)
	}
	if badge := (ModelInfo{ID: "Qwen/Test", Provider: "huggingface", Tier: "HF"}).RenderBadge(); !strings.Contains(badge, "HUGGING FACE") {
		t.Errorf("expected HUGGING FACE badge for HF model, got %q", badge)
	}
}

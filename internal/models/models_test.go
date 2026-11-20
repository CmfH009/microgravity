package models

import (
	"context"
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

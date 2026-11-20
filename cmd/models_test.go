package cmd

import (
	"context"
	"testing"

	"github.com/CmfH009/microgravity/internal/models"
)

func TestModelSwitcherValidation(t *testing.T) {
	scanner := models.NewScanner()
	catalog, err := scanner.Scan(context.Background())
	if err != nil {
		t.Fatalf("Failed to scan models catalog: %v", err)
	}

	validModels := make(map[string]bool)
	for _, m := range catalog {
		validModels[m.ID] = true
	}

	testCases := []struct {
		target   string
		expected bool
	}{
		{"gemini-3.1-pro", true},
		{"gemma:2b", true},
		{"gemini-3.6-flash", true},
		{"invalid-model-name", false},
	}

	for _, tc := range testCases {
		if validModels[tc.target] != tc.expected {
			t.Errorf("Model %s validation expected %v, got %v", tc.target, tc.expected, validModels[tc.target])
		}
	}
}

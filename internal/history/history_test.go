package history

import (
	"testing"
)

func TestSaveAndLoadVisionHistory(t *testing.T) {
	tempDir := t.TempDir()
	h := NewHistoryWithDir(tempDir)

	entry := HistoryEntry{
		UserText: "Inspect diagram",
		Images:   []string{"data:image/png;base64,iVBORw0KGgo..."},
	}

	err := h.SaveEntry(entry)
	if err != nil {
		t.Fatalf("SaveEntry failed: %v", err)
	}

	loaded, err := h.LoadEntries()
	if err != nil || len(loaded) != 1 {
		t.Fatalf("Expected 1 loaded entry, got %d", len(loaded))
	}
	if len(loaded[0].Images) != 1 {
		t.Errorf("Expected 1 image payload in loaded history")
	}
}

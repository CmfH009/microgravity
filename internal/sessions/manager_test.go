package sessions

import (
	"testing"
)

func TestSessionManager(t *testing.T) {
	tempDir := t.TempDir()
	m := NewManager(tempDir)

	m.SaveSession("feat-auth", "User: Add auth\nAgent: Added auth")
	list, err := m.ListSessions()
	if err != nil || len(list) != 1 {
		t.Fatalf("Expected 1 session, got %d", len(list))
	}
	if list[0].Name != "feat-auth" {
		t.Errorf("Expected name 'feat-auth', got '%s'", list[0].Name)
	}

	err = m.DeleteSession("feat-auth")
	if err != nil {
		t.Fatalf("Failed to delete session: %v", err)
	}
}

func TestSessionExportImport(t *testing.T) {
	tempDir := t.TempDir()
	m := NewManager(tempDir)

	sessionContent := "User: Implement feature X\nAgent: Done implementing feature X."
	err := m.SaveSession("feature-x", sessionContent)
	if err != nil {
		t.Fatalf("SaveSession failed: %v", err)
	}

	exportPath := tempDir + "/exported_feature_x.txt"
	err = m.ExportSession("feature-x", exportPath)
	if err != nil {
		t.Fatalf("ExportSession failed: %v", err)
	}

	err = m.ImportSession(exportPath, "imported-feature-x")
	if err != nil {
		t.Fatalf("ImportSession failed: %v", err)
	}

	list, err := m.ListSessions()
	if err != nil {
		t.Fatalf("ListSessions failed: %v", err)
	}

	found := false
	for _, s := range list {
		if s.Name == "imported-feature-x" {
			found = true
			if s.Snippet == "" {
				t.Errorf("Expected snippet for imported session, got empty")
			}
		}
	}
	if !found {
		t.Errorf("Imported session 'imported-feature-x' not found in list")
	}
}


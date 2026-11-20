package journal

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestJournal_RecordAndUndo(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "journal.json")
	j := New(path)

	// Create a file, record a write that modifies it
	target := filepath.Join(dir, "file.txt")
	if err := os.WriteFile(target, []byte("original"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := j.Record("write", target, "original", "modified"); err != nil {
		t.Fatal(err)
	}
	if j.Len() != 1 {
		t.Fatalf("expected 1 entry, got %d", j.Len())
	}

	// Apply the change
	if err := os.WriteFile(target, []byte("modified"), 0644); err != nil {
		t.Fatal(err)
	}

	// Undo it
	undone, err := j.Undo(1)
	if err != nil {
		t.Fatal(err)
	}
	if len(undone) != 1 {
		t.Fatalf("expected 1 undone entry, got %d", len(undone))
	}

	content, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "original" {
		t.Fatalf("expected original content restored, got %q", string(content))
	}
}

func TestJournal_UndoNewFileDeletesIt(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "journal.json")
	j := New(path)

	target := filepath.Join(dir, "newfile.txt")
	if err := j.Record("write", target, "", "brand new content"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("brand new content"), 0644); err != nil {
		t.Fatal(err)
	}

	if _, err := j.Undo(1); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatal("expected new file to be deleted on undo")
	}
}

func TestJournal_UndoMultiple(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "journal.json")
	j := New(path)

	files := []string{"a.txt", "b.txt", "c.txt"}
	for _, f := range files {
		target := filepath.Join(dir, f)
		if err := os.WriteFile(target, []byte("orig"), 0644); err != nil {
			t.Fatal(err)
		}
		if err := j.Record("write", target, "orig", "modified"); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(target, []byte("modified"), 0644); err != nil {
			t.Fatal(err)
		}
	}

	undone, err := j.Undo(2)
	if err != nil {
		t.Fatal(err)
	}
	if len(undone) != 2 {
		t.Fatalf("expected 2 undone entries, got %d", len(undone))
	}
	if j.Len() != 1 {
		t.Fatalf("expected 1 entry remaining, got %d", j.Len())
	}

	// a.txt should still be modified (only 2 of 3 undone; a is oldest)
	content, _ := os.ReadFile(filepath.Join(dir, "a.txt"))
	if string(content) != "modified" {
		t.Fatalf("expected a.txt still modified, got %q", string(content))
	}
	// b.txt and c.txt should be restored to orig
	contentB, _ := os.ReadFile(filepath.Join(dir, "b.txt"))
	if string(contentB) != "orig" {
		t.Fatalf("expected b.txt restored to orig, got %q", string(contentB))
	}
	contentC, _ := os.ReadFile(filepath.Join(dir, "c.txt"))
	if string(contentC) != "orig" {
		t.Fatalf("expected c.txt restored to orig, got %q", string(contentC))
	}
}

func TestJournal_List(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "journal.json")
	j := New(path)

	j.Record("write", "a.txt", "", "x")
	j.Record("replace", "b.txt", "old", "new")

	list := j.List()
	if len(list) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(list))
	}
	if list[0].Target != "a.txt" || list[1].Target != "b.txt" {
		t.Fatalf("unexpected entries: %+v", list)
	}
}

func TestJournal_Clear(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "journal.json")
	j := New(path)

	j.Record("write", "a.txt", "", "x")
	if j.Len() != 1 {
		t.Fatalf("expected 1 entry, got %d", j.Len())
	}

	if err := j.Clear(); err != nil {
		t.Fatal(err)
	}
	if j.Len() != 0 {
		t.Fatalf("expected 0 entries after clear, got %d", j.Len())
	}
}

func TestJournal_Disabled(t *testing.T) {
	os.Setenv("MG_JOURNAL_DISABLE", "1")
	// Reset singleton so it picks up the env
	journalOnce = sync.Once{}
	defaultJournal = nil
	j := Default()
	defer func() {
		os.Unsetenv("MG_JOURNAL_DISABLE")
		journalOnce = sync.Once{}
		defaultJournal = nil
	}()
	if j.Enabled() {
		t.Fatal("expected journal disabled")
	}
	if err := j.Record("write", "x.txt", "", "y"); err != nil {
		t.Fatal(err)
	}
	if j.Len() != 0 {
		t.Fatalf("expected 0 entries when disabled, got %d", j.Len())
	}
}

func TestJournal_UndoMoreThanAvailable(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "journal.json")
	j := New(path)

	j.Record("write", "a.txt", "", "x")
	undone, err := j.Undo(100)
	if err != nil {
		t.Fatal(err)
	}
	if len(undone) != 1 {
		t.Fatalf("expected 1 undone entry, got %d", len(undone))
	}
	if j.Len() != 0 {
		t.Fatalf("expected 0 entries remaining, got %d", j.Len())
	}
}

func TestEntryJSON(t *testing.T) {
	e := Entry{Op: "replace", Target: "f.txt", Original: "old", New: "new", Timestamp: "t"}
	b, err := json.Marshal(e)
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	if !strings.Contains(s, `"op":"replace"`) {
		t.Fatalf("unexpected JSON: %s", s)
	}
}

// Package journal implements a write-ahead journal for file modifications
// made by Microgravity's write tools (write_to_file, replace_file_content).
//
// Each modification is recorded before it is applied, capturing the target
// path, the original content (snapshot before change), and the new content.
// `mg undo [n]` reverts the last n modifications by restoring original content,
// providing a safety net for autonomous agent file edits.
package journal

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// ErrJournalDisabled is returned when journaling is disabled via env.
var ErrJournalDisabled = errors.New("journaling disabled")

// Entry records one file modification.
type Entry struct {
	Op        string `json:"op"` // "write" | "replace"
	Target    string `json:"target"`
	Original  string `json:"original"`  // content before change ("" for new file)
	New       string `json:"new"`       // content after change
	Timestamp string `json:"timestamp"`
}

// Journal is a write-ahead log of file modifications.
type Journal struct {
	mu      sync.Mutex
	path    string
	entries []Entry
	enabled bool
}

// defaultJournal is the process-wide singleton.
var defaultJournal *Journal
var journalOnce sync.Once

// Default returns the process-wide journal singleton.
func Default() *Journal {
	journalOnce.Do(func() {
		defaultJournal = NewDefault()
	})
	return defaultJournal
}

// NewDefault creates a journal at ~/.microgravity/journal.json unless
// MG_JOURNAL_DISABLE=1.
func NewDefault() *Journal {
	j := &Journal{enabled: true}
	home, err := os.UserHomeDir()
	if err == nil {
		j.path = filepath.Join(home, ".microgravity", "journal.json")
	} else {
		j.path = filepath.Join(".microgravity", "journal.json")
	}
	if v := os.Getenv("MG_JOURNAL_DISABLE"); v != "" && v != "0" && !strings.EqualFold(v, "false") {
		j.enabled = false
		return j
	}
	_ = j.load()
	return j
}

// New creates a journal at the given path (used by tests).
func New(path string) *Journal {
	return &Journal{path: path, enabled: true}
}

// Enabled reports whether journaling is active.
func (j *Journal) Enabled() bool {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.enabled
}

// Record appends a modification entry before the change is applied.
func (j *Journal) Record(op, target, original, new string) error {
	j.mu.Lock()
	defer j.mu.Unlock()
	if !j.enabled {
		return nil
	}
	entry := Entry{
		Op:        op,
		Target:    target,
		Original:  original,
		New:       new,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	}
	j.entries = append(j.entries, entry)
	return j.save()
}

// Undo reverts the last n modifications by restoring original content.
// Returns the entries that were undone (in reverse chronological order).
func (j *Journal) Undo(n int) ([]Entry, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	if !j.enabled {
		return nil, ErrJournalDisabled
	}
	if n <= 0 {
		return nil, nil
	}
	if n > len(j.entries) {
		n = len(j.entries)
	}

	// Take the last n entries (most recent first for display).
	undone := make([]Entry, n)
	copy(undone, j.entries[len(j.entries)-n:])
	// Reverse so most recent is first.
	for i, k := 0, n-1; i < k; i, k = i+1, k-1 {
		undone[i], undone[k] = undone[k], undone[i]
	}

	// Restore original content for each undone entry (oldest first).
	for i := len(j.entries) - n; i < len(j.entries); i++ {
		e := j.entries[i]
		if e.Original == "" {
			// File was created by this write; remove it.
			_ = os.Remove(e.Target)
		} else {
			if err := os.WriteFile(e.Target, []byte(e.Original), 0644); err != nil {
				return undone, fmt.Errorf("restore %s: %w", e.Target, err)
			}
		}
	}

	j.entries = j.entries[:len(j.entries)-n]
	if err := j.save(); err != nil {
		return undone, err
	}
	return undone, nil
}

// List returns a copy of all journal entries.
func (j *Journal) List() []Entry {
	j.mu.Lock()
	defer j.mu.Unlock()
	out := make([]Entry, len(j.entries))
	copy(out, j.entries)
	return out
}

// Len returns the number of journal entries.
func (j *Journal) Len() int {
	j.mu.Lock()
	defer j.mu.Unlock()
	return len(j.entries)
}

// Clear removes all journal entries.
func (j *Journal) Clear() error {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.entries = nil
	return j.save()
}

func (j *Journal) save() error {
	if j.path == "" {
		return nil
	}
	if dir := filepath.Dir(j.path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return err
		}
	}
	data, err := json.MarshalIndent(j.entries, "", "  ")
	if err != nil {
		return err
	}
	tmp := j.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return err
	}
	return os.Rename(tmp, j.path)
}

func (j *Journal) load() error {
	if j.path == "" {
		return nil
	}
	data, err := os.ReadFile(j.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if len(data) == 0 {
		return nil
	}
	var entries []Entry
	if err := json.Unmarshal(data, &entries); err != nil {
		return err
	}
	j.entries = entries
	return nil
}

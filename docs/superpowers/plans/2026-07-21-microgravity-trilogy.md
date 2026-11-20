# Microgravity Trilogy Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement 3 major features for Microgravity (`mg`): Real-Time Plan Task Progress Mutator, Interactive TUI Session Manager, and Persisted Multi-Modal Vision Context.

**Architecture:** Extend `internal/plan` with real-time task status mutation, build an interactive LipGloss TUI session selector in `cmd/sessions.go`, and enhance `internal/history` to store image payloads across multi-turn sessions.

**Tech Stack:** Go 1.22+, `github.com/charmbracelet/lipgloss`, `github.com/c-bata/go-prompt`, `github.com/spf13/cobra`.

---

### Task 1: Real-Time Plan Task Progress Mutator

**Files:**
- Modify: `internal/plan/plan.go:60-90`
- Test: `internal/plan/plan_test.go:30-65`
- Modify: `cmd/chat.go:410-465`

- [ ] **Step 1: Write failing unit test for task status mutation**

```go
func TestPlanMutateTaskByTool(t *testing.T) {
	markdown := `- [ ] Write the failing test
- [ ] Implement code
`
	p, err := ParsePlan(markdown)
	if err != nil {
		t.Fatalf("ParsePlan failed: %v", err)
	}

	p.MarkNextPending(StatusInProgress)
	if p.Tasks[0].Status != StatusInProgress {
		t.Errorf("Expected Task 0 to be StatusInProgress")
	}

	p.MarkNextPending(StatusCompleted)
	if p.Tasks[0].Status != StatusCompleted {
		t.Errorf("Expected Task 0 to be StatusCompleted")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test -v ./internal/plan/...`
Expected: FAIL with `MarkNextPending undefined`

- [ ] **Step 3: Implement MarkNextPending in internal/plan/plan.go**

```go
func (p *Plan) MarkNextPending(newStatus TaskStatus) bool {
	for i := range p.Tasks {
		if p.Tasks[i].Status == StatusPending || p.Tasks[i].Status == StatusInProgress {
			p.Tasks[i].Status = newStatus
			return true
		}
	}
	return false
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test -v ./internal/plan/...`
Expected: PASS

- [ ] **Step 5: Wire live plan mutation into cmd/chat.go after native tool execution**

- [ ] **Step 6: Commit**

```bash
git add internal/plan/ cmd/chat.go
git commit -m "feat(plan): add live task status mutator on tool executions"
```

---

### Task 2: Interactive Session Manager (mg sessions)

**Files:**
- Create: `internal/sessions/manager.go`
- Test: `internal/sessions/manager_test.go`
- Modify: `cmd/sessions.go:1-60`

- [ ] **Step 1: Write failing unit test for session list and delete operations**

```go
func TestSessionManager(t *testing.T) {
	tempDir := t.TempDir()
	m := NewManager(tempDir)

	m.SaveSession("feat-auth", "User: Add auth\nAgent: Added auth")
	list, err := m.ListSessions()
	if err != nil || len(list) != 1 {
		t.Fatalf("Expected 1 session, got %d", len(list))
	}

	err = m.DeleteSession("feat-auth")
	if err != nil {
		t.Fatalf("Failed to delete session: %v", err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test -v ./internal/sessions/...`
Expected: FAIL with `package not found`

- [ ] **Step 3: Implement internal/sessions/manager.go**

- [ ] **Step 4: Run test to verify it passes**

Run: `go test -v ./internal/sessions/...`
Expected: PASS

- [x] **Step 5: Update cmd/sessions.go with interactive LipGloss TUI menu**

- [x] **Step 6: Commit**

```bash
git add internal/sessions/ cmd/sessions.go
git commit -m "feat(sessions): add interactive TUI session manager"
```

---

### Task 3: Persisted Multi-Modal Vision Context

**Files:**
- Modify: `internal/history/history.go:10-50`
- Test: `internal/history/history_test.go:10-40`
- Modify: `internal/vision/parser.go:20-60`

- [x] **Step 1: Write failing unit test for vision history persistence**

```go
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
```

- [x] **Step 2: Run test to verify it fails**

Run: `go test -v ./internal/history/...`
Expected: FAIL with missing struct fields/methods

- [x] **Step 3: Implement vision entry persistence in internal/history**

- [x] **Step 4: Run test to verify it passes**

Run: `go test -v ./internal/history/...`
Expected: PASS

- [x] **Step 5: Commit**

```bash
git add internal/history/ internal/vision/
git commit -m "feat(vision): persist multi-modal image context across chat turns"
```

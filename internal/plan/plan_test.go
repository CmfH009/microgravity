package plan

import (
	"strings"
	"testing"
)

func TestParsePlan(t *testing.T) {
	markdown := `# Implementation Plan

- [ ] Task 1: Initialize database
- [-] Task 2: Create user API routes
- [x] Task 3: Setup authentication
- [!] Task 4: Deploy to staging
`

	p, err := ParsePlan(markdown)
	if err != nil {
		t.Fatalf("ParsePlan failed: %v", err)
	}

	if len(p.Tasks) != 4 {
		t.Fatalf("Expected 4 tasks, got %d", len(p.Tasks))
	}

	if p.Tasks[0].Status != StatusPending {
		t.Errorf("Task 1 expected pending, got %v", p.Tasks[0].Status)
	}
	if p.Tasks[1].Status != StatusInProgress {
		t.Errorf("Task 2 expected in progress, got %v", p.Tasks[1].Status)
	}
	if p.Tasks[2].Status != StatusCompleted {
		t.Errorf("Task 3 expected completed, got %v", p.Tasks[2].Status)
	}
	if p.Tasks[3].Status != StatusFailed {
		t.Errorf("Task 4 expected failed, got %v", p.Tasks[3].Status)
	}
}

func TestPlanProgressAndRender(t *testing.T) {
	markdown := `- [x] Step A
- [ ] Step B
- [ ] Step C
`
	p, err := ParsePlan(markdown)
	if err != nil {
		t.Fatalf("ParsePlan failed: %v", err)
	}

	completed, total, pct := p.Progress()
	if completed != 1 || total != 3 || pct != 33 {
		t.Errorf("Progress expected 1/3 (33%%), got %d/%d (%d%%)", completed, total, pct)
	}

	// Update Step B to completed
	updated := p.UpdateTask("Step B", StatusCompleted)
	if !updated {
		t.Errorf("Failed to update Step B")
	}

	completed, total, pct = p.Progress()
	if completed != 2 || total != 3 || pct != 66 {
		t.Errorf("Progress expected 2/3 (66%%), got %d/%d (%d%%)", completed, total, pct)
	}

	tuiOutput := p.RenderTUI()
	if tuiOutput == "" {
		t.Errorf("RenderTUI returned empty string")
	}
	if !strings.Contains(tuiOutput, "Step A") || !strings.Contains(tuiOutput, "Step B") {
		t.Errorf("RenderTUI output missing task steps")
	}
}

func TestMarkNextPending(t *testing.T) {
	markdown := `- [ ] Task 1: Write test
- [ ] Task 2: Implement code
`
	p, err := ParsePlan(markdown)
	if err != nil {
		t.Fatalf("ParsePlan failed: %v", err)
	}

	p.MarkNextPending(StatusInProgress)
	if p.Tasks[0].Status != StatusInProgress {
		t.Errorf("Expected Task 0 to be StatusInProgress, got %v", p.Tasks[0].Status)
	}

	p.MarkNextPending(StatusCompleted)
	if p.Tasks[0].Status != StatusCompleted {
		t.Errorf("Expected Task 0 to be StatusCompleted, got %v", p.Tasks[0].Status)
	}
}

func TestToMarkdown(t *testing.T) {
	p := &Plan{
		Title: "Refactoring Plan",
		Tasks: []Task{
			{ID: 1, Description: "Setup DB", Status: StatusCompleted},
			{ID: 2, Description: "Write APIs", Status: StatusInProgress},
			{ID: 3, Description: "Add tests", Status: StatusPending},
		},
	}

	md := p.ToMarkdown()
	if !strings.Contains(md, "# Refactoring Plan") {
		t.Errorf("Expected markdown title, got %s", md)
	}
	if !strings.Contains(md, "- [x] Setup DB") {
		t.Errorf("Expected completed task in md, got %s", md)
	}
	if !strings.Contains(md, "- [-] Write APIs") {
		t.Errorf("Expected in-progress task in md, got %s", md)
	}
	if !strings.Contains(md, "- [ ] Add tests") {
		t.Errorf("Expected pending task in md, got %s", md)
	}
}

func TestUpdateTaskByIDAndAddTask(t *testing.T) {
	p := &Plan{
		Title: "Test Plan",
	}

	task1 := p.AddTask("First Task")
	if task1.ID != 1 || task1.Status != StatusPending {
		t.Errorf("Unexpected task created: %+v", task1)
	}

	task2 := p.AddTask("Second Task")
	if task2.ID != 2 {
		t.Errorf("Unexpected task ID: %d", task2.ID)
	}

	ok := p.UpdateTaskByID(2, StatusCompleted)
	if !ok || p.Tasks[1].Status != StatusCompleted {
		t.Errorf("Failed to update task by ID")
	}

	okInvalid := p.UpdateTaskByID(999, StatusCompleted)
	if okInvalid {
		t.Errorf("Expected false for invalid ID, got true")
	}
}

func TestSaveAndLoadPlanFromFile(t *testing.T) {
	p := &Plan{
		Title: "Disk Storage Plan",
		Tasks: []Task{
			{ID: 1, Description: "Verify disk write", Status: StatusCompleted},
		},
	}

	tmpFile := t.TempDir() + "/test_plan.md"
	err := p.SaveToFile(tmpFile)
	if err != nil {
		t.Fatalf("SaveToFile failed: %v", err)
	}

	loaded, err := LoadPlanFromFile(tmpFile)
	if err != nil {
		t.Fatalf("LoadPlanFromFile failed: %v", err)
	}

	if loaded.Title != p.Title || len(loaded.Tasks) != 1 || loaded.Tasks[0].Description != "Verify disk write" {
		t.Errorf("Loaded plan does not match saved plan: %+v", loaded)
	}
}


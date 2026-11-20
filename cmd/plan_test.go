package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/purav/microgravity/internal/plan"
)

func TestPlanCommands(t *testing.T) {
	tmpDir := t.TempDir()
	planPath := filepath.Join(tmpDir, "PLAN.md")

	// Test initializing plan
	p := &plan.Plan{
		Title: "Test Project Plan",
	}
	p.AddTask("Build Core Engine")
	p.AddTask("Add CLI Commands")

	err := p.SaveToFile(planPath)
	if err != nil {
		t.Fatalf("Failed to save plan: %v", err)
	}

	// Verify loading
	loaded, err := plan.LoadPlanFromFile(planPath)
	if err != nil {
		t.Fatalf("Failed to load plan: %v", err)
	}

	if len(loaded.Tasks) != 2 {
		t.Fatalf("Expected 2 tasks, got %d", len(loaded.Tasks))
	}

	// Update task 1 to completed
	loaded.UpdateTaskByID(1, plan.StatusCompleted)
	err = loaded.SaveToFile(planPath)
	if err != nil {
		t.Fatalf("Failed to resave plan: %v", err)
	}

	// Reload & verify status
	reloaded, _ := plan.LoadPlanFromFile(planPath)
	if reloaded.Tasks[0].Status != plan.StatusCompleted {
		t.Errorf("Task 1 status expected completed, got %s", reloaded.Tasks[0].Status)
	}

	// Clean up
	os.Remove(planPath)
}

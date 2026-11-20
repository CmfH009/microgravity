package swarm_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/purav/microgravity/internal/swarm"
)

func TestSwarmManager(t *testing.T) {
	sm := swarm.NewSwarmManager()

	worker1 := sm.Spawn("task-1", "Audit security rules")
	worker2 := sm.Spawn("task-2", "Optimize database queries")

	if worker1.ID != "task-1" || worker2.ID != "task-2" {
		t.Fatalf("Unexpected worker IDs: %s, %s", worker1.ID, worker2.ID)
	}

	// Run workers concurrently
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	sm.ExecuteWorker(ctx, "task-1", func(ctx context.Context) (string, error) {
		time.Sleep(50 * time.Millisecond)
		return "Security audit complete: 0 flaws found", nil
	})

	sm.ExecuteWorker(ctx, "task-2", func(ctx context.Context) (string, error) {
		time.Sleep(50 * time.Millisecond)
		return "Database queries optimized: 35% faster", nil
	})

	list := sm.List()
	if len(list) != 2 {
		t.Errorf("Expected 2 workers, got %d", len(list))
	}

	rendered := sm.RenderDashboard()
	if !strings.Contains(rendered, "task-1") || !strings.Contains(rendered, "Security audit complete") {
		t.Errorf("Dashboard missing worker output: %s", rendered)
	}
}

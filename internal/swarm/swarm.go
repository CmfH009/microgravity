package swarm

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/lipgloss"
)

type WorkerStatus string

const (
	StatusPending   WorkerStatus = "PENDING"
	StatusRunning   WorkerStatus = "RUNNING"
	StatusCompleted WorkerStatus = "COMPLETED"
	StatusFailed    WorkerStatus = "FAILED"
)

type Worker struct {
	ID         string
	TaskPrompt string
	Status     WorkerStatus
	Result     string
	Err        error
	StartTime  time.Time
	EndTime    time.Time
}

type SwarmManager struct {
	mu      sync.RWMutex
	workers map[string]*Worker
}

func NewSwarmManager() *SwarmManager {
	return &SwarmManager{
		workers: make(map[string]*Worker),
	}
}

func (sm *SwarmManager) Spawn(id, prompt string) *Worker {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	w := &Worker{
		ID:         id,
		TaskPrompt: prompt,
		Status:     StatusPending,
	}
	sm.workers[id] = w
	return w
}

func (sm *SwarmManager) ExecuteWorker(ctx context.Context, id string, fn func(ctx context.Context) (string, error)) {
	sm.mu.Lock()
	w, exists := sm.workers[id]
	if !exists {
		sm.mu.Unlock()
		return
	}
	w.Status = StatusRunning
	w.StartTime = time.Now()
	sm.mu.Unlock()

	res, err := fn(ctx)

	sm.mu.Lock()
	defer sm.mu.Unlock()
	w.EndTime = time.Now()
	if err != nil {
		w.Status = StatusFailed
		w.Err = err
	} else {
		w.Status = StatusCompleted
		w.Result = res
	}
}

func (sm *SwarmManager) List() []*Worker {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	var list []*Worker
	for _, w := range sm.workers {
		workerCopy := *w
		list = append(list, &workerCopy)
	}
	sort.Slice(list, func(i, j int) bool {
		return list[i].ID < list[j].ID
	})
	return list
}

func (sm *SwarmManager) RenderDashboard() string {
	workers := sm.List()

	boxStyle := lipgloss.NewStyle().
		Border(lipgloss.DoubleBorder()).
		BorderForeground(lipgloss.Color("208")).
		Padding(1, 2)

	titleStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("213")).Bold(true)
	pendingBadge := lipgloss.NewStyle().Foreground(lipgloss.Color("0")).Background(lipgloss.Color("220")).Bold(true).Padding(0, 1)
	runningBadge := lipgloss.NewStyle().Foreground(lipgloss.Color("15")).Background(lipgloss.Color("33")).Bold(true).Padding(0, 1)
	completedBadge := lipgloss.NewStyle().Foreground(lipgloss.Color("15")).Background(lipgloss.Color("40")).Bold(true).Padding(0, 1)
	failedBadge := lipgloss.NewStyle().Foreground(lipgloss.Color("15")).Background(lipgloss.Color("196")).Bold(true).Padding(0, 1)

	var sb strings.Builder
	sb.WriteString(titleStyle.Render("🐝 MICROGRAVITY PARALLEL SUBAGENT SWARM DASHBOARD") + "\n\n")

	if len(workers) == 0 {
		sb.WriteString("No active swarm workers.\n")
		return boxStyle.Render(sb.String())
	}

	for _, w := range workers {
		var badge string
		switch w.Status {
		case StatusPending:
			badge = pendingBadge.Render("PENDING")
		case StatusRunning:
			badge = runningBadge.Render("RUNNING")
		case StatusCompleted:
			badge = completedBadge.Render("COMPLETED")
		case StatusFailed:
			badge = failedBadge.Render("FAILED")
		}

		sb.WriteString(fmt.Sprintf("[%s] %s %s\n", w.ID, badge, w.TaskPrompt))
		if w.Status == StatusCompleted && w.Result != "" {
			sb.WriteString(fmt.Sprintf("    └── Result: %s\n", w.Result))
		} else if w.Status == StatusFailed && w.Err != nil {
			sb.WriteString(fmt.Sprintf("    └── Error: %v\n", w.Err))
		}
		sb.WriteString("\n")
	}

	return boxStyle.Render(strings.TrimSpace(sb.String()))
}

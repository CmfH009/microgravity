package plan

import (
	"fmt"
	"os"
	"regexp"
	"strings"
	"sync"

	"github.com/charmbracelet/lipgloss"
)

type TaskStatus string

const (
	StatusPending    TaskStatus = "[ ]"
	StatusInProgress TaskStatus = "[-]"
	StatusCompleted  TaskStatus = "[x]"
	StatusFailed     TaskStatus = "[!]"
)

type Task struct {
	ID          int
	Description string
	Status      TaskStatus
}

type Plan struct {
	mu    sync.RWMutex
	Title string
	Tasks []Task
}

var taskRegex = regexp.MustCompile(`(?m)^\s*-\s*\[([ x\-!])\]\s*(.+)`)
var titleRegex = regexp.MustCompile(`(?m)^#\s*(.+)`)

func NewPlan(title string) *Plan {
	if title == "" {
		title = "Execution Plan"
	}
	return &Plan{
		Title: title,
		Tasks: make([]Task, 0),
	}
}

func ParsePlan(markdown string) (*Plan, error) {
	p := &Plan{}

	if titleMatch := titleRegex.FindStringSubmatch(markdown); len(titleMatch) > 1 {
		p.Title = strings.TrimSpace(titleMatch[1])
	} else {
		p.Title = "Execution Plan"
	}

	matches := taskRegex.FindAllStringSubmatch(markdown, -1)
	for i, match := range matches {
		if len(match) < 3 {
			continue
		}
		statusChar := match[1]
		desc := strings.TrimSpace(match[2])

		var status TaskStatus
		switch statusChar {
		case "x":
			status = StatusCompleted
		case "-":
			status = StatusInProgress
		case "!":
			status = StatusFailed
		default:
			status = StatusPending
		}

		p.Tasks = append(p.Tasks, Task{
			ID:          i + 1,
			Description: desc,
			Status:      status,
		})
	}

	return p, nil
}

func (p *Plan) Progress() (completed int, total int, percentage int) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	total = len(p.Tasks)
	if total == 0 {
		return 0, 0, 0
	}
	for _, t := range p.Tasks {
		if t.Status == StatusCompleted {
			completed++
		}
	}
	percentage = (completed * 100) / total
	return completed, total, percentage
}

func (p *Plan) UpdateTask(query string, newStatus TaskStatus) bool {
	p.mu.Lock()
	defer p.mu.Unlock()

	queryLower := strings.ToLower(strings.TrimSpace(query))
	for i := range p.Tasks {
		if strings.Contains(strings.ToLower(p.Tasks[i].Description), queryLower) {
			p.Tasks[i].Status = newStatus
			return true
		}
	}
	return false
}

func (p *Plan) MarkNextPending(newStatus TaskStatus) bool {
	p.mu.Lock()
	defer p.mu.Unlock()

	for i := range p.Tasks {
		if p.Tasks[i].Status == StatusPending || p.Tasks[i].Status == StatusInProgress {
			p.Tasks[i].Status = newStatus
			return true
		}
	}
	return false
}

func (p *Plan) UpdateTaskByID(id int, newStatus TaskStatus) bool {
	p.mu.Lock()
	defer p.mu.Unlock()

	for i := range p.Tasks {
		if p.Tasks[i].ID == id {
			p.Tasks[i].Status = newStatus
			return true
		}
	}
	return false
}

func (p *Plan) AddTask(description string) Task {
	p.mu.Lock()
	defer p.mu.Unlock()

	t := Task{
		ID:          len(p.Tasks) + 1,
		Description: strings.TrimSpace(description),
		Status:      StatusPending,
	}
	p.Tasks = append(p.Tasks, t)
	return t
}

func (p *Plan) GetTasks() []Task {
	p.mu.RLock()
	defer p.mu.RUnlock()

	tasksCopy := make([]Task, len(p.Tasks))
	copy(tasksCopy, p.Tasks)
	return tasksCopy
}

func (p *Plan) ToMarkdown() string {
	p.mu.RLock()
	defer p.mu.RUnlock()

	var sb strings.Builder
	if p.Title != "" {
		sb.WriteString(fmt.Sprintf("# %s\n\n", p.Title))
	} else {
		sb.WriteString("# Execution Plan\n\n")
	}

	for _, t := range p.Tasks {
		sb.WriteString(fmt.Sprintf("- %s %s\n", t.Status, t.Description))
	}

	return sb.String()
}

func (p *Plan) SaveToFile(filename string) error {
	content := p.ToMarkdown()
	return os.WriteFile(filename, []byte(content), 0644)
}

func LoadPlanFromFile(filename string) (*Plan, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, err
	}
	return ParsePlan(string(data))
}

var (
	PlanBoxStyle = lipgloss.NewStyle().
			Border(lipgloss.DoubleBorder()).
			BorderForeground(lipgloss.Color("99")). // Violet / Magenta
			Padding(1, 2).
			MarginTop(1).
			MarginBottom(1)

	PlanTitleStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("213")).
			Bold(true)

	ProgressBarStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("39")).
				Bold(true)

	BadgeCompleted  = lipgloss.NewStyle().Foreground(lipgloss.Color("15")).Background(lipgloss.Color("48")).Bold(true).Padding(0, 1).Render("DONE")
	BadgeInProgress = lipgloss.NewStyle().Foreground(lipgloss.Color("15")).Background(lipgloss.Color("214")).Bold(true).Padding(0, 1).Render("IN PROGRESS")
	BadgePending    = lipgloss.NewStyle().Foreground(lipgloss.Color("15")).Background(lipgloss.Color("240")).Bold(true).Padding(0, 1).Render("PENDING")
	BadgeFailed     = lipgloss.NewStyle().Foreground(lipgloss.Color("15")).Background(lipgloss.Color("196")).Bold(true).Padding(0, 1).Render("FAILED")

	TaskIdStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("147")).Bold(true)
	TaskDescStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("255"))
)

func (p *Plan) RenderTUI() string {
	var sb strings.Builder

	sb.WriteString(PlanTitleStyle.Render("📋  EXECUTION PLAN: "+p.Title) + "\n")

	completed, total, pct := p.Progress()

	// Build progress bar string: [▓▓▓▓▓▓▓▓░░░░░░░░] 50%
	barWidth := 24
	filled := (pct * barWidth) / 100
	barStr := strings.Repeat("▓", filled) + strings.Repeat("░", barWidth-filled)

	progressLine := fmt.Sprintf("[%s] %d%% (%d/%d tasks completed)\n", barStr, pct, completed, total)
	sb.WriteString(ProgressBarStyle.Render(progressLine) + "\n")

	for _, task := range p.GetTasks() {
		var badge string
		switch task.Status {
		case StatusCompleted:
			badge = BadgeCompleted
		case StatusInProgress:
			badge = BadgeInProgress
		case StatusFailed:
			badge = BadgeFailed
		default:
			badge = BadgePending
		}

		line := fmt.Sprintf("  %s %s %s", TaskIdStyle.Render(fmt.Sprintf("#%02d", task.ID)), badge, TaskDescStyle.Render(task.Description))
		sb.WriteString(line + "\n")
	}

	return PlanBoxStyle.Render(strings.TrimRight(sb.String(), "\n"))
}


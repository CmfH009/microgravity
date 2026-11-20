package tasks

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/purav/microgravity/internal/events"
)

type TaskStatus string

const (
	StatusRunning TaskStatus = "RUNNING"
	StatusDone    TaskStatus = "DONE"
	StatusFailed  TaskStatus = "FAILED"
	StatusKilled  TaskStatus = "KILLED"
)

type TaskInfo struct {
	ID           string     `json:"id"`
	Conversation string     `json:"conversation_id"`
	CommandLine  string     `json:"command_line"`
	Cwd          string     `json:"cwd"`
	Status       TaskStatus `json:"status"`
	CreatedAt    time.Time  `json:"created_at"`
	CompletedAt  time.Time  `json:"completed_at,omitempty"`
	ExitCode     int        `json:"exit_code"`
	LogPath      string     `json:"log_path"`
	cmd          *exec.Cmd
	stdinPipe    io.WriteCloser
	cancel       context.CancelFunc
	mu           sync.RWMutex
}

type TaskResult struct {
	IsBackground bool      `json:"is_background"`
	TaskID       string    `json:"task_id"`
	LogPath      string    `json:"log_path"`
	Output       string    `json:"output"`
	ExitCode     int       `json:"exit_code"`
	CreatedAt    time.Time `json:"created_at"`
	CompletedAt  time.Time `json:"completed_at"`
}

type Manager struct {
	mu         sync.RWMutex
	tasks      map[string]*TaskInfo
	counter    uint64
	brainDir   string
	eventBus   *events.EventBus
	notifyChan chan<- string
}

var globalManager *Manager
var once sync.Once

func GetManager() *Manager {
	once.Do(func() {
		home, _ := os.UserHomeDir()
		brain := filepath.Join(home, ".gemini", "antigravity-cli", "brain")
		globalManager = NewManager(brain, nil, nil)
	})
	return globalManager
}

func NewManager(brainDir string, eb *events.EventBus, notifyChan chan<- string) *Manager {
	if brainDir == "" {
		home, _ := os.UserHomeDir()
		brainDir = filepath.Join(home, ".gemini", "antigravity-cli", "brain")
	}
	return &Manager{
		tasks:      make(map[string]*TaskInfo),
		brainDir:   brainDir,
		eventBus:   eb,
		notifyChan: notifyChan,
	}
}

func (m *Manager) SetNotifyChan(ch chan<- string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.notifyChan = ch
}

func (m *Manager) SetEventBus(eb *events.EventBus) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.eventBus = eb
}

func (m *Manager) Launch(ctx context.Context, cmdLine, cwd string, waitMs int, convID string) (*TaskResult, error) {
	if convID == "" {
		convID = fmt.Sprintf("conv-%d", time.Now().UnixNano()%1000000)
	}
	if cwd == "" {
		cwd, _ = os.Getwd()
	}
	if waitMs <= 0 {
		waitMs = 5000
	}

	taskNum := atomic.AddUint64(&m.counter, 1)
	taskID := fmt.Sprintf("%s/task-%d", convID, taskNum)

	taskLogDir := filepath.Join(m.brainDir, convID, ".system_generated", "tasks")
	_ = os.MkdirAll(taskLogDir, 0755)
	logPath := filepath.Join(taskLogDir, fmt.Sprintf("task-%d.log", taskNum))

	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		return nil, fmt.Errorf("failed to create task log file: %w", err)
	}

	subCtx, cancel := context.WithCancel(ctx)

	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.CommandContext(subCtx, "powershell", "-NoProfile", "-NonInteractive", "-Command", cmdLine)
	} else {
		cmd = exec.CommandContext(subCtx, "bash", "-c", cmdLine)
	}
	cmd.Dir = cwd

	stdinPipe, _ := cmd.StdinPipe()

	pr, pw := io.Pipe()
	cmd.Stdout = io.MultiWriter(logFile, pw)
	cmd.Stderr = io.MultiWriter(logFile, pw)

	createdAt := time.Now()
	task := &TaskInfo{
		ID:           taskID,
		Conversation: convID,
		CommandLine:  cmdLine,
		Cwd:          cwd,
		Status:       StatusRunning,
		CreatedAt:    createdAt,
		LogPath:      logPath,
		cmd:          cmd,
		stdinPipe:    stdinPipe,
		cancel:       cancel,
	}

	m.mu.Lock()
	m.tasks[taskID] = task
	m.mu.Unlock()

	if err := cmd.Start(); err != nil {
		cancel()
		logFile.Close()
		pw.Close()
		pr.Close()
		return nil, fmt.Errorf("failed to start command: %w", err)
	}

	doneChan := make(chan error, 1)
	var outputBuf strings.Builder
	var outMu sync.Mutex

	outputDone := make(chan struct{})
	go func() {
		defer close(outputDone)
		scanner := bufio.NewScanner(pr)
		for scanner.Scan() {
			outMu.Lock()
			outputBuf.WriteString(scanner.Text() + "\n")
			outMu.Unlock()
		}
	}()

	go func() {
		waitErr := cmd.Wait()
		_ = pw.Close()
		_ = logFile.Close()
		_ = stdinPipe.Close()
		doneChan <- waitErr
	}()

	select {
	case err := <-doneChan:
		<-outputDone
		completedAt := time.Now()
		task.mu.Lock()
		task.CompletedAt = completedAt
		if err != nil {
			if exitErr, ok := err.(*exec.ExitError); ok {
				task.ExitCode = exitErr.ExitCode()
			} else {
				task.ExitCode = 1
			}
			task.Status = StatusFailed
		} else {
			task.ExitCode = 0
			task.Status = StatusDone
		}
		task.mu.Unlock()

		outMu.Lock()
		out := outputBuf.String()
		outMu.Unlock()

		return &TaskResult{
			IsBackground: false,
			TaskID:       taskID,
			LogPath:      logPath,
			Output:       out,
			ExitCode:     task.ExitCode,
			CreatedAt:    createdAt,
			CompletedAt:  completedAt,
		}, nil

	case <-time.After(time.Duration(waitMs) * time.Millisecond):
		// Transition to background task
		go func() {
			waitErr := <-doneChan
			completedAt := time.Now()
			task.mu.Lock()
			task.CompletedAt = completedAt
			if waitErr != nil {
				if exitErr, ok := waitErr.(*exec.ExitError); ok {
					task.ExitCode = exitErr.ExitCode()
				} else {
					task.ExitCode = 1
				}
				if subCtx.Err() == context.Canceled {
					task.Status = StatusKilled
				} else {
					task.Status = StatusFailed
				}
			} else {
				task.ExitCode = 0
				task.Status = StatusDone
			}
			task.mu.Unlock()

			outMu.Lock()
			finalOut := outputBuf.String()
			outMu.Unlock()

			task.mu.RLock()
			finalStatus := task.Status
			finalExitCode := task.ExitCode
			task.mu.RUnlock()
			notification := fmt.Sprintf(`[Message] timestamp=%s sender=%s priority=MESSAGE_PRIORITY_HIGH content=Task id "%s" finished with result:

The command exited with code %d.
Status: %s
Output:
%s`, completedAt.Format(time.RFC3339), taskID, taskID, finalExitCode, finalStatus, finalOut)

			m.mu.RLock()
			notifyChan := m.notifyChan
			eventBus := m.eventBus
			m.mu.RUnlock()
			if notifyChan != nil {
				select {
				case notifyChan <- notification:
				default:
				}
			}
			if eventBus != nil {
				eventBus.Publish("task_completed", notification)
			}
		}()

		return &TaskResult{
			IsBackground: true,
			TaskID:       taskID,
			LogPath:      logPath,
			CreatedAt:    createdAt,
		}, nil
	}
}

func (m *Manager) List() []*TaskInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()

	res := make([]*TaskInfo, 0, len(m.tasks))
	for _, t := range m.tasks {
		res = append(res, t)
	}
	return res
}

func (m *Manager) Get(taskID string) (*TaskInfo, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	t, ok := m.tasks[taskID]
	return t, ok
}

func (m *Manager) Kill(taskID string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	t, ok := m.tasks[taskID]
	if !ok {
		return false
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	if t.Status == StatusRunning {
		if t.cancel != nil {
			t.cancel()
		}
		t.Status = StatusKilled
		t.CompletedAt = time.Now()
		return true
	}
	return false
}

func (m *Manager) SendInput(taskID, input string) error {
	m.mu.RLock()
	t, ok := m.tasks[taskID]
	m.mu.RUnlock()

	if !ok {
		return fmt.Errorf("task %q not found", taskID)
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	if t.Status != StatusRunning {
		return fmt.Errorf("task %q is not running (status: %s)", taskID, t.Status)
	}
	if t.stdinPipe == nil {
		return fmt.Errorf("task %q stdin is not open", taskID)
	}

	_, err := io.WriteString(t.stdinPipe, input+"\n")
	return err
}

func (m *Manager) GetStatusOutput(taskID string) (string, error) {
	t, ok := m.Get(taskID)
	if !ok {
		return "", fmt.Errorf("task %q not found", taskID)
	}

	t.mu.RLock()
	defer t.mu.RUnlock()

	logContent := ""
	if data, err := os.ReadFile(t.LogPath); err == nil {
		logContent = string(data)
		if len(logContent) > 2000 {
			logContent = "... [truncated]\n" + logContent[len(logContent)-2000:]
		}
	}

	return fmt.Sprintf("Task: %s\nStatus: %s\nLog: %s\nLog output:\n%s", t.ID, t.Status, t.LogPath, logContent), nil
}

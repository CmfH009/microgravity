package lock

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
)

type FileLock struct {
	path string
	file *os.File
}

func Acquire(name string) (*FileLock, error) {
	tmpDir := os.TempDir()
	lockPath := filepath.Join(tmpDir, fmt.Sprintf("mg_%s.lock", name))

	// Check if old process alive
	if data, err := os.ReadFile(lockPath); err == nil {
		pid, parseErr := strconv.Atoi(string(data))
		if parseErr != nil || processExists(pid) {
			return nil, fmt.Errorf("lock active: %s", lockPath)
		}
		_ = os.Remove(lockPath)
	}

	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0666)
	if err != nil {
		return nil, fmt.Errorf("lock active: %w", err)
	}

	_, _ = f.WriteString(strconv.Itoa(os.Getpid()))
	return &FileLock{path: lockPath, file: f}, nil
}

func processExists(pid int) bool {
	if pid <= 0 {
		return false
	}
	p, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	if p == nil {
		return false
	}
	if pid == os.Getpid() {
		return true
	}
	return false
}

func (l *FileLock) Release() {
	if l == nil {
		return
	}
	if l.file != nil {
		_ = l.file.Close()
	}
	_ = os.Remove(l.path)
}

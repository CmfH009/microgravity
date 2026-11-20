package sessions

import (
	"os"
	"path/filepath"
	"strings"
	"time"
)

type SessionInfo struct {
	Name    string
	Size    int64
	ModTime time.Time
	Snippet string
}

type Manager struct {
	dir string
}

func NewManager(dirs ...string) *Manager {
	var dir string
	if len(dirs) > 0 {
		dir = dirs[0]
	} else {
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, ".microgravity")
	}
	os.MkdirAll(dir, 0755)
	return &Manager{dir: dir}
}

func (m *Manager) SaveSession(name, data string) error {
	path := filepath.Join(m.dir, "history_"+name+".txt")
	return os.WriteFile(path, []byte(data), 0644)
}

func (m *Manager) GetSessionPath(name string) string {
	return filepath.Join(m.dir, "history_"+name+".txt")
}

func (m *Manager) ListSessions() ([]SessionInfo, error) {
	files, err := os.ReadDir(m.dir)
	if err != nil {
		return nil, err
	}

	var sessions []SessionInfo
	for _, f := range files {
		if !f.IsDir() && strings.HasPrefix(f.Name(), "history_") && strings.HasSuffix(f.Name(), ".txt") {
			name := strings.TrimPrefix(f.Name(), "history_")
			name = strings.TrimSuffix(name, ".txt")
			
			filePath := filepath.Join(m.dir, f.Name())
			info, err := f.Info()
			if err != nil {
				continue
			}

			// Extract snippet
			snippet := ""
			content, err := os.ReadFile(filePath)
			if err == nil {
				lines := strings.Split(string(content), "\n")
				for _, l := range lines {
					trimmed := strings.TrimSpace(l)
					if trimmed != "" {
						if len(trimmed) > 80 {
							snippet = trimmed[:80] + "..."
						} else {
							snippet = trimmed
						}
						break
					}
				}
			}
			
			sessions = append(sessions, SessionInfo{
				Name:    name,
				Size:    info.Size(),
				ModTime: info.ModTime(),
				Snippet: snippet,
			})
		}
	}
	return sessions, nil
}

func (m *Manager) ExportSession(name, exportFilePath string) error {
	srcPath := m.GetSessionPath(name)
	data, err := os.ReadFile(srcPath)
	if err != nil {
		return err
	}
	return os.WriteFile(exportFilePath, data, 0644)
}

func (m *Manager) ImportSession(importFilePath, name string) error {
	data, err := os.ReadFile(importFilePath)
	if err != nil {
		return err
	}
	return m.SaveSession(name, string(data))
}

func (m *Manager) DeleteSession(name string) error {
	path := m.GetSessionPath(name)
	return os.Remove(path)
}

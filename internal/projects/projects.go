package projects

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type ProjectEntry struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Path      string    `json:"path"`
	CreatedAt time.Time `json:"createdAt"`
	LastSeen  time.Time `json:"lastSeen"`
}

type Cache struct {
	Projects map[string]ProjectEntry `json:"projects"`
}

type Manager struct {
	mu        sync.RWMutex
	cacheFile string
}

func NewManager() (*Manager, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	cacheDir := filepath.Join(home, ".gemini", "antigravity-cli", "cache")
	_ = os.MkdirAll(cacheDir, 0755)
	return &Manager{
		cacheFile: filepath.Join(cacheDir, "projects.json"),
	}, nil
}

func (m *Manager) Load() (*Cache, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	data, err := os.ReadFile(m.cacheFile)
	if err != nil {
		return &Cache{Projects: make(map[string]ProjectEntry)}, nil
	}

	var cache Cache
	if err := json.Unmarshal(data, &cache); err != nil {
		return &Cache{Projects: make(map[string]ProjectEntry)}, nil
	}
	if cache.Projects == nil {
		cache.Projects = make(map[string]ProjectEntry)
	}
	return &cache, nil
}

func (m *Manager) Save(cache *Cache) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	data, err := json.MarshalIndent(cache, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(m.cacheFile, data, 0644)
}

func (m *Manager) RegisterProject(path, name string) (string, error) {
	absPath, err := filepath.Abs(path)
	if err != nil {
		absPath = path
	}

	cache, _ := m.Load()
	if name == "" {
		name = filepath.Base(absPath)
	}

	for _, p := range cache.Projects {
		if p.Path == absPath {
			p.LastSeen = time.Now()
			cache.Projects[p.ID] = p
			_ = m.Save(cache)
			return p.ID, nil
		}
	}

	id := filepath.Base(absPath)
	now := time.Now()
	entry := ProjectEntry{
		ID:        id,
		Name:      name,
		Path:      absPath,
		CreatedAt: now,
		LastSeen:  now,
	}

	cache.Projects[id] = entry
	_ = m.Save(cache)
	return id, nil
}

func (m *Manager) GetProject(path string) (*ProjectEntry, bool) {
	absPath, err := filepath.Abs(path)
	if err != nil {
		absPath = path
	}

	cache, _ := m.Load()
	for _, p := range cache.Projects {
		if p.Path == absPath || p.ID == path {
			return &p, true
		}
	}
	return nil, false
}

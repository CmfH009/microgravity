package history

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

var CurrentSession = "default"

func GetHistoryDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(home, ".microgravity")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", err
	}
	return dir, nil
}

func GetHistoryPath() (string, error) {
	dir, err := GetHistoryDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "history_"+CurrentSession+".txt"), nil
}

func LoadHistory() (string, error) {
	path, err := GetHistoryPath()
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	return string(data), nil
}

func SaveHistory(content string) error {
	path, err := GetHistoryPath()
	if err != nil {
		return err
	}
	return os.WriteFile(path, []byte(content), 0600)
}

func ClearHistory() error {
	path, err := GetHistoryPath()
	if err != nil {
		return err
	}
	err = os.Remove(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func ListSessions() ([]string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	dir := filepath.Join(home, ".microgravity")
	files, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var sessions []string
	for _, f := range files {
		if !f.IsDir() && strings.HasPrefix(f.Name(), "history_") && strings.HasSuffix(f.Name(), ".txt") {
			name := strings.TrimPrefix(f.Name(), "history_")
			name = strings.TrimSuffix(name, ".txt")
			sessions = append(sessions, name)
		}
	}
	return sessions, nil
}

type HistoryEntry struct {
	UserText string   `json:"user_text"`
	Images   []string `json:"images"`
}

type History struct {
	dir string
}

func NewHistoryWithDir(dir string) *History {
	return &History{dir: dir}
}

func (h *History) SaveEntry(entry HistoryEntry) error {
	path := filepath.Join(h.dir, "history_vision_"+CurrentSession+".jsonl")
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()

	b, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	_, err = f.Write(append(b, '\n'))
	return err
}

func (h *History) LoadEntries() ([]HistoryEntry, error) {
	path := filepath.Join(h.dir, "history_vision_"+CurrentSession+".jsonl")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var entries []HistoryEntry
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		var e HistoryEntry
		if err := json.Unmarshal(scanner.Bytes(), &e); err == nil {
			entries = append(entries, e)
		}
	}
	return entries, scanner.Err()
}

package indexer

import (
	"encoding/json"
	"os"
	"path/filepath"
)

type FileMeta struct {
	Hash       string `json:"hash"`
	ModTime    int64  `json:"mod_time"`
	ChunkCount int    `json:"chunk_count"`
}

type Manifest struct {
	Files map[string]FileMeta `json:"files"`
}

func LoadManifest(manifestPath string) (*Manifest, error) {
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return &Manifest{Files: make(map[string]FileMeta)}, nil
	}
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return &Manifest{Files: make(map[string]FileMeta)}, nil
	}
	if m.Files == nil {
		m.Files = make(map[string]FileMeta)
	}
	return &m, nil
}

func (m *Manifest) Save(manifestPath string) error {
	dir := filepath.Dir(manifestPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(manifestPath, data, 0644)
}

package projects

import (
	"path/filepath"
	"testing"
)

func TestProjectsCache(t *testing.T) {
	tempDir := t.TempDir()
	mgr := &Manager{
		cacheFile: filepath.Join(tempDir, "projects.json"),
	}

	testPath := filepath.Join(tempDir, "my-app")
	id, err := mgr.RegisterProject(testPath, "My App")
	if err != nil {
		t.Fatalf("failed to register project: %v", err)
	}
	if id != "my-app" {
		t.Errorf("expected project ID 'my-app', got %s", id)
	}

	p, found := mgr.GetProject(testPath)
	if !found || p.Name != "My App" {
		t.Errorf("project lookup failed: %+v", p)
	}
}

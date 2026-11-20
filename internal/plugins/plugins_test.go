package plugins

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPluginManifestAndValidation(t *testing.T) {
	tempDir := t.TempDir()
	sourceDir := filepath.Join(tempDir, "source-plugin")
	os.MkdirAll(sourceDir, 0755)

	manifestPath := filepath.Join(sourceDir, "plugin.json")
	content := `{
		"name": "test-plugin",
		"version": "1.2.0",
		"description": "A test plugin for Microgravity"
	}`
	if err := os.WriteFile(manifestPath, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write plugin.json: %v", err)
	}

	manifest, err := LoadManifest(sourceDir)
	if err != nil {
		t.Fatalf("failed to load manifest: %v", err)
	}
	if manifest.Name != "test-plugin" || manifest.Version != "1.2.0" {
		t.Errorf("manifest data mismatch: %+v", manifest)
	}

	mgr := &Manager{
		pluginsDir: filepath.Join(tempDir, "installed"),
		configFile: filepath.Join(tempDir, "plugins.json"),
	}
	os.MkdirAll(mgr.pluginsDir, 0755)

	if err := mgr.Validate(sourceDir); err != nil {
		t.Errorf("expected validation to succeed, got %v", err)
	}

	// Install plugin
	installed, err := mgr.Install(sourceDir)
	if err != nil {
		t.Fatalf("failed to install plugin: %v", err)
	}
	if installed.Name != "test-plugin" {
		t.Errorf("expected installed plugin name test-plugin, got %s", installed.Name)
	}

	// Disable and enable plugin
	if err := mgr.Disable("test-plugin"); err != nil {
		t.Fatalf("failed to disable plugin: %v", err)
	}
	list, _ := mgr.List()
	if len(list) == 0 || list[0].Enabled {
		t.Errorf("expected plugin to be disabled")
	}

	if err := mgr.Enable("test-plugin"); err != nil {
		t.Fatalf("failed to enable plugin: %v", err)
	}
	list, _ = mgr.List()
	if len(list) == 0 || !list[0].Enabled {
		t.Errorf("expected plugin to be enabled")
	}

	// Uninstall plugin
	if err := mgr.Uninstall("test-plugin"); err != nil {
		t.Fatalf("failed to uninstall plugin: %v", err)
	}
	list, _ = mgr.List()
	if len(list) != 0 {
		t.Errorf("expected empty plugin list after uninstall")
	}
}

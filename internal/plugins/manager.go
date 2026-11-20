package plugins

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

type Manager struct {
	pluginsDir string
	configFile string
}

func NewManager() (*Manager, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	pluginsDir := filepath.Join(home, ".gemini", "config", "plugins")
	_ = os.MkdirAll(pluginsDir, 0755)

	configFile := filepath.Join(filepath.Dir(pluginsDir), "plugins.json")
	return &Manager{
		pluginsDir: pluginsDir,
		configFile: configFile,
	}, nil
}

func (m *Manager) LoadConfig() (*PluginsConfig, error) {
	data, err := os.ReadFile(m.configFile)
	if err != nil {
		return &PluginsConfig{Imports: []PluginImport{}, Disabled: make(map[string]bool)}, nil
	}
	var cfg PluginsConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return &PluginsConfig{Imports: []PluginImport{}, Disabled: make(map[string]bool)}, nil
	}
	if cfg.Disabled == nil {
		cfg.Disabled = make(map[string]bool)
	}
	return &cfg, nil
}

func (m *Manager) SaveConfig(cfg *PluginsConfig) error {
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(m.configFile, data, 0644)
}

func (m *Manager) List() ([]PluginInfo, error) {
	cfg, _ := m.LoadConfig()
	var results []PluginInfo

	entries, err := os.ReadDir(m.pluginsDir)
	if err == nil {
		for _, e := range entries {
			if e.IsDir() {
				pluginPath := filepath.Join(m.pluginsDir, e.Name())
				manifest, err := LoadManifest(pluginPath)
				comps := DetectComponents(pluginPath)
				desc := ""
				ver := "1.0.0"
				if err == nil && manifest != nil {
					if manifest.Description != "" {
						desc = manifest.Description
					}
					if manifest.Version != "" {
						ver = manifest.Version
					}
				}
				enabled := true
				if cfg.Disabled[e.Name()] {
					enabled = false
				}

				results = append(results, PluginInfo{
					Name:        e.Name(),
					Version:     ver,
					Description: desc,
					Source:      "local",
					ImportedAt:  time.Now().Format(time.RFC3339),
					Components:  comps,
					Enabled:     enabled,
					Path:        pluginPath,
				})
			}
		}
	}

	return results, nil
}

func (m *Manager) Install(target string) (*PluginInfo, error) {
	fileInfo, err := os.Stat(target)
	if err != nil || !fileInfo.IsDir() {
		return nil, fmt.Errorf("install target must be a directory: %s", target)
	}

	manifest, err := LoadManifest(target)
	if err != nil {
		return nil, err
	}

	if !validPluginName(manifest.Name) {
		return nil, fmt.Errorf("invalid plugin name %q", manifest.Name)
	}
	destDir := filepath.Join(m.pluginsDir, manifest.Name)
	tmpDir, err := os.MkdirTemp(m.pluginsDir, ".install-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmpDir)
	if err := copyDir(target, tmpDir); err != nil {
		return nil, fmt.Errorf("failed to copy plugin: %w", err)
	}

	if err := os.RemoveAll(destDir); err != nil {
		return nil, fmt.Errorf("replace existing plugin: %w", err)
	}
	if err := os.Rename(tmpDir, destDir); err != nil {
		return nil, fmt.Errorf("activate plugin: %w", err)
	}
	comps := DetectComponents(destDir)
	cfg, _ := m.LoadConfig()
	cfg.Imports = append(cfg.Imports, PluginImport{
		Name:       manifest.Name,
		Source:     target,
		ImportedAt: time.Now().Format(time.RFC3339),
		Components: comps,
	})
	_ = m.SaveConfig(cfg)

	return &PluginInfo{
		Name:        manifest.Name,
		Version:     manifest.Version,
		Description: manifest.Description,
		Source:      target,
		ImportedAt:  time.Now().Format(time.RFC3339),
		Components:  comps,
		Enabled:     true,
		Path:        destDir,
	}, nil
}

func (m *Manager) Uninstall(name string) error {
	if !validPluginName(name) {
		return fmt.Errorf("invalid plugin name %q", name)
	}
	destDir := filepath.Join(m.pluginsDir, name)
	if err := os.RemoveAll(destDir); err != nil {
		return fmt.Errorf("failed to remove plugin directory: %w", err)
	}

	cfg, _ := m.LoadConfig()
	var updatedImports []PluginImport
	for _, imp := range cfg.Imports {
		if imp.Name != name {
			updatedImports = append(updatedImports, imp)
		}
	}
	cfg.Imports = updatedImports
	delete(cfg.Disabled, name)
	return m.SaveConfig(cfg)
}

func (m *Manager) Enable(name string) error {
	destDir := filepath.Join(m.pluginsDir, name)
	if _, err := os.Stat(destDir); os.IsNotExist(err) {
		return fmt.Errorf("plugin %q not found or invalid", name)
	}

	cfg, _ := m.LoadConfig()
	delete(cfg.Disabled, name)
	return m.SaveConfig(cfg)
}

func (m *Manager) Disable(name string) error {
	destDir := filepath.Join(m.pluginsDir, name)
	if _, err := os.Stat(destDir); os.IsNotExist(err) {
		return fmt.Errorf("plugin %q not found or invalid", name)
	}

	cfg, _ := m.LoadConfig()
	cfg.Disabled[name] = true
	return m.SaveConfig(cfg)
}

func (m *Manager) Validate(path string) error {
	if path == "" {
		path = "."
	}
	manifest, err := LoadManifest(path)
	if err != nil {
		return err
	}
	if manifest.Name == "" {
		return fmt.Errorf("plugin manifest missing 'name' field")
	}
	return nil
}

func (m *Manager) Import(source string) (int, error) {
	home, _ := os.UserHomeDir()
	var scanPath string
	srcName := source
	switch strings.ToLower(source) {
	case "gemini", "gemini-cli":
		scanPath = filepath.Join(home, ".gemini", "config", "plugins")
		srcName = "gemini-cli"
	case "claude", "claude-code":
		scanPath = filepath.Join(home, ".claude", "plugins")
		srcName = "claude-code"
	default:
		scanPath = source
	}

	stat, err := os.Stat(scanPath)
	if err != nil || !stat.IsDir() {
		return 0, fmt.Errorf("unknown import source or invalid path: %s", source)
	}

	entries, err := os.ReadDir(scanPath)
	if err != nil {
		return 0, err
	}

	count := 0
	for _, e := range entries {
		if e.IsDir() {
			srcDir := filepath.Join(scanPath, e.Name())
			destDir := filepath.Join(m.pluginsDir, e.Name())
			if srcDir != destDir {
				_ = copyDir(srcDir, destDir)
			}
			count++
		}
	}

	cfg, _ := m.LoadConfig()
	for _, e := range entries {
		if e.IsDir() {
			comps := DetectComponents(filepath.Join(m.pluginsDir, e.Name()))
			cfg.Imports = append(cfg.Imports, PluginImport{
				Name:       e.Name(),
				Source:     srcName,
				ImportedAt: time.Now().Format(time.RFC3339),
				Components: comps,
			})
		}
	}
	_ = m.SaveConfig(cfg)

	return count, nil
}

var pluginNamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)

func validPluginName(name string) bool {
	return pluginNamePattern.MatchString(name) && name != "." && name != ".."
}

func directoryDigest(dir string) (string, error) {
	h := sha256.New()
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		_, _ = h.Write([]byte(path))
		_, _ = h.Write(data)
		return nil
	})
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", h.Sum(nil)), nil
}

func copyDir(src, dst string) error {
	absSrc, err := filepath.Abs(src)
	if err != nil {
		return err
	}
	absDst, err := filepath.Abs(dst)
	if err != nil {
		return err
	}

	return filepath.Walk(absSrc, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		// Avoid recursive descent if destination is inside source
		if path == absDst || (strings.HasPrefix(path, absDst+string(filepath.Separator))) {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		rel, err := filepath.Rel(absSrc, path)
		if err != nil {
			return err
		}
		targetPath := filepath.Join(absDst, rel)
		if info.IsDir() {
			return os.MkdirAll(targetPath, 0755)
		}
		return copyFile(path, targetPath)
	})
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, in)
	return err
}

package sidecar

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

// FindPython discovers the preferred Python interpreter executable.
func FindPython() string {
	if envPy := os.Getenv("MG_PYTHON"); envPy != "" {
		if _, err := os.Stat(envPy); err == nil {
			return envPy
		}
	}

	// Check local virtual environments
	cwd, _ := os.Getwd()
	venvPaths := []string{
		filepath.Join(cwd, ".venv", "Scripts", "python.exe"),
		filepath.Join(cwd, ".venv", "bin", "python"),
		filepath.Join("C:\\", "AgyHut", ".venv", "Scripts", "python.exe"),
		filepath.Join("A:\\", ".venv", "Scripts", "python.exe"),
	}

	for _, vp := range venvPaths {
		if _, err := os.Stat(vp); err == nil {
			return vp
		}
	}

	// Check PATH
	candidates := []string{"python", "python3"}
	if runtime.GOOS == "windows" {
		candidates = []string{"python.exe", "python3.exe", "py.exe", "python"}
	}

	for _, name := range candidates {
		if path, err := exec.LookPath(name); err == nil {
			return path
		}
	}

	return "python"
}

// FindScript resolves a Python sidecar script relative to workspace, binary, or python/ directory.
func FindScript(relPath string, fallbackName string) string {
	cwd, _ := os.Getwd()
	searchRoots := []string{cwd}

	if exe, err := os.Executable(); err == nil {
		searchRoots = append(searchRoots, filepath.Dir(exe))
	}

	// Check A:\projects\microgravity and C:\AgyHut\microgravity
	searchRoots = append(searchRoots,
		filepath.Join("A:", "projects", "microgravity"),
		filepath.Join("C:", "AgyHut", "projects", "microgravity"),
		filepath.Join("C:", "AgyHut", "microgravity"),
	)

	for _, root := range searchRoots {
		candidates := []string{
			filepath.Join(root, "python", relPath),
			filepath.Join(root, relPath),
			filepath.Join(root, fallbackName),
			filepath.Join(root, "scripts", fallbackName),
		}

		for _, cand := range candidates {
			if _, err := os.Stat(cand); err == nil {
				return cand
			}
		}

		// Also check parent directories up to 4 levels
		dir := root
		for i := 0; i < 4; i++ {
			parent := filepath.Dir(dir)
			if parent == dir {
				break
			}
			cand := filepath.Join(parent, "python", relPath)
			if _, err := os.Stat(cand); err == nil {
				return cand
			}
			candRoot := filepath.Join(parent, fallbackName)
			if _, err := os.Stat(candRoot); err == nil {
				return candRoot
			}
			dir = parent
		}
	}

	return fallbackName
}

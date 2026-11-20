package sidecar

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

var (
	osGetwd      = os.Getwd
	osExecutable = os.Executable
	lookPath     = exec.LookPath
	currentGOOS  = runtime.GOOS
)

// FindPython discovers the preferred Python interpreter executable.
// Search order:
//  1. MG_PYTHON environment variable
//  2. VIRTUAL_ENV environment variable (Scripts/python.exe or bin/python)
//  3. Local .venv in current working directory or executable directory
//  4. System PATH candidates
//  5. Fallback to "python"
func FindPython() string {
	if envPy := os.Getenv("MG_PYTHON"); envPy != "" {
		if isExistingFile(envPy) {
			return envPy
		}
		if !strings.ContainsAny(envPy, "/\\") {
			if path, err := lookPath(envPy); err == nil {
				return path
			}
		}
	}

	if venv := os.Getenv("VIRTUAL_ENV"); venv != "" {
		if py := findVenvPython(venv); py != "" {
			return py
		}
	}

	for _, root := range localSearchRoots() {
		if py := findVenvPython(filepath.Join(root, ".venv")); py != "" {
			return py
		}
	}

	candidates := []string{"python3", "python"}
	if currentGOOS == "windows" {
		candidates = []string{"python.exe", "python3.exe", "py.exe", "python"}
	}

	for _, name := range candidates {
		if path, err := lookPath(name); err == nil {
			return path
		}
	}

	return "python"
}

// FindScript resolves a Python sidecar script relative to configured environment
// directories, working directory, executable directory, or up to 4 parent directories.
func FindScript(relPath string, fallbackName string) string {
	if sidecarDir := os.Getenv("MG_SIDECAR_DIR"); sidecarDir != "" {
		if found := findScriptInDir(sidecarDir, relPath, fallbackName, true); found != "" {
			return found
		}
	}

	if mgHome := os.Getenv("MG_HOME"); mgHome != "" {
		if found := findScriptInDir(mgHome, relPath, fallbackName, false); found != "" {
			return found
		}
	}

	roots := localSearchRoots()
	seen := make(map[string]struct{}, len(roots)*5)

	for _, root := range roots {
		cleanRoot := filepath.Clean(root)
		if _, ok := seen[cleanRoot]; ok {
			continue
		}
		seen[cleanRoot] = struct{}{}
		if found := findScriptInDir(cleanRoot, relPath, fallbackName, false); found != "" {
			return found
		}
	}

	for _, root := range roots {
		dir := filepath.Clean(root)
		for i := 0; i < 4; i++ {
			parent := filepath.Dir(dir)
			if parent == dir {
				break
			}
			if _, ok := seen[parent]; !ok {
				seen[parent] = struct{}{}
				if found := findScriptInDir(parent, relPath, fallbackName, false); found != "" {
					return found
				}
			}
			dir = parent
		}
	}

	return fallbackName
}

func localSearchRoots() []string {
	var roots []string
	seen := make(map[string]struct{}, 2)

	if cwd, err := osGetwd(); err == nil && cwd != "" {
		clean := filepath.Clean(cwd)
		seen[clean] = struct{}{}
		roots = append(roots, clean)
	}

	if exe, err := osExecutable(); err == nil && exe != "" {
		exeDir := filepath.Clean(filepath.Dir(exe))
		if _, ok := seen[exeDir]; !ok {
			roots = append(roots, exeDir)
		}
	}

	return roots
}

func findVenvPython(venvDir string) string {
	if venvDir == "" {
		return ""
	}
	candidates := []string{
		filepath.Join(venvDir, "bin", "python"),
		filepath.Join(venvDir, "Scripts", "python.exe"),
	}
	if currentGOOS == "windows" {
		candidates = []string{
			filepath.Join(venvDir, "Scripts", "python.exe"),
			filepath.Join(venvDir, "bin", "python"),
		}
	}
	for _, cand := range candidates {
		if isExistingFile(cand) {
			return cand
		}
	}
	return ""
}

func findScriptInDir(dir, relPath, fallbackName string, directFirst bool) string {
	var candidates []string
	if directFirst {
		if relPath != "" {
			candidates = append(candidates, filepath.Join(dir, relPath))
		}
		if fallbackName != "" {
			candidates = append(candidates, filepath.Join(dir, fallbackName))
		}
		if relPath != "" {
			candidates = append(candidates, filepath.Join(dir, "python", relPath))
		}
		if fallbackName != "" {
			candidates = append(candidates, filepath.Join(dir, "scripts", fallbackName))
		}
	} else {
		if relPath != "" {
			candidates = append(candidates,
				filepath.Join(dir, "python", relPath),
				filepath.Join(dir, relPath),
			)
		}
		if fallbackName != "" {
			candidates = append(candidates,
				filepath.Join(dir, fallbackName),
				filepath.Join(dir, "scripts", fallbackName),
			)
		}
	}

	for _, cand := range candidates {
		if isExistingFile(cand) {
			return cand
		}
	}
	return ""
}

func isExistingFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

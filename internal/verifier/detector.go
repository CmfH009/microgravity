package verifier

import (
	"os"
	"path/filepath"
	"strings"
)

// DetectFrameworks scans the workspace directory for test runners across
// Go, TypeScript/JavaScript (vitest, jest, npm), Python (pytest), and Rust (cargo).
func (v *Verifier) DetectFrameworks(cwd string) []FrameworkInfo {
	var frameworks []FrameworkInfo

	// 1. Go project (go.mod in cwd, or non-root cwd containing .go files)
	if fileExists(cwd, "go.mod") {
		frameworks = append(frameworks, FrameworkInfo{
			Type:    FrameworkGo,
			Command: "go",
			Args:    []string{"test", "./..."},
		})
	}

	// 2. TypeScript / JavaScript project (package.json)
	if fileExists(cwd, "package.json") {
		pkgData, _ := os.ReadFile(filepath.Join(cwd, "package.json"))
		pkgStr := string(pkgData)

		hasVitestConfig := fileExists(cwd, "vitest.config.ts") ||
			fileExists(cwd, "vitest.config.js") ||
			fileExists(cwd, "vitest.config.mts") ||
			fileExists(cwd, "vitest.config.cjs") ||
			fileExists(cwd, "vitest.config.mjs")

		hasJestConfig := fileExists(cwd, "jest.config.js") ||
			fileExists(cwd, "jest.config.ts") ||
			fileExists(cwd, "jest.config.json") ||
			fileExists(cwd, "jest.config.cjs") ||
			fileExists(cwd, "jest.config.mjs")

		if hasVitestConfig || strings.Contains(pkgStr, `"vitest"`) {
			frameworks = append(frameworks, FrameworkInfo{
				Type:    FrameworkVitest,
				Command: "npx",
				Args:    []string{"vitest", "run"},
			})
		} else if hasJestConfig || strings.Contains(pkgStr, `"jest"`) {
			frameworks = append(frameworks, FrameworkInfo{
				Type:    FrameworkJest,
				Command: "npx",
				Args:    []string{"jest"},
			})
		} else {
			frameworks = append(frameworks, FrameworkInfo{
				Type:    FrameworkNPM,
				Command: "npm",
				Args:    []string{"test"},
			})
		}
	}

	// 3. Python project (pytest.ini, pyproject.toml, setup.py, requirements.txt, or tests/ directly in cwd)
	if fileExists(cwd, "pytest.ini") ||
		fileExists(cwd, "pyproject.toml") ||
		fileExists(cwd, "setup.py") ||
		fileExists(cwd, "requirements.txt") ||
		fileExists(cwd, "Pipfile") ||
		fileExists(cwd, "tox.ini") ||
		dirExists(cwd, "tests") {
		frameworks = append(frameworks, FrameworkInfo{
			Type:    FrameworkPytest,
			Command: "pytest",
			Args:    []string{},
		})
	}

	// 4. Rust project (Cargo.toml)
	if fileExists(cwd, "Cargo.toml") {
		frameworks = append(frameworks, FrameworkInfo{
			Type:    FrameworkCargo,
			Command: "cargo",
			Args:    []string{"test"},
		})
	}

	return frameworks
}

func fileExists(dir, filename string) bool {
	info, err := os.Stat(filepath.Join(dir, filename))
	return err == nil && !info.IsDir()
}

func dirExists(dir, dirname string) bool {
	info, err := os.Stat(filepath.Join(dir, dirname))
	return err == nil && info.IsDir()
}

func hasExtension(dir, ext string) bool {
	found := false
	_ = filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if !info.IsDir() && strings.HasSuffix(info.Name(), ext) {
			found = true
			return filepath.SkipAll
		}
		// Skip hidden directories and node_modules / target / .git
		if info.IsDir() && (strings.HasPrefix(info.Name(), ".") || info.Name() == "node_modules" || info.Name() == "target" || info.Name() == "vendor") {
			return filepath.SkipDir
		}
		return nil
	})
	return found
}

func hasPythonTests(dir string) bool {
	if dirExists(dir, "tests") {
		return true
	}
	found := false
	_ = filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if !info.IsDir() {
			name := info.Name()
			if (strings.HasPrefix(name, "test_") && strings.HasSuffix(name, ".py")) ||
				(strings.HasSuffix(name, "_test.py")) {
				found = true
				return filepath.SkipAll
			}
		}
		if info.IsDir() && (strings.HasPrefix(info.Name(), ".") || info.Name() == "__pycache__" || info.Name() == "venv" || info.Name() == ".venv") {
			return filepath.SkipDir
		}
		return nil
	})
	return found
}

package sidecar

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

func isolateLocator(t *testing.T) (cwdDir, exeDir string) {
	t.Helper()

	cwdDir = t.TempDir()
	exeDir = t.TempDir()
	emptyPathDir := t.TempDir()

	t.Setenv("MG_PYTHON", "")
	t.Setenv("VIRTUAL_ENV", "")
	t.Setenv("MG_SIDECAR_DIR", "")
	t.Setenv("MG_HOME", "")
	t.Setenv("PATH", emptyPathDir)

	t.Chdir(cwdDir)

	origGetwd := osGetwd
	origExecutable := osExecutable
	origLookPath := lookPath
	origGOOS := currentGOOS
	t.Cleanup(func() {
		osGetwd = origGetwd
		osExecutable = origExecutable
		lookPath = origLookPath
		currentGOOS = origGOOS
	})

	osExecutable = func() (string, error) {
		return filepath.Join(exeDir, "mg.exe"), nil
	}
	lookPath = func(file string) (string, error) {
		return "", errors.New("not found")
	}

	return cwdDir, exeDir
}

func touchFile(t *testing.T, path string) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll(%q): %v", path, err)
	}
	if err := os.WriteFile(path, []byte("stub"), 0o755); err != nil {
		t.Fatalf("WriteFile(%q): %v", path, err)
	}
	return path
}

func TestNoHardcodedMachinePathsInLocator(t *testing.T) {
	data, err := os.ReadFile("locator.go")
	if err != nil {
		t.Fatalf("failed to read locator.go: %v", err)
	}
	lower := strings.ToLower(string(data))
	forbidden := []string{
		`a:\`,
		`a:/`,
		`c:\agyhut`,
		`c:/agyhut`,
		`agyhut`,
	}
	for _, needle := range forbidden {
		if strings.Contains(lower, needle) {
			t.Fatalf("locator.go contains forbidden hardcoded machine path %q", needle)
		}
	}
}

func TestFindPython_MGPython(t *testing.T) {
	cwdDir, _ := isolateLocator(t)

	// Seed VIRTUAL_ENV and local .venv to confirm MG_PYTHON takes precedence.
	venvDir := t.TempDir()
	touchFile(t, filepath.Join(venvDir, "Scripts", "python.exe"))
	t.Setenv("VIRTUAL_ENV", venvDir)
	touchFile(t, filepath.Join(cwdDir, ".venv", "Scripts", "python.exe"))

	customPy := touchFile(t, filepath.Join(t.TempDir(), "custom-python"))
	t.Setenv("MG_PYTHON", customPy)
	if got := FindPython(); got != customPy {
		t.Fatalf("FindPython() = %q, want %q", got, customPy)
	}

	// Directory in MG_PYTHON must not be accepted.
	t.Setenv("MG_PYTHON", t.TempDir())
	if got := FindPython(); got == os.Getenv("MG_PYTHON") {
		t.Fatalf("FindPython() accepted directory %q", got)
	}

	// Missing MG_PYTHON path falls through to VIRTUAL_ENV.
	t.Setenv("MG_PYTHON", filepath.Join(t.TempDir(), "missing-python.exe"))
	wantVenv := filepath.Join(venvDir, "Scripts", "python.exe")
	if got := FindPython(); got != wantVenv {
		t.Fatalf("FindPython() = %q, want VIRTUAL_ENV fallback %q", got, wantVenv)
	}

	// Bare command name in MG_PYTHON resolves via lookPath.
	t.Setenv("MG_PYTHON", "my-custom-py")
	lookPath = func(file string) (string, error) {
		if file == "my-custom-py" {
			return filepath.Join("resolved", "my-custom-py"), nil
		}
		return "", errors.New("not found")
	}
	if got := FindPython(); got != filepath.Join("resolved", "my-custom-py") {
		t.Fatalf("FindPython() = %q, want lookPath resolution", got)
	}
}

func TestFindPython_VirtualEnv(t *testing.T) {
	cwdDir, _ := isolateLocator(t)
	touchFile(t, filepath.Join(cwdDir, ".venv", "Scripts", "python.exe"))

	t.Run("Scripts_python_exe", func(t *testing.T) {
		venvDir := t.TempDir()
		want := touchFile(t, filepath.Join(venvDir, "Scripts", "python.exe"))
		t.Setenv("VIRTUAL_ENV", venvDir)
		if got := FindPython(); got != want {
			t.Fatalf("FindPython() = %q, want %q", got, want)
		}
	})

	t.Run("bin_python", func(t *testing.T) {
		venvDir := t.TempDir()
		want := touchFile(t, filepath.Join(venvDir, "bin", "python"))
		t.Setenv("VIRTUAL_ENV", venvDir)
		if got := FindPython(); got != want {
			t.Fatalf("FindPython() = %q, want %q", got, want)
		}
	})
}

func TestFindPython_LocalVenvCwdAndExe(t *testing.T) {
	t.Run("cwd_scripts_python_beats_exe_venv", func(t *testing.T) {
		cwdDir, exeDir := isolateLocator(t)
		cwdPy := touchFile(t, filepath.Join(cwdDir, ".venv", "Scripts", "python.exe"))
		touchFile(t, filepath.Join(exeDir, ".venv", "Scripts", "python.exe"))

		if got := FindPython(); got != cwdPy {
			t.Fatalf("FindPython() = %q, want %q", got, cwdPy)
		}
	})

	t.Run("cwd_bin_python", func(t *testing.T) {
		cwdDir, _ := isolateLocator(t)
		cwdPy := touchFile(t, filepath.Join(cwdDir, ".venv", "bin", "python"))

		if got := FindPython(); got != cwdPy {
			t.Fatalf("FindPython() = %q, want %q", got, cwdPy)
		}
	})

	t.Run("exe_dir_venv_when_cwd_has_none", func(t *testing.T) {
		_, exeDir := isolateLocator(t)
		exePy := touchFile(t, filepath.Join(exeDir, ".venv", "bin", "python"))

		if got := FindPython(); got != exePy {
			t.Fatalf("FindPython() = %q, want %q", got, exePy)
		}
	})
}

func TestFindPython_PathCandidatesAndFallback(t *testing.T) {
	t.Run("windows_candidate_order", func(t *testing.T) {
		isolateLocator(t)
		currentGOOS = "windows"

		var tried []string
		lookPath = func(file string) (string, error) {
			tried = append(tried, file)
			if file == "py.exe" {
				return `C:\Python\py.exe`, nil
			}
			return "", errors.New("not found")
		}

		if got := FindPython(); got != `C:\Python\py.exe` {
			t.Fatalf("FindPython() = %q, want C:\\Python\\py.exe", got)
		}
		wantTried := []string{"python.exe", "python3.exe", "py.exe"}
		if !reflect.DeepEqual(tried, wantTried) {
			t.Fatalf("tried = %v, want %v", tried, wantTried)
		}
	})

	t.Run("unix_candidate_order_and_fallback", func(t *testing.T) {
		isolateLocator(t)
		currentGOOS = "linux"

		var tried []string
		lookPath = func(file string) (string, error) {
			tried = append(tried, file)
			return "", errors.New("not found")
		}

		if got := FindPython(); got != "python" {
			t.Fatalf("FindPython() = %q, want fallback \"python\"", got)
		}
		wantTried := []string{"python3", "python"}
		if !reflect.DeepEqual(tried, wantTried) {
			t.Fatalf("tried = %v, want %v", tried, wantTried)
		}
	})

	t.Run("real_exec_lookpath_from_PATH", func(t *testing.T) {
		isolateLocator(t)
		lookPath = exec.LookPath

		pathDir := t.TempDir()
		binName := "python3"
		if runtime.GOOS == "windows" {
			binName = "python3.exe"
		}
		want := touchFile(t, filepath.Join(pathDir, binName))
		t.Setenv("PATH", pathDir)

		if got := FindPython(); got != want {
			t.Fatalf("FindPython() = %q, want %q", got, want)
		}
	})
}

func TestFindScript_EnvVars(t *testing.T) {
	rel := filepath.Join("browser", "mg_tool_browser.py")
	fallback := "mg_tool_browser.py"

	t.Run("MG_SIDECAR_DIR_precedence_and_direct_relpath", func(t *testing.T) {
		cwdDir, _ := isolateLocator(t)
		touchFile(t, filepath.Join(cwdDir, "python", rel))

		mgHome := t.TempDir()
		touchFile(t, filepath.Join(mgHome, "python", rel))
		t.Setenv("MG_HOME", mgHome)

		sidecarDir := t.TempDir()
		want := touchFile(t, filepath.Join(sidecarDir, rel))
		t.Setenv("MG_SIDECAR_DIR", sidecarDir)

		if got := FindScript(rel, fallback); got != want {
			t.Fatalf("FindScript() = %q, want %q", got, want)
		}
	})

	t.Run("MG_SIDECAR_DIR_fallbackName_and_python_subdir", func(t *testing.T) {
		isolateLocator(t)

		sidecarDirect := t.TempDir()
		wantDirect := touchFile(t, filepath.Join(sidecarDirect, fallback))
		t.Setenv("MG_SIDECAR_DIR", sidecarDirect)
		if got := FindScript(rel, fallback); got != wantDirect {
			t.Fatalf("FindScript() = %q, want %q", got, wantDirect)
		}

		sidecarRoot := t.TempDir()
		wantPy := touchFile(t, filepath.Join(sidecarRoot, "python", rel))
		t.Setenv("MG_SIDECAR_DIR", sidecarRoot)
		if got := FindScript(rel, fallback); got != wantPy {
			t.Fatalf("FindScript() = %q, want %q", got, wantPy)
		}
	})

	t.Run("MG_SIDECAR_DIR_does_not_walk_up_parents", func(t *testing.T) {
		isolateLocator(t)
		parentDir := t.TempDir()
		touchFile(t, filepath.Join(parentDir, "python", rel))
		subDir := filepath.Join(parentDir, "nested")
		if err := os.MkdirAll(subDir, 0o755); err != nil {
			t.Fatalf("MkdirAll: %v", err)
		}
		t.Setenv("MG_SIDECAR_DIR", subDir)

		if got := FindScript(rel, fallback); got != fallback {
			t.Fatalf("FindScript() = %q, want fallback %q", got, fallback)
		}
	})

	t.Run("MG_HOME_beats_cwd_and_exe", func(t *testing.T) {
		cwdDir, exeDir := isolateLocator(t)
		touchFile(t, filepath.Join(cwdDir, "python", rel))
		touchFile(t, filepath.Join(exeDir, "python", rel))

		mgHome := t.TempDir()
		want := touchFile(t, filepath.Join(mgHome, "python", rel))
		t.Setenv("MG_HOME", mgHome)

		if got := FindScript(rel, fallback); got != want {
			t.Fatalf("FindScript() = %q, want %q", got, want)
		}
	})

	t.Run("invalid_env_vars_fall_through_to_cwd", func(t *testing.T) {
		cwdDir, _ := isolateLocator(t)
		t.Setenv("MG_SIDECAR_DIR", filepath.Join(t.TempDir(), "missing"))
		t.Setenv("MG_HOME", filepath.Join(t.TempDir(), "missing"))
		want := touchFile(t, filepath.Join(cwdDir, "python", rel))

		if got := FindScript(rel, fallback); got != want {
			t.Fatalf("FindScript() = %q, want %q", got, want)
		}
	})
}

func TestFindScript_CwdExeAndParentDiscovery(t *testing.T) {
	rel := filepath.Join("letta", "mg_memory_letta.py")
	fallback := "mg_memory_letta.py"

	t.Run("cwd_discovery_beats_exe", func(t *testing.T) {
		cwdDir, exeDir := isolateLocator(t)
		want := touchFile(t, filepath.Join(cwdDir, "python", rel))
		touchFile(t, filepath.Join(exeDir, "python", rel))

		if got := FindScript(rel, fallback); got != want {
			t.Fatalf("FindScript() = %q, want %q", got, want)
		}
	})

	t.Run("exe_dir_discovery_beats_cwd_parents", func(t *testing.T) {
		root := t.TempDir()
		nestedCwd := filepath.Join(root, "a", "b")
		if err := os.MkdirAll(nestedCwd, 0o755); err != nil {
			t.Fatalf("MkdirAll: %v", err)
		}
		_, exeDir := isolateLocator(t)
		t.Chdir(nestedCwd)

		touchFile(t, filepath.Join(root, "python", rel))
		wantExe := touchFile(t, filepath.Join(exeDir, "python", rel))

		if got := FindScript(rel, fallback); got != wantExe {
			t.Fatalf("FindScript() = %q, want %q", got, wantExe)
		}
	})

	t.Run("walks_up_to_4_parent_directories_from_cwd", func(t *testing.T) {
		base := t.TempDir()
		deepCwd := filepath.Join(base, "l1", "l2", "l3", "l4")
		if err := os.MkdirAll(deepCwd, 0o755); err != nil {
			t.Fatalf("MkdirAll: %v", err)
		}
		isolateLocator(t)
		t.Chdir(deepCwd)

		want := touchFile(t, filepath.Join(base, "python", rel))
		if got := FindScript(rel, fallback); got != want {
			t.Fatalf("FindScript() = %q, want %q at 4 levels up", got, want)
		}
	})

	t.Run("does_not_exceed_4_parent_directories_from_cwd", func(t *testing.T) {
		base := t.TempDir()
		tooDeepCwd := filepath.Join(base, "l1", "l2", "l3", "l4", "l5")
		if err := os.MkdirAll(tooDeepCwd, 0o755); err != nil {
			t.Fatalf("MkdirAll: %v", err)
		}
		isolateLocator(t)
		t.Chdir(tooDeepCwd)

		touchFile(t, filepath.Join(base, "python", rel))
		if got := FindScript(rel, fallback); got != fallback {
			t.Fatalf("FindScript() = %q, want fallback %q beyond 4 levels", got, fallback)
		}
	})

	t.Run("walks_up_to_4_parent_directories_from_exe", func(t *testing.T) {
		base := t.TempDir()
		deepExeDir := filepath.Join(base, "e1", "e2", "e3", "e4")
		if err := os.MkdirAll(deepExeDir, 0o755); err != nil {
			t.Fatalf("MkdirAll: %v", err)
		}
		isolateLocator(t)
		osExecutable = func() (string, error) {
			return filepath.Join(deepExeDir, "mg.exe"), nil
		}

		want := touchFile(t, filepath.Join(base, "scripts", fallback))
		if got := FindScript(rel, fallback); got != want {
			t.Fatalf("FindScript() = %q, want %q at 4 levels up from exe", got, want)
		}
	})

	t.Run("fallback_when_nothing_found", func(t *testing.T) {
		isolateLocator(t)
		if got := FindScript(rel, fallback); got != fallback {
			t.Fatalf("FindScript() = %q, want fallback %q", got, fallback)
		}
	})
}

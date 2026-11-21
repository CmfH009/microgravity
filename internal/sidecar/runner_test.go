package sidecar

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunnerInvoke_SuccessAndFraming(t *testing.T) {
	var gotName string
	var gotArgs []string
	var gotReq map[string]interface{}

	fake := CommandRunnerFunc(func(ctx context.Context, name string, args []string, stdin []byte) ([]byte, []byte, error) {
		gotName = name
		gotArgs = append([]string(nil), args...)
		if err := json.Unmarshal(stdin, &gotReq); err != nil {
			t.Fatalf("expected valid JSON stdin, got %q: %v", string(stdin), err)
		}
		return []byte(`{"status":"ok","title":"Example Domain","count":2}`), nil, nil
	})

	runner := &Runner{
		Command:    fake,
		FindPython: func() string { return "/fake/bin/python3" },
		FindScript: func(relPath, fallbackName string) string {
			return filepath.Join("/fake/scripts", relPath)
		},
	}

	resp, err := runner.Invoke(context.Background(), filepath.Join("browser", "mg_tool_browser.py"), "mg_tool_browser.py", map[string]interface{}{
		"action": "navigate",
		"url":    "https://example.com",
	})
	if err != nil {
		t.Fatalf("Invoke returned unexpected error: %v", err)
	}
	if gotName != "/fake/bin/python3" {
		t.Fatalf("interpreter = %q, want /fake/bin/python3", gotName)
	}
	wantScript := filepath.Join("/fake/scripts", "browser", "mg_tool_browser.py")
	if len(gotArgs) != 1 || gotArgs[0] != wantScript {
		t.Fatalf("args = %v, want [%q]", gotArgs, wantScript)
	}
	if gotReq["action"] != "navigate" || gotReq["url"] != "https://example.com" {
		t.Fatalf("unexpected stdin payload: %+v", gotReq)
	}
	if resp["title"] != "Example Domain" {
		t.Fatalf("resp[title] = %v, want Example Domain", resp["title"])
	}
}

func TestRunnerInvoke_NilContextAndNilRequest(t *testing.T) {
	var gotStdin string
	runner := &Runner{
		Command: CommandRunnerFunc(func(ctx context.Context, name string, args []string, stdin []byte) ([]byte, []byte, error) {
			if ctx == nil {
				t.Fatal("expected non-nil context passed to CommandRunner")
			}
			gotStdin = string(stdin)
			return []byte(`{"status":"ok"}`), nil, nil
		}),
		FindPython: func() string { return "py" },
		FindScript: func(relPath, fallbackName string) string { return fallbackName },
	}

	resp, err := runner.Invoke(nil, "letta/mg_memory_letta.py", "mg_memory_letta.py", nil)
	if err != nil {
		t.Fatalf("Invoke(nil, ..., nil) failed: %v", err)
	}
	if gotStdin != "{}" {
		t.Fatalf("stdin = %q, want {}", gotStdin)
	}
	if resp["status"] != "ok" {
		t.Fatalf("unexpected response: %+v", resp)
	}
}

func TestRunnerInvoke_ErrorModes(t *testing.T) {
	t.Run("marshal_error", func(t *testing.T) {
		called := false
		runner := &Runner{
			Command: CommandRunnerFunc(func(ctx context.Context, name string, args []string, stdin []byte) ([]byte, []byte, error) {
				called = true
				return nil, nil, nil
			}),
			FindPython: func() string { return "py" },
			FindScript: func(relPath, fallbackName string) string { return fallbackName },
		}

		_, err := runner.Invoke(context.Background(), "browser/mg_tool_browser.py", "mg_tool_browser.py", map[string]interface{}{
			"bad": make(chan int),
		})
		if err == nil || !strings.Contains(err.Error(), "marshal") {
			t.Fatalf("expected marshal error, got %v", err)
		}
		if called {
			t.Fatal("CommandRunner should not be invoked on marshal failure")
		}
	})

	t.Run("process_failure_with_stderr", func(t *testing.T) {
		sentinel := errors.New("exit status 1")
		runner := &Runner{
			Command: CommandRunnerFunc(func(ctx context.Context, name string, args []string, stdin []byte) ([]byte, []byte, error) {
				return nil, []byte("Traceback: ModuleNotFoundError: playwright\n"), sentinel
			}),
			FindPython: func() string { return "py" },
			FindScript: func(relPath, fallbackName string) string { return fallbackName },
		}

		_, err := runner.Invoke(context.Background(), "browser/mg_tool_browser.py", "mg_tool_browser.py", map[string]interface{}{"action": "navigate"})
		if err == nil {
			t.Fatal("expected error on process failure")
		}
		if !errors.Is(err, sentinel) {
			t.Fatalf("expected wrapped sentinel error, got %v", err)
		}
		if !strings.Contains(err.Error(), "ModuleNotFoundError: playwright") {
			t.Fatalf("expected stderr captured in error, got %v", err)
		}
	})

	t.Run("process_failure_without_stderr", func(t *testing.T) {
		sentinel := context.DeadlineExceeded
		runner := &Runner{
			Command: CommandRunnerFunc(func(ctx context.Context, name string, args []string, stdin []byte) ([]byte, []byte, error) {
				return nil, nil, sentinel
			}),
			FindPython: func() string { return "py" },
			FindScript: func(relPath, fallbackName string) string { return fallbackName },
		}

		_, err := runner.Invoke(context.Background(), "letta/mg_memory_letta.py", "mg_memory_letta.py", map[string]interface{}{"op": "stats"})
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("expected wrapped DeadlineExceeded, got %v", err)
		}
		if strings.Contains(err.Error(), "stderr:") {
			t.Fatalf("unexpected stderr section when stderr is empty: %v", err)
		}
	})

	t.Run("invalid_json_stdout", func(t *testing.T) {
		runner := &Runner{
			Command: CommandRunnerFunc(func(ctx context.Context, name string, args []string, stdin []byte) ([]byte, []byte, error) {
				return []byte("not-json-output"), nil, nil
			}),
			FindPython: func() string { return "py" },
			FindScript: func(relPath, fallbackName string) string { return fallbackName },
		}

		_, err := runner.Invoke(context.Background(), "letta/mg_memory_letta.py", "mg_memory_letta.py", map[string]interface{}{"op": "stats"})
		if err == nil || !strings.Contains(err.Error(), "not-json-output") {
			t.Fatalf("expected raw stdout in parse error, got %v", err)
		}
	})

	t.Run("null_json_stdout", func(t *testing.T) {
		runner := &Runner{
			Command: CommandRunnerFunc(func(ctx context.Context, name string, args []string, stdin []byte) ([]byte, []byte, error) {
				return []byte("null"), nil, nil
			}),
			FindPython: func() string { return "py" },
			FindScript: func(relPath, fallbackName string) string { return fallbackName },
		}

		_, err := runner.Invoke(context.Background(), "letta/mg_memory_letta.py", "mg_memory_letta.py", map[string]interface{}{"op": "stats"})
		if err == nil || !strings.Contains(err.Error(), "null") {
			t.Fatalf("expected null object error, got %v", err)
		}
	})

	t.Run("status_error_unpacked", func(t *testing.T) {
		runner := &Runner{
			Command: CommandRunnerFunc(func(ctx context.Context, name string, args []string, stdin []byte) ([]byte, []byte, error) {
				return []byte(`{"status":"error","error":"selector not found"}`), nil, nil
			}),
			FindPython: func() string { return "py" },
			FindScript: func(relPath, fallbackName string) string { return fallbackName },
		}

		_, err := runner.Invoke(context.Background(), "browser/mg_tool_browser.py", "mg_tool_browser.py", map[string]interface{}{"action": "click"})
		var sErr *SidecarError
		if !errors.As(err, &sErr) {
			t.Fatalf("expected *SidecarError, got %T (%v)", err, err)
		}
		if sErr.Script != "mg_tool_browser.py" || sErr.Message != "selector not found" {
			t.Fatalf("unexpected SidecarError: %+v", sErr)
		}
	})

	t.Run("status_error_missing_message", func(t *testing.T) {
		runner := &Runner{
			Command: CommandRunnerFunc(func(ctx context.Context, name string, args []string, stdin []byte) ([]byte, []byte, error) {
				return []byte(`{"status":"error"}`), nil, nil
			}),
			FindPython: func() string { return "py" },
			FindScript: func(relPath, fallbackName string) string { return fallbackName },
		}

		_, err := runner.Invoke(context.Background(), "browser/mg_tool_browser.py", "mg_tool_browser.py", map[string]interface{}{})
		if err == nil || !strings.Contains(err.Error(), "unknown error") {
			t.Fatalf("expected fallback error message, got %v", err)
		}
	})
}

func TestDefaultRunnerAndPackageInvoke(t *testing.T) {
	restore := SetDefaultRunner(&Runner{
		Command: CommandRunnerFunc(func(ctx context.Context, name string, args []string, stdin []byte) ([]byte, []byte, error) {
			return []byte(`{"status":"ok","via":"package-invoke"}`), nil, nil
		}),
		FindPython: func() string { return "fake-py" },
		FindScript: func(relPath, fallbackName string) string { return fallbackName },
	})
	defer restore()

	resp, err := Invoke(context.Background(), "letta/mg_memory_letta.py", "mg_memory_letta.py", map[string]interface{}{"op": "stats"})
	if err != nil {
		t.Fatalf("package Invoke failed: %v", err)
	}
	if resp["via"] != "package-invoke" {
		t.Fatalf("unexpected response: %+v", resp)
	}
}

package sidecar

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"sync"
)

// CommandRunner is the process-execution seam used by Runner to invoke a
// Python sidecar script with JSON bytes on stdin and capture stdout/stderr.
type CommandRunner interface {
	Run(ctx context.Context, name string, args []string, stdin []byte) (stdout []byte, stderr []byte, err error)
}

// CommandRunnerFunc adapts a plain function to CommandRunner.
type CommandRunnerFunc func(ctx context.Context, name string, args []string, stdin []byte) (stdout []byte, stderr []byte, err error)

// Run invokes f.
func (f CommandRunnerFunc) Run(ctx context.Context, name string, args []string, stdin []byte) ([]byte, []byte, error) {
	return f(ctx, name, args, stdin)
}

type execCommandRunner struct{}

func (execCommandRunner) Run(ctx context.Context, name string, args []string, stdin []byte) ([]byte, []byte, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdin = bytes.NewReader(stdin)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	return stdout.Bytes(), stderr.Bytes(), err
}

// SidecarError represents an application-level error returned by a sidecar
// script via {"status": "error", "error": "..."}.
type SidecarError struct {
	Script  string
	Message string
}

func (e *SidecarError) Error() string {
	if e == nil {
		return ""
	}
	if e.Script != "" {
		return fmt.Sprintf("sidecar %s error: %s", e.Script, e.Message)
	}
	return fmt.Sprintf("sidecar error: %s", e.Message)
}

// Runner owns Python interpreter discovery, sidecar script lookup, JSON
// stdin/stdout framing, stderr capture, and status=="error" unpacking.
type Runner struct {
	Command    CommandRunner
	FindPython func() string
	FindScript func(relPath, fallbackName string) string
}

// NewRunner creates a Runner using cmdRunner (or the production os/exec
// adapter when cmdRunner is nil).
func NewRunner(cmdRunner CommandRunner) *Runner {
	if cmdRunner == nil {
		cmdRunner = execCommandRunner{}
	}
	return &Runner{
		Command:    cmdRunner,
		FindPython: FindPython,
		FindScript: FindScript,
	}
}

var (
	defaultRunnerMu sync.RWMutex
	defaultRunner   = NewRunner(nil)
)

// DefaultRunner returns the package-level Runner used by Invoke.
func DefaultRunner() *Runner {
	defaultRunnerMu.RLock()
	r := defaultRunner
	defaultRunnerMu.RUnlock()
	if r == nil {
		return NewRunner(nil)
	}
	return r
}

// SetDefaultRunner replaces the package-level Runner and returns a restore
// function suitable for defer / t.Cleanup. Passing nil restores the production
// os/exec runner.
func SetDefaultRunner(r *Runner) func() {
	if r == nil {
		r = NewRunner(nil)
	}
	defaultRunnerMu.Lock()
	prev := defaultRunner
	defaultRunner = r
	defaultRunnerMu.Unlock()
	return func() {
		defaultRunnerMu.Lock()
		defaultRunner = prev
		defaultRunnerMu.Unlock()
	}
}

// Invoke runs a Python sidecar script using the default Runner.
func Invoke(ctx context.Context, scriptRelPath, fallbackName string, req map[string]interface{}) (map[string]interface{}, error) {
	return DefaultRunner().Invoke(ctx, scriptRelPath, fallbackName, req)
}

// Invoke resolves the Python interpreter and sidecar script, marshals req as
// JSON to stdin, runs the process via the CommandRunner seam, unmarshals the
// JSON stdout object, and unpacks status=="error" responses into errors.
func (r *Runner) Invoke(ctx context.Context, scriptRelPath, fallbackName string, req map[string]interface{}) (map[string]interface{}, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	label := fallbackName
	if label == "" {
		label = scriptRelPath
	}

	if req == nil {
		req = map[string]interface{}{}
	}
	reqBytes, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal sidecar request (%s): %w", label, err)
	}

	findPy := FindPython
	findScr := FindScript
	var cmdRunner CommandRunner = execCommandRunner{}
	if r != nil {
		if r.FindPython != nil {
			findPy = r.FindPython
		}
		if r.FindScript != nil {
			findScr = r.FindScript
		}
		if r.Command != nil {
			cmdRunner = r.Command
		}
	}

	python := findPy()
	scriptPath := findScr(scriptRelPath, fallbackName)

	stdout, stderr, err := cmdRunner.Run(ctx, python, []string{scriptPath}, reqBytes)
	if err != nil {
		stderrStr := strings.TrimSpace(string(stderr))
		if stderrStr != "" {
			return nil, fmt.Errorf("sidecar script %s failed: %w\nstderr: %s", label, err, stderrStr)
		}
		return nil, fmt.Errorf("sidecar script %s failed: %w", label, err)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(stdout, &result); err != nil {
		return nil, fmt.Errorf("failed to parse sidecar output (%s): %w\nraw: %s", label, err, string(stdout))
	}
	if result == nil {
		return nil, fmt.Errorf("failed to parse sidecar output (%s): expected JSON object, got null", label)
	}

	if status, ok := result["status"].(string); ok && status == "error" {
		errMsg, _ := result["error"].(string)
		if strings.TrimSpace(errMsg) == "" {
			errMsg = "unknown error"
		}
		return nil, &SidecarError{Script: label, Message: errMsg}
	}

	return result, nil
}

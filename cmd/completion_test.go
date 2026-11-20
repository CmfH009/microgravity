package cmd

import (
	"bytes"
	"strings"
	"testing"
)

func TestCompletionCommandAllShells(t *testing.T) {
	tests := []struct {
		shell    string
		expected string
	}{
		{shell: "bash", expected: "microgravity"},
		{shell: "zsh", expected: "#compdef microgravity"},
		{shell: "fish", expected: "complete -c microgravity"},
		{shell: "powershell", expected: "Register-ArgumentCompleter"},
	}

	for _, tc := range tests {
		t.Run(tc.shell, func(t *testing.T) {
			var out bytes.Buffer
			var errBuf bytes.Buffer
			rootCmd.SetOut(&out)
			rootCmd.SetErr(&errBuf)
			rootCmd.SetArgs([]string{"completion", tc.shell})
			defer func() {
				rootCmd.SetOut(nil)
				rootCmd.SetErr(nil)
				rootCmd.SetArgs(nil)
			}()

			if err := rootCmd.Execute(); err != nil {
				t.Fatalf("completion %s failed: %v (stderr: %s)", tc.shell, err, errBuf.String())
			}

			script := out.String()
			if strings.TrimSpace(script) == "" {
				t.Fatalf("completion %s returned empty output", tc.shell)
			}
			if !strings.Contains(script, tc.expected) {
				t.Errorf("completion %s output missing expected marker %q", tc.shell, tc.expected)
			}
		})
	}
}

func TestCompletionCommandInvalidArgs(t *testing.T) {
	cases := []struct {
		name string
		args []string
	}{
		{name: "unsupported shell", args: []string{"completion", "tcsh"}},
		{name: "missing shell", args: []string{"completion"}},
		{name: "too many args", args: []string{"completion", "bash", "zsh"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var out bytes.Buffer
			var errBuf bytes.Buffer
			rootCmd.SetOut(&out)
			rootCmd.SetErr(&errBuf)
			rootCmd.SetArgs(tc.args)
			defer func() {
				rootCmd.SetOut(nil)
				rootCmd.SetErr(nil)
				rootCmd.SetArgs(nil)
			}()

			if err := rootCmd.Execute(); err == nil {
				t.Fatalf("expected error for args %v, got nil", tc.args)
			}
		})
	}
}

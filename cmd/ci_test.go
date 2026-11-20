package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCiCmdDryRun(t *testing.T) {
	buf := new(bytes.Buffer)
	ciCmd.SetOut(buf)
	ciCmd.SetErr(buf)

	// Reset flags to defaults for testing
	ciDryRun = true
	ciForce = true
	ciOutputDir = ".github/workflows"
	ciBinaryName = "microgravity"
	ciGoVersion = "1.23"
	ciDisableLint = false
	ciDisableTest = false
	ciDisableMatrix = false
	ciDisableRelease = false

	err := ciCmd.RunE(ciCmd, []string{})
	if err != nil {
		t.Fatalf("ciCmd execution failed: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "=== File: ci.yml ===") {
		t.Errorf("expected dry-run output to contain ci.yml section, got: %s", out)
	}
	if !strings.Contains(out, "=== File: release.yml ===") {
		t.Errorf("expected dry-run output to contain release.yml section, got: %s", out)
	}
}

func TestCiCmdWriteFiles(t *testing.T) {
	tmpDir := t.TempDir()

	buf := new(bytes.Buffer)
	ciCmd.SetOut(buf)
	ciCmd.SetErr(buf)

	ciDryRun = false
	ciForce = true
	ciOutputDir = tmpDir
	ciBinaryName = "test-app"
	ciGoVersion = "1.23"
	ciDisableLint = false
	ciDisableTest = false
	ciDisableMatrix = false
	ciDisableRelease = false

	err := ciCmd.RunE(ciCmd, []string{})
	if err != nil {
		t.Fatalf("ciCmd execution failed: %v", err)
	}

	ciFile := filepath.Join(tmpDir, "ci.yml")
	if _, err := os.Stat(ciFile); os.IsNotExist(err) {
		t.Errorf("expected ci.yml to be generated at %s", ciFile)
	}

	releaseFile := filepath.Join(tmpDir, "release.yml")
	if _, err := os.Stat(releaseFile); os.IsNotExist(err) {
		t.Errorf("expected release.yml to be generated at %s", releaseFile)
	}

	out := buf.String()
	if !strings.Contains(out, "WORKFLOW SUMMARY") {
		t.Errorf("expected output to contain rendered summary, got: %s", out)
	}
}

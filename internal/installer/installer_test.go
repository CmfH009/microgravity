package installer

import (
	"os"
	"path/filepath"
	"testing"
)

func TestInstallBinaries(t *testing.T) {
	tempSrcDir := t.TempDir()
	tempTargetDir := t.TempDir()

	srcFile := filepath.Join(tempSrcDir, "sample.exe")
	if err := os.WriteFile(srcFile, []byte("binary data content"), 0755); err != nil {
		t.Fatalf("Failed to write dummy binary: %v", err)
	}

	res, err := InstallBinaries(srcFile, tempTargetDir)
	if err != nil {
		t.Fatalf("InstallBinaries failed: %v", err)
	}

	if len(res) != 2 {
		t.Errorf("Expected 2 installed files (microgravity.exe and mg.exe), got %d", len(res))
	}

	if _, err := os.Stat(filepath.Join(tempTargetDir, "microgravity.exe")); err != nil {
		t.Errorf("microgravity.exe missing in target dir: %v", err)
	}
	if _, err := os.Stat(filepath.Join(tempTargetDir, "mg.exe")); err != nil {
		t.Errorf("mg.exe missing in target dir: %v", err)
	}
}

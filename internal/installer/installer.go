package installer

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// InstallBinaries copies source binary to target directory under names microgravity.exe and mg.exe.
func InstallBinaries(srcPath, targetDir string) ([]string, error) {
	if err := os.MkdirAll(targetDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create target directory: %w", err)
	}

	targets := []string{
		filepath.Join(targetDir, "microgravity.exe"),
		filepath.Join(targetDir, "mg.exe"),
	}

	installed := make([]string, 0, len(targets))

	for _, target := range targets {
		if err := copyFile(srcPath, target); err != nil {
			return nil, fmt.Errorf("failed to copy to %s: %w", target, err)
		}
		installed = append(installed, target)
	}

	return installed, nil
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

	if _, err = io.Copy(out, in); err != nil {
		return err
	}
	return out.Sync()
}

// GetDefaultInstallDir returns GOPATH/bin or home/go/bin directory.
func GetDefaultInstallDir() string {
	gopath := os.Getenv("GOPATH")
	if gopath != "" {
		return filepath.Join(gopath, "bin")
	}

	home, err := os.UserHomeDir()
	if err == nil {
		return filepath.Join(home, "go", "bin")
	}

	return "."
}

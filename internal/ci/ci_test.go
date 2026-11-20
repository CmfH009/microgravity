package ci

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.AppName != "microgravity" {
		t.Errorf("expected AppName to be 'microgravity', got '%s'", cfg.AppName)
	}
	if cfg.GoVersion != "1.23" {
		t.Errorf("expected GoVersion to be '1.23', got '%s'", cfg.GoVersion)
	}
	if cfg.OutputDir != ".github/workflows" {
		t.Errorf("expected OutputDir to be '.github/workflows', got '%s'", cfg.OutputDir)
	}
	if len(cfg.Platforms) != 6 {
		t.Errorf("expected 6 default platform targets, got %d", len(cfg.Platforms))
	}
}

func TestValidateConfig(t *testing.T) {
	tests := []struct {
		name    string
		cfg     Config
		wantErr bool
	}{
		{
			name:    "valid config",
			cfg:     DefaultConfig(),
			wantErr: false,
		},
		{
			name: "empty app name",
			cfg: Config{
				AppName:   "",
				GoVersion: "1.23",
				OutputDir: ".github/workflows",
			},
			wantErr: true,
		},
		{
			name: "empty go version",
			cfg: Config{
				AppName:   "mg",
				GoVersion: "",
				OutputDir: ".github/workflows",
			},
			wantErr: true,
		},
		{
			name: "empty output dir",
			cfg: Config{
				AppName:   "mg",
				GoVersion: "1.23",
				OutputDir: "",
			},
			wantErr: true,
		},
		{
			name: "empty platforms with matrix enabled",
			cfg: Config{
				AppName:      "mg",
				GoVersion:    "1.23",
				OutputDir:    ".github/workflows",
				EnableMatrix: true,
				Platforms:    nil,
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateConfig(tt.cfg)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateConfig() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestGenerateCIWorkflow(t *testing.T) {
	cfg := DefaultConfig()
	yamlStr := GenerateCIWorkflow(cfg)

	if !strings.Contains(yamlStr, "name: CI Pipeline") {
		t.Error("expected YAML to contain workflow name")
	}
	if !strings.Contains(yamlStr, "go-version: '1.23'") {
		t.Error("expected YAML to contain Go version 1.23")
	}
	if !strings.Contains(yamlStr, "lint:") {
		t.Error("expected YAML to contain lint job")
	}
	if !strings.Contains(yamlStr, "unit-test:") {
		t.Error("expected YAML to contain unit-test job")
	}
	if !strings.Contains(yamlStr, "build-matrix:") {
		t.Error("expected YAML to contain build-matrix job")
	}

	// Verify valid YAML syntax
	var node yaml.Node
	if err := yaml.Unmarshal([]byte(yamlStr), &node); err != nil {
		t.Fatalf("generated CI workflow is invalid YAML: %v", err)
	}
}

func TestGenerateReleaseWorkflow(t *testing.T) {
	cfg := DefaultConfig()
	yamlStr := GenerateReleaseWorkflow(cfg)

	if !strings.Contains(yamlStr, "name: Automated Release Deployment") {
		t.Error("expected YAML to contain release workflow name")
	}
	if !strings.Contains(yamlStr, "tags:") || !strings.Contains(yamlStr, "'v*'") {
		t.Error("expected YAML to contain tag trigger v*")
	}
	if !strings.Contains(yamlStr, "build-release-matrix:") {
		t.Error("expected YAML to contain build-release-matrix job")
	}
	if !strings.Contains(yamlStr, "publish-release:") {
		t.Error("expected YAML to contain publish-release job")
	}

	// Verify valid YAML syntax
	var node yaml.Node
	if err := yaml.Unmarshal([]byte(yamlStr), &node); err != nil {
		t.Fatalf("generated release workflow is invalid YAML: %v", err)
	}
}

func TestWriteWorkflows(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := DefaultConfig()
	cfg.OutputDir = tmpDir

	written, err := WriteWorkflows(cfg)
	if err != nil {
		t.Fatalf("WriteWorkflows failed: %v", err)
	}
	if len(written) != 2 {
		t.Fatalf("expected 2 written files, got %d", len(written))
	}

	ciPath := filepath.Join(tmpDir, "ci.yml")
	if _, err := os.Stat(ciPath); os.IsNotExist(err) {
		t.Errorf("expected %s to exist", ciPath)
	}

	releasePath := filepath.Join(tmpDir, "release.yml")
	if _, err := os.Stat(releasePath); os.IsNotExist(err) {
		t.Errorf("expected %s to exist", releasePath)
	}

	// Attempting to rewrite without force should fail
	_, err = WriteWorkflows(cfg)
	if err == nil {
		t.Errorf("expected error when overwriting without ForceOverwrite")
	}

	// Force overwrite should succeed
	cfg.ForceOverwrite = true
	_, err = WriteWorkflows(cfg)
	if err != nil {
		t.Errorf("expected ForceOverwrite to succeed, got: %v", err)
	}
}

func TestRenderSummary(t *testing.T) {
	cfg := DefaultConfig()
	files := []string{".github/workflows/ci.yml", ".github/workflows/release.yml"}
	summary := RenderSummary(cfg, files)

	if !strings.Contains(summary, "GITHUB ACTIONS CI/CD WORKFLOW SUMMARY") {
		t.Errorf("expected summary to contain header title")
	}
	if !strings.Contains(summary, "microgravity") {
		t.Errorf("expected summary to contain app name")
	}
}

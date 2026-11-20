package ci

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// MatrixTarget defines a platform target for cross-compilation matrix.
type MatrixTarget struct {
	OS     string `yaml:"os" json:"os"`
	GOOS   string `yaml:"goos" json:"goos"`
	GOARCH string `yaml:"goarch" json:"goarch"`
}

// Config specifies parameters for generating GitHub Actions workflows.
type Config struct {
	AppName        string         `json:"app_name"`
	GoVersion      string         `json:"go_version"`
	OutputDir      string         `json:"output_dir"`
	Platforms      []MatrixTarget `json:"platforms"`
	EnableLint     bool           `json:"enable_lint"`
	EnableTest     bool           `json:"enable_test"`
	EnableMatrix   bool           `json:"enable_matrix"`
	EnableRelease  bool           `json:"enable_release"`
	ForceOverwrite bool           `json:"force_overwrite"`
}

// DefaultPlatforms returns the standard cross-platform matrix targets.
func DefaultPlatforms() []MatrixTarget {
	return []MatrixTarget{
		{OS: "ubuntu-latest", GOOS: "linux", GOARCH: "amd64"},
		{OS: "ubuntu-latest", GOOS: "linux", GOARCH: "arm64"},
		{OS: "macos-latest", GOOS: "darwin", GOARCH: "amd64"},
		{OS: "macos-latest", GOOS: "darwin", GOARCH: "arm64"},
		{OS: "windows-latest", GOOS: "windows", GOARCH: "amd64"},
		{OS: "windows-latest", GOOS: "windows", GOARCH: "arm64"},
	}
}

// DefaultConfig returns standard CI/CD workflow configuration.
func DefaultConfig() Config {
	return Config{
		AppName:        "microgravity",
		GoVersion:      "1.23",
		OutputDir:      ".github/workflows",
		Platforms:      DefaultPlatforms(),
		EnableLint:     true,
		EnableTest:     true,
		EnableMatrix:   true,
		EnableRelease:  true,
		ForceOverwrite: false,
	}
}

// ValidateConfig checks if the given configuration is valid.
func ValidateConfig(cfg Config) error {
	if strings.TrimSpace(cfg.AppName) == "" {
		return fmt.Errorf("app_name cannot be empty")
	}
	if strings.TrimSpace(cfg.GoVersion) == "" {
		return fmt.Errorf("go_version cannot be empty")
	}
	if strings.TrimSpace(cfg.OutputDir) == "" {
		return fmt.Errorf("output_dir cannot be empty")
	}
	if cfg.EnableMatrix || cfg.EnableRelease {
		if len(cfg.Platforms) == 0 {
			return fmt.Errorf("platforms list cannot be empty when matrix or release compilation is enabled")
		}
	}
	return nil
}

// FormatBinaryName computes the binary output filename for a target.
func FormatBinaryName(appName, goos, goarch string) string {
	ext := ""
	if goos == "windows" {
		ext = ".exe"
	}
	return fmt.Sprintf("%s-%s-%s%s", appName, goos, goarch, ext)
}

// GenerateCIWorkflow constructs the main CI workflow YAML (ci.yml).
func GenerateCIWorkflow(cfg Config) string {
	if len(cfg.Platforms) == 0 {
		cfg.Platforms = DefaultPlatforms()
	}

	var sb strings.Builder
	sb.WriteString("name: CI Pipeline\n\n")
	sb.WriteString("on:\n")
	sb.WriteString("  push:\n")
	sb.WriteString("    branches: [ main, master ]\n")
	sb.WriteString("  pull_request:\n")
	sb.WriteString("    branches: [ main, master ]\n\n")
	sb.WriteString("permissions:\n")
	sb.WriteString("  contents: read\n\n")
	sb.WriteString("jobs:\n")

	if cfg.EnableLint {
		sb.WriteString("  lint:\n")
		sb.WriteString("    name: Lint & Code Quality\n")
		sb.WriteString("    runs-on: ubuntu-latest\n")
		sb.WriteString("    steps:\n")
		sb.WriteString("      - name: Checkout Code\n")
		sb.WriteString("        uses: actions/checkout@v4\n\n")
		sb.WriteString("      - name: Setup Go\n")
		sb.WriteString("        uses: actions/setup-go@v5\n")
		sb.WriteString("        with:\n")
		fmt.Fprintf(&sb, "          go-version: '%s'\n", cfg.GoVersion)
		sb.WriteString("          cache: true\n\n")
		sb.WriteString("      - name: Run Go Vet & Format Check\n")
		sb.WriteString("        run: |\n")
		sb.WriteString("          go vet ./...\n")
		sb.WriteString("          test -z $(gofmt -l .)\n\n")
		sb.WriteString("      - name: Golangci-lint\n")
		sb.WriteString("        uses: golangci/golangci-lint-action@v6\n")
		sb.WriteString("        with:\n")
		sb.WriteString("          version: v1.58.0\n\n")
	}

	if cfg.EnableTest {
		sb.WriteString("  unit-test:\n")
		sb.WriteString("    name: Unit Testing & Race Detection\n")
		sb.WriteString("    runs-on: ubuntu-latest\n")
		sb.WriteString("    steps:\n")
		sb.WriteString("      - name: Checkout Code\n")
		sb.WriteString("        uses: actions/checkout@v4\n\n")
		sb.WriteString("      - name: Setup Go\n")
		sb.WriteString("        uses: actions/setup-go@v5\n")
		sb.WriteString("        with:\n")
		fmt.Fprintf(&sb, "          go-version: '%s'\n", cfg.GoVersion)
		sb.WriteString("          cache: true\n\n")
		sb.WriteString("      - name: Run Tests with Race Detector\n")
		sb.WriteString("        run: go test -v -race -coverprofile=coverage.txt ./...\n\n")
	}

	if cfg.EnableMatrix {
		sb.WriteString("  build-matrix:\n")
		sb.WriteString("    name: Cross-Platform Matrix Build\n")
		sb.WriteString("    runs-on: ${{ matrix.os }}\n")
		sb.WriteString("    strategy:\n")
		sb.WriteString("      fail-fast: false\n")
		sb.WriteString("      matrix:\n")
		sb.WriteString("        include:\n")
		for _, p := range cfg.Platforms {
			bName := FormatBinaryName(cfg.AppName, p.GOOS, p.GOARCH)
			fmt.Fprintf(&sb, "          - os: %s\n", p.OS)
			fmt.Fprintf(&sb, "            goos: %s\n", p.GOOS)
			fmt.Fprintf(&sb, "            goarch: %s\n", p.GOARCH)
			fmt.Fprintf(&sb, "            binary_name: %s\n", bName)
		}
		sb.WriteString("    steps:\n")
		sb.WriteString("      - name: Checkout Code\n")
		sb.WriteString("        uses: actions/checkout@v4\n\n")
		sb.WriteString("      - name: Setup Go\n")
		sb.WriteString("        uses: actions/setup-go@v5\n")
		sb.WriteString("        with:\n")
		fmt.Fprintf(&sb, "          go-version: '%s'\n\n", cfg.GoVersion)
		sb.WriteString("      - name: Compile Binary (${{ matrix.goos }}/${{ matrix.goarch }})\n")
		sb.WriteString("        env:\n")
		sb.WriteString("          GOOS: ${{ matrix.goos }}\n")
		sb.WriteString("          GOARCH: ${{ matrix.goarch }}\n")
		sb.WriteString("          CGO_ENABLED: '0'\n")
		sb.WriteString("        run: go build -v -ldflags=\"-s -w\" -o dist/${{ matrix.binary_name }} .\n\n")
		sb.WriteString("      - name: Upload Build Artifact\n")
		sb.WriteString("        uses: actions/upload-artifact@v4\n")
		sb.WriteString("        with:\n")
		sb.WriteString("          name: binary-${{ matrix.goos }}-${{ matrix.goarch }}\n")
		sb.WriteString("          path: dist/${{ matrix.binary_name }}\n")
	}

	return sb.String()
}

// GenerateReleaseWorkflow constructs the release deployment workflow YAML (release.yml).
func GenerateReleaseWorkflow(cfg Config) string {
	if len(cfg.Platforms) == 0 {
		cfg.Platforms = DefaultPlatforms()
	}

	var sb strings.Builder
	sb.WriteString("name: Automated Release Deployment\n\n")
	sb.WriteString("on:\n")
	sb.WriteString("  push:\n")
	sb.WriteString("    tags:\n")
	sb.WriteString("      - 'v*'\n\n")
	sb.WriteString("permissions:\n")
	sb.WriteString("  contents: write\n\n")
	sb.WriteString("jobs:\n")
	sb.WriteString("  build-release-matrix:\n")
	sb.WriteString("    name: Build Release Binaries\n")
	sb.WriteString("    runs-on: ${{ matrix.os }}\n")
	sb.WriteString("    strategy:\n")
	sb.WriteString("      fail-fast: false\n")
	sb.WriteString("      matrix:\n")
	sb.WriteString("        include:\n")
	for _, p := range cfg.Platforms {
		bName := FormatBinaryName(cfg.AppName, p.GOOS, p.GOARCH)
		fmt.Fprintf(&sb, "          - os: %s\n", p.OS)
		fmt.Fprintf(&sb, "            goos: %s\n", p.GOOS)
		fmt.Fprintf(&sb, "            goarch: %s\n", p.GOARCH)
		fmt.Fprintf(&sb, "            asset_name: %s\n", bName)
	}
	sb.WriteString("    steps:\n")
	sb.WriteString("      - name: Checkout Code\n")
	sb.WriteString("        uses: actions/checkout@v4\n\n")
	sb.WriteString("      - name: Setup Go\n")
	sb.WriteString("        uses: actions/setup-go@v5\n")
	sb.WriteString("        with:\n")
	fmt.Fprintf(&sb, "          go-version: '%s'\n\n", cfg.GoVersion)
	sb.WriteString("      - name: Build Release Binary\n")
	sb.WriteString("        env:\n")
	sb.WriteString("          GOOS: ${{ matrix.goos }}\n")
	sb.WriteString("          GOARCH: ${{ matrix.goarch }}\n")
	sb.WriteString("          CGO_ENABLED: '0'\n")
	sb.WriteString("        run: go build -v -ldflags=\"-s -w\" -o dist/${{ matrix.asset_name }} .\n\n")
	sb.WriteString("      - name: Generate Checksum\n")
	sb.WriteString("        shell: bash\n")
	sb.WriteString("        run: |\n")
	sb.WriteString("          cd dist\n")
	sb.WriteString("          sha256sum ${{ matrix.asset_name }} > ${{ matrix.asset_name }}.sha256\n\n")
	sb.WriteString("      - name: Upload Release Artifacts\n")
	sb.WriteString("        uses: actions/upload-artifact@v4\n")
	sb.WriteString("        with:\n")
	sb.WriteString("          name: release-${{ matrix.goos }}-${{ matrix.goarch }}\n")
	sb.WriteString("          path: |\n")
	sb.WriteString("            dist/${{ matrix.asset_name }}\n")
	sb.WriteString("            dist/${{ matrix.asset_name }}.sha256\n\n")
	sb.WriteString("  publish-release:\n")
	sb.WriteString("    name: Publish GitHub Release\n")
	sb.WriteString("    needs: build-release-matrix\n")
	sb.WriteString("    runs-on: ubuntu-latest\n")
	sb.WriteString("    steps:\n")
	sb.WriteString("      - name: Checkout Code\n")
	sb.WriteString("        uses: actions/checkout@v4\n\n")
	sb.WriteString("      - name: Download All Build Artifacts\n")
	sb.WriteString("        uses: actions/upload-artifact/download-artifact@v4\n")
	sb.WriteString("        with:\n")
	sb.WriteString("          path: release-assets\n")
	sb.WriteString("          merge-multiple: true\n\n")
	sb.WriteString("      - name: Consolidate Checksums\n")
	sb.WriteString("        shell: bash\n")
	sb.WriteString("        run: |\n")
	sb.WriteString("          cd release-assets\n")
	sb.WriteString("          cat *.sha256 > checksums.txt\n\n")
	sb.WriteString("      - name: Create GitHub Release\n")
	sb.WriteString("        uses: softprops/action-gh-release@v2\n")
	sb.WriteString("        with:\n")
	sb.WriteString("          files: |\n")
	sb.WriteString("            release-assets/*\n")
	sb.WriteString("          generate_release_notes: true\n")
	sb.WriteString("          draft: false\n")
	sb.WriteString("          prerelease: false\n")
	sb.WriteString("        env:\n")
	sb.WriteString("          GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}\n")

	return sb.String()
}

// GenerateAll builds a map of workflow filenames to YAML strings.
func GenerateAll(cfg Config) map[string]string {
	workflows := make(map[string]string)
	if cfg.EnableLint || cfg.EnableTest || cfg.EnableMatrix {
		workflows["ci.yml"] = GenerateCIWorkflow(cfg)
	}
	if cfg.EnableRelease {
		workflows["release.yml"] = GenerateReleaseWorkflow(cfg)
	}
	return workflows
}

// WriteWorkflows saves generated workflows to the specified output directory.
func WriteWorkflows(cfg Config) ([]string, error) {
	if err := ValidateConfig(cfg); err != nil {
		return nil, err
	}

	if err := os.MkdirAll(cfg.OutputDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create directory %s: %w", cfg.OutputDir, err)
	}

	workflows := GenerateAll(cfg)
	var written []string

	for filename, content := range workflows {
		targetPath := filepath.Join(cfg.OutputDir, filename)

		if _, err := os.Stat(targetPath); err == nil && !cfg.ForceOverwrite {
			return nil, fmt.Errorf("workflow file already exists: %s (use --force to overwrite)", targetPath)
		}

		if err := os.WriteFile(targetPath, []byte(content), 0644); err != nil {
			return nil, fmt.Errorf("failed to write workflow file %s: %w", targetPath, err)
		}

		written = append(written, targetPath)
	}

	return written, nil
}

// RenderSummary formats a visual terminal card summarizing the workflow generation.
func RenderSummary(cfg Config, writtenFiles []string) string {
	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#7D56F4")).
		MarginBottom(1)

	labelStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#04B575"))

	valueStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#FAFAFA"))

	var sb strings.Builder
	sb.WriteString(titleStyle.Render("⚡ GITHUB ACTIONS CI/CD WORKFLOW SUMMARY") + "\n\n")

	sb.WriteString(fmt.Sprintf("%s %s\n", labelStyle.Render("Binary Name:"), valueStyle.Render(cfg.AppName)))
	sb.WriteString(fmt.Sprintf("%s %s\n", labelStyle.Render("Go Version: "), valueStyle.Render(cfg.GoVersion)))
	sb.WriteString(fmt.Sprintf("%s %s\n", labelStyle.Render("Output Dir: "), valueStyle.Render(cfg.OutputDir)))

	jobs := []string{}
	if cfg.EnableLint {
		jobs = append(jobs, "Linting")
	}
	if cfg.EnableTest {
		jobs = append(jobs, "Unit Testing")
	}
	if cfg.EnableMatrix {
		jobs = append(jobs, "Cross-Platform Matrix Build")
	}
	if cfg.EnableRelease {
		jobs = append(jobs, "Automated Release Deployment")
	}
	sb.WriteString(fmt.Sprintf("%s %s\n", labelStyle.Render("Enabled Jobs:"), valueStyle.Render(strings.Join(jobs, ", "))))

	sb.WriteString(fmt.Sprintf("%s %d targets (Windows, macOS, Linux)\n", labelStyle.Render("Matrix Count:"), len(cfg.Platforms)))

	if len(writtenFiles) > 0 {
		sb.WriteString("\n" + labelStyle.Render("Generated Files:") + "\n")
		for _, f := range writtenFiles {
			sb.WriteString(fmt.Sprintf("  - %s\n", valueStyle.Render(f)))
		}
	}

	cardStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#7D56F4")).
		Padding(1, 2)

	return cardStyle.Render(sb.String())
}

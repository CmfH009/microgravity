package verifier

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

type FrameworkType string

const (
	FrameworkGo      FrameworkType = "go"
	FrameworkVitest  FrameworkType = "vitest"
	FrameworkJest    FrameworkType = "jest"
	FrameworkNPM     FrameworkType = "npm"
	FrameworkPytest  FrameworkType = "pytest"
	FrameworkCargo   FrameworkType = "cargo"
	FrameworkUnknown FrameworkType = "unknown"
)

type FrameworkInfo struct {
	Type    FrameworkType
	Command string
	Args    []string
}

type FailureDiagnosis struct {
	Framework     FrameworkType `json:"framework"`
	TestName      string        `json:"test_name,omitempty"`
	FilePath      string        `json:"file_path,omitempty"`
	LineNumber    int           `json:"line_number,omitempty"`
	Message       string        `json:"message"`
	OutputSnippet string        `json:"output_snippet,omitempty"`
	SuggestedFix  string        `json:"suggested_fix,omitempty"`
}

type VerificationResult struct {
	Passed    bool                 `json:"passed"`
	Command   string               `json:"command"`
	Output    string               `json:"output"`
	Duration  time.Duration        `json:"duration"`
	Framework FrameworkType        `json:"framework"`
	Results   []VerificationResult `json:"results,omitempty"`
	Diagnoses []FailureDiagnosis   `json:"diagnoses,omitempty"`
}

type FixOptions struct {
	MaxAttempts int
	AutoCommit  bool
	Framework   FrameworkType
}

type RepairAttempt struct {
	Attempt   int
	Diagnoses []FailureDiagnosis
	Passed    bool
	Output    string
}

type Verifier struct{}

func New() *Verifier {
	return &Verifier{}
}

// Verify automatically detects project test runners in cwd across Go, JS/TS, Python, and Rust, executing tests and returning combined results.
func (v *Verifier) Verify(ctx context.Context, cwd string) (*VerificationResult, error) {
	startTime := time.Now()
	frameworks := v.DetectFrameworks(cwd)

	if len(frameworks) == 0 {
		return &VerificationResult{
			Passed:    true,
			Command:   "none",
			Output:    "No automated test runner detected for directory",
			Duration:  time.Since(startTime),
			Framework: FrameworkUnknown,
		}, nil
	}

	var results []VerificationResult
	allPassed := true
	var commands []string
	var outputs []string
	var allDiagnoses []FailureDiagnosis

	for _, fw := range frameworks {
		fwStart := time.Now()
		out, err := runTestCommand(ctx, cwd, fw)
		passed := err == nil
		fwDuration := time.Since(fwStart)

		fullCmd := fw.Command
		if len(fw.Args) > 0 {
			fullCmd += " " + strings.Join(fw.Args, " ")
		}

		diagnoses := DiagnoseOutput(out, fw.Type)
		res := VerificationResult{
			Passed:    passed,
			Command:   fullCmd,
			Output:    out,
			Duration:  fwDuration,
			Framework: fw.Type,
			Diagnoses: diagnoses,
		}
		results = append(results, res)
		allDiagnoses = append(allDiagnoses, diagnoses...)

		commands = append(commands, fullCmd)
		if out != "" {
			outputs = append(outputs, fmt.Sprintf("[%s (%s)]:\n%s", strings.ToUpper(string(fw.Type)), fullCmd, out))
		}

		if !passed {
			allPassed = false
		}
	}

	primaryFw := FrameworkUnknown
	if len(frameworks) > 0 {
		primaryFw = frameworks[0].Type
	}

	combinedOutput := strings.Join(outputs, "\n\n")
	if combinedOutput == "" {
		combinedOutput = "All tests passed cleanly."
	}

	combinedResult := &VerificationResult{
		Passed:    allPassed,
		Command:   strings.Join(commands, " ; "),
		Output:    combinedOutput,
		Duration:  time.Since(startTime),
		Framework: primaryFw,
		Results:   results,
		Diagnoses: allDiagnoses,
	}

	return combinedResult, nil
}

func runTestCommand(ctx context.Context, cwd string, info FrameworkInfo) (string, error) {
	cmdName := info.Command
	args := info.Args

	// Cross-platform executable lookups and fallbacks
	if info.Type == FrameworkPytest {
		if _, err := exec.LookPath("pytest"); err != nil {
			cmdName = "python"
			args = append([]string{"-B", "-m", "pytest"}, args...)
		} else {
			args = append([]string{"-o", "cache_dir=" + filepath.Join(cwd, ".pytest_cache")}, args...)
		}
	} else if cmdName == "npx" {
		if _, err := exec.LookPath("npx"); err != nil {
			cmdName = "npm"
			args = []string{"test"}
		}
	}

	cmd := exec.CommandContext(ctx, cmdName, args...)
	cmd.Dir = cwd
	cmd.Env = append(os.Environ(), "PYTHONDONTWRITEBYTECODE=1")
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

var (
	BadgeSuccessStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("82")). // Green
				Bold(true).
				Border(lipgloss.RoundedBorder()).
				BorderForeground(lipgloss.Color("82")).
				Padding(0, 1)

	BadgeFailedStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("196")). // Red
				Bold(true).
				Border(lipgloss.RoundedBorder()).
				BorderForeground(lipgloss.Color("196")).
				Padding(0, 1)
)

func (vr *VerificationResult) RenderBadge() string {
	if vr.Passed {
		text := fmt.Sprintf("✓ VERIFICATION PASSED: %s (%v)", vr.Command, vr.Duration)
		return BadgeSuccessStyle.Render(text)
	}

	text := fmt.Sprintf("✗ VERIFICATION FAILED: %s (%v)\n%s", vr.Command, vr.Duration, vr.Output)
	return BadgeFailedStyle.Render(text)
}

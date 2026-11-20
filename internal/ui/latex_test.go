package ui

import (
	"os"
	"strings"
	"testing"
)

func TestRenderLaTeX(t *testing.T) {
	input := `Formula: \alpha + \beta = \sum x^2 \leq \infty`
	out := RenderLaTeX(input)

	if !strings.Contains(out, "α") || !strings.Contains(out, "β") || !strings.Contains(out, "∑") || !strings.Contains(out, "x²") || !strings.Contains(out, "≤") || !strings.Contains(out, "∞") {
		t.Errorf("LaTeX translation failed, got: %s", out)
	}

	// Test disable env
	os.Setenv("AGY_CLI_DISABLE_LATEX", "1")
	defer os.Unsetenv("AGY_CLI_DISABLE_LATEX")

	disabledOut := RenderLaTeX(input)
	if disabledOut != input {
		t.Errorf("expected verbatim input when AGY_CLI_DISABLE_LATEX=1, got: %s", disabledOut)
	}
}

package profiler_test

import (
	"context"
	"strings"
	"testing"

	"github.com/CmfH009/microgravity/internal/profiler"
)

func TestProfilePackage(t *testing.T) {
	ctx := context.Background()
	report, err := profiler.ProfilePackage(ctx, "./internal/compressor")
	if err != nil {
		t.Fatalf("ProfilePackage failed: %v", err)
	}

	if report.PackageName == "" {
		t.Errorf("Expected non-empty package name")
	}

	rendered := profiler.RenderProfileReport(report)
	if !strings.Contains(rendered, "BENCHMARK") || !strings.Contains(rendered, "MEMORY") {
		t.Errorf("Rendered profile report missing headers: %s", rendered)
	}
}

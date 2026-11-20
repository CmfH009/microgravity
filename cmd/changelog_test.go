package cmd

import (
	"testing"

	"github.com/CmfH009/microgravity/internal/changelog"
)

func TestChangelogCmd(t *testing.T) {
	rendered := changelog.Render()
	if len(rendered) == 0 {
		t.Fatalf("expected non-empty changelog output")
	}
}

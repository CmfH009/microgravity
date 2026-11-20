package cmd

import (
	"testing"

	"github.com/purav/microgravity/internal/changelog"
)

func TestChangelogCmd(t *testing.T) {
	rendered := changelog.Render()
	if len(rendered) == 0 {
		t.Fatalf("expected non-empty changelog output")
	}
}

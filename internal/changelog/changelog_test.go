package changelog

import (
	"strings"
	"testing"
)

func TestGetChangelog(t *testing.T) {
	cl := GetChangelog()
	if len(cl) == 0 {
		t.Fatalf("expected non-empty changelog")
	}

	rendered := Render()
	if !strings.Contains(rendered, "MICROGRAVITY RELEASE NOTES") {
		t.Errorf("rendered changelog missing header, got: %s", rendered)
	}
	if !strings.Contains(rendered, "1.1.0") {
		t.Errorf("rendered changelog missing version 1.1.0")
	}
}

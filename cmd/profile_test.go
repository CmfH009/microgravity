package cmd

import (
	"bytes"
	"testing"
)

func TestProfileCmd(t *testing.T) {
	buf := new(bytes.Buffer)
	profileCmd.SetOut(buf)
	profileCmd.SetErr(buf)

	if err := profileCmd.RunE(profileCmd, []string{"./internal/compressor"}); err != nil {
		t.Fatalf("profileCmd execute failed: %v", err)
	}

	out := buf.String()
	if out == "" {
		t.Errorf("Expected non-empty output from profileCmd")
	}
}

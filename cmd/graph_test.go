package cmd

import (
	"bytes"
	"testing"
)

func TestGraphCmd(t *testing.T) {
	buf := new(bytes.Buffer)
	graphCmd.SetOut(buf)
	graphCmd.SetErr(buf)

	if err := graphCmd.RunE(graphCmd, []string{}); err != nil {
		t.Fatalf("graphCmd execute failed: %v", err)
	}

	out := buf.String()
	if out == "" {
		t.Errorf("Expected non-empty output from graphCmd")
	}
}

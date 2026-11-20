package cmd

import (
	"bytes"
	"testing"
)

func TestSwarmCmd(t *testing.T) {
	buf := new(bytes.Buffer)
	swarmCmd.SetOut(buf)
	swarmCmd.SetErr(buf)

	args := []string{"Task 1: Verify code quality", "Task 2: Audit dependencies"}
	if err := swarmCmd.RunE(swarmCmd, args); err != nil {
		t.Fatalf("swarmCmd execute failed: %v", err)
	}

	out := buf.String()
	if out == "" {
		t.Errorf("Expected non-empty output from swarmCmd")
	}
}

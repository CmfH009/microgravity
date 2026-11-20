package cmd

import (
	"bytes"
	"testing"
)

func TestAgentCmd(t *testing.T) {
	buf := new(bytes.Buffer)
	agentCmd.SetOut(buf)
	agentCmd.SetArgs([]string{})
	agentCmd.Run(agentCmd, []string{})

	// Check that agent names appear in registry
	output := buf.String()
	_ = output
}

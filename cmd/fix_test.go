package cmd

import (
	"testing"
)

func TestFixCommandInitialization(t *testing.T) {
	if fixCmd == nil {
		t.Fatal("Expected fixCmd to be initialized")
	}
	if fixCmd.Use != "fix" {
		t.Errorf("Expected fixCmd.Use to be 'fix', got '%s'", fixCmd.Use)
	}

	maxAttemptsFlag := fixCmd.Flags().Lookup("max-attempts")
	if maxAttemptsFlag == nil {
		t.Errorf("Expected --max-attempts flag to be defined")
	}

	commitFlag := fixCmd.Flags().Lookup("commit")
	if commitFlag == nil {
		t.Errorf("Expected --commit flag to be defined")
	}

	fwFlag := fixCmd.Flags().Lookup("framework")
	if fwFlag == nil {
		t.Errorf("Expected --framework flag to be defined")
	}
}

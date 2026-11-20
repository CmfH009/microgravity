package cmd

import (
	"testing"
)

func TestUpdateCmd(t *testing.T) {
	if updateCmd.Name() != "update" {
		t.Errorf("expected update command name 'update', got %s", updateCmd.Name())
	}
}

package cmd

import (
	"testing"
)

func TestPreviewCmdSetup(t *testing.T) {
	if previewCmd.Use != "preview" {
		t.Errorf("expected command use to be 'preview', got %s", previewCmd.Use)
	}

	portFlag := previewCmd.Flags().Lookup("port")
	if portFlag == nil {
		t.Errorf("expected --port flag to be present")
	}

	dirFlag := previewCmd.Flags().Lookup("dir")
	if dirFlag == nil {
		t.Errorf("expected --dir flag to be present")
	}
}

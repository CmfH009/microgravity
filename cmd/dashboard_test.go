package cmd

import (
	"bytes"
	"testing"
)

func TestDashboardCommand(t *testing.T) {
	if dashboardCmd.Use != "dashboard" {
		t.Errorf("expected dashboard command use to be 'dashboard', got '%s'", dashboardCmd.Use)
	}

	webFlag := dashboardCmd.Flags().Lookup("web")
	if webFlag == nil {
		t.Fatalf("expected --web flag on dashboard command")
	}

	portFlag := dashboardCmd.Flags().Lookup("port")
	if portFlag == nil {
		t.Fatalf("expected --port flag on dashboard command")
	}

	liveFlag := dashboardCmd.Flags().Lookup("live")
	if liveFlag == nil {
		t.Fatalf("expected --live flag on dashboard command")
	}
}

func TestRunDashboardExecution(t *testing.T) {
	buf := new(bytes.Buffer)
	dashboardCmd.SetOut(buf)

	err := runDashboard(false, 8085, false)
	if err != nil {
		t.Fatalf("expected runDashboard to succeed, got %v", err)
	}
}

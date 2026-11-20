package trajectory

import (
	"bufio"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestLoggerAGYParity(t *testing.T) {
	sessionID := "test-session-agy-123"
	logger, err := NewLogger(sessionID)
	if err != nil {
		t.Fatalf("failed to create logger: %v", err)
	}

	longContent := strings.Repeat("Extensive output from execution. ", 30)
	longThinking := strings.Repeat("Analyzing AST structure and symbols. ", 20)
	tools := []ToolCallInfo{
		{ToolName: "run_command", Arguments: map[string]interface{}{"CommandLine": "go test ./..."}},
	}

	err = logger.LogStep("MODEL", "PLANNER_RESPONSE", "DONE", longContent, longThinking, tools)
	if err != nil {
		t.Fatalf("LogStep failed: %v", err)
	}

	// Verify compact log
	compactFile, err := os.Open(logger.compactPath)
	if err != nil {
		t.Fatalf("failed to open compact log: %v", err)
	}
	defer compactFile.Close()

	scanner := bufio.NewScanner(compactFile)
	if !scanner.Scan() {
		t.Fatalf("compact log is empty")
	}

	var step Step
	if err := json.Unmarshal(scanner.Bytes(), &step); err != nil {
		t.Fatalf("failed to unmarshal compact step: %v", err)
	}

	if step.StepIndex != 1 {
		t.Errorf("expected step_index 1, got %d", step.StepIndex)
	}
	if step.Source != "MODEL" || step.Type != "PLANNER_RESPONSE" || step.Status != "DONE" {
		t.Errorf("unexpected step metadata: %+v", step)
	}
	if len(step.TruncatedFields) == 0 {
		t.Errorf("expected truncated_fields to be present in compact log")
	}
	if !strings.Contains(step.Content, "[truncated]") {
		t.Errorf("expected truncated content in compact log")
	}

	// Verify full log
	fullFile, err := os.Open(logger.fullLogPath)
	if err != nil {
		t.Fatalf("failed to open full log: %v", err)
	}
	defer fullFile.Close()

	scannerFull := bufio.NewScanner(fullFile)
	if !scannerFull.Scan() {
		t.Fatalf("full log is empty")
	}

	var fullStep Step
	if err := json.Unmarshal(scannerFull.Bytes(), &fullStep); err != nil {
		t.Fatalf("failed to unmarshal full step: %v", err)
	}

	if fullStep.Content != longContent {
		t.Errorf("expected full untruncated content in full log")
	}
	if len(fullStep.TruncatedFields) != 0 {
		t.Errorf("expected no truncated_fields in full log")
	}
}

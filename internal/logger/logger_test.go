package logger

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestLogger_PlainTextFormat(t *testing.T) {
	os.Unsetenv("MG_LOG_FORMAT")

	// Just verify it doesn't panic - we can't easily capture stdout
	Info("test message %s", "plain")
	Warn("warning %d", 42)
	Error("error occurred")
}

func TestLogger_JSONFormat(t *testing.T) {
	os.Setenv("MG_LOG_FORMAT", "json")
	defer os.Unsetenv("MG_LOG_FORMAT")

	Info("test json message")

	// Verify the env var is read correctly
	if !IsJSON() {
		t.Fatal("expected IsJSON() to return true when MG_LOG_FORMAT=json")
	}
}

func TestLogger_IsJSONFalse(t *testing.T) {
	os.Unsetenv("MG_LOG_FORMAT")
	if IsJSON() {
		t.Fatal("expected IsJSON() to return false when MG_LOG_FORMAT is unset")
	}
}

func TestLogger_IsJSONOther(t *testing.T) {
	os.Setenv("MG_LOG_FORMAT", "text")
	defer os.Unsetenv("MG_LOG_FORMAT")
	if IsJSON() {
		t.Fatal("expected IsJSON() to return false when MG_LOG_FORMAT=text")
	}
}

func TestEntryJSONMarshal(t *testing.T) {
	e := entry{
		Level:   InfoLevel,
		Message: "hello",
		Time:    "2026-10-19T00:00:00Z",
	}
	b, err := json.Marshal(e)
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	if !strings.Contains(s, `"level":"info"`) {
		t.Fatalf("expected level field, got %s", s)
	}
	if !strings.Contains(s, `"msg":"hello"`) {
		t.Fatalf("expected msg field, got %s", s)
	}
	if !strings.Contains(s, `"ts"`) {
		t.Fatalf("expected ts field, got %s", s)
	}
}

// Package logger provides structured logging for Microgravity.
//
// By default, log lines are plain text. Set MG_LOG_FORMAT=json to emit
// machine-readable JSON objects with level, message, and timestamp fields.
package logger

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"
)

// Level is a log severity level.
type Level string

const (
	DebugLevel Level = "debug"
	InfoLevel  Level = "info"
	WarnLevel  Level = "warn"
	ErrorLevel Level = "error"
)

// entry is a structured log entry.
type entry struct {
	Level   Level   `json:"level"`
	Message string  `json:"msg"`
	Time    string  `json:"ts"`
	Caller  string  `json:"caller,omitempty"`
}

// format determines the output format from MG_LOG_FORMAT.
func format() string {
	return strings.ToLower(os.Getenv("MG_LOG_FORMAT"))
}

// IsJSON reports whether structured JSON logging is enabled.
func IsJSON() bool {
	return format() == "json"
}

// write emits a log entry in the configured format.
func write(level Level, msg string, args ...interface{}) {
	if len(args) > 0 {
		msg = fmt.Sprintf(msg, args...)
	}

	if IsJSON() {
		e := entry{
			Level:   level,
			Message: msg,
			Time:    time.Now().UTC().Format(time.RFC3339),
		}
		b, err := json.Marshal(e)
		if err != nil {
			fmt.Fprintf(os.Stderr, "{\"level\":\"error\",\"msg\":\"failed to marshal log entry: %v\"}\n", err)
			return
		}
		fmt.Println(string(b))
		return
	}

	fmt.Printf("[%s] %s\n", strings.ToUpper(string(level)), msg)
}

// Debug logs a debug-level message.
func Debug(msg string, args ...interface{}) {
	write(DebugLevel, msg, args...)
}

// Info logs an info-level message.
func Info(msg string, args ...interface{}) {
	write(InfoLevel, msg, args...)
}

// Warn logs a warning-level message.
func Warn(msg string, args ...interface{}) {
	write(WarnLevel, msg, args...)
}

// Error logs an error-level message.
func Error(msg string, args ...interface{}) {
	write(ErrorLevel, msg, args...)
}

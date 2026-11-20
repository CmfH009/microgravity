package cmdparse

import "testing"

func TestExtractToolCallSkipsUnrelatedJSON(t *testing.T) {
	call, ok, err := ExtractToolCall(`Answer {"metadata":{"ok":true}} then {"tool_call":"read_file","arguments":{"path":"a}b"}}`)
	if err != nil || !ok {
		t.Fatalf("expected tool call, got %v, %v", ok, err)
	}
	if call.ToolCall != "read_file" || call.Arguments["path"] != "a}b" {
		t.Fatalf("unexpected call: %#v", call)
	}
}

func TestExtractToolCalls_MultipleBlocks(t *testing.T) {
	text := "I will view two files:\n```json\n{\"tool_call\": \"view_file\", \"arguments\": {\"AbsolutePath\": \"A:/file1.txt\"}}\n```\nAnd check file 2:\n```json\n{\"tool_call\": \"view_file\", \"arguments\": {\"AbsolutePath\": \"A:/file2.txt\"}}\n```\n"
	calls, ok, err := ExtractToolCalls(text)
	if err != nil || !ok {
		t.Fatalf("expected multi-tool calls, got %v, %v", ok, err)
	}
	if len(calls) != 2 {
		t.Fatalf("expected 2 tool calls, got %d", len(calls))
	}
	if calls[0].Arguments["AbsolutePath"] != "A:/file1.txt" {
		t.Errorf("expected file1.txt, got %v", calls[0].Arguments["AbsolutePath"])
	}
	if calls[1].Arguments["AbsolutePath"] != "A:/file2.txt" {
		t.Errorf("expected file2.txt, got %v", calls[1].Arguments["AbsolutePath"])
	}
}

func TestExtractToolCalls_JSONArray(t *testing.T) {
	text := `[
		{"tool_call": "grep_search", "arguments": {"Query": "TODO"}},
		{"name": "list_dir", "args": {"DirectoryPath": "A:/projects"}}
	]`

	calls, ok, err := ExtractToolCalls(text)
	if err != nil || !ok {
		t.Fatalf("expected array tool calls, got %v, %v", ok, err)
	}
	if len(calls) != 2 {
		t.Fatalf("expected 2 tool calls, got %d", len(calls))
	}
	if calls[0].ToolCall != "grep_search" || calls[0].Arguments["Query"] != "TODO" {
		t.Errorf("unexpected call 0: %#v", calls[0])
	}
	if calls[1].ToolCall != "list_dir" || calls[1].Arguments["DirectoryPath"] != "A:/projects" {
		t.Errorf("unexpected call 1: %#v", calls[1])
	}
}

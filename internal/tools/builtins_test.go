package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/purav/microgravity/internal/events"
	"github.com/purav/microgravity/internal/rag"
	"github.com/purav/microgravity/internal/scheduler"
)

func TestBuiltins(t *testing.T) {
	registry := NewRegistry()
	bus := events.NewEventBus()
	sched := scheduler.New()
	defer sched.Stop()
	ch := make(chan string, 5)

	// Create a temp rag store
	dbPath := filepath.Join(os.TempDir(), "test_builtins_rag.db")
	defer os.Remove(dbPath)
	ragStore, err := rag.NewStore(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer ragStore.Close()

	RegisterBuiltins(registry, bus, sched, ch, ragStore)

	ctx := context.Background()

	t.Run("get_current_time", func(t *testing.T) {
		out, err := registry.Execute(ctx, "get_current_time", map[string]interface{}{})
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out, "Current Date & Time") {
			t.Errorf("unexpected time format: %s", out)
		}
	})

	t.Run("run_command and manage_task", func(t *testing.T) {
		out, err := registry.Execute(ctx, "run_command", map[string]interface{}{
			"CommandLine":       "echo 'running test'",
			"Cwd":               t.TempDir(),
			"WaitMsBeforeAsync": float64(5000),
		})
		if err != nil {
			t.Fatalf("run_command failed: %v", err)
		}
		if !strings.Contains(out, "The command exited with code 0") {
			t.Errorf("unexpected run_command output: %s", out)
		}

		taskList, err := registry.Execute(ctx, "manage_task", map[string]interface{}{
			"Action": "list",
		})
		if err != nil {
			t.Fatalf("manage_task list failed: %v", err)
		}
		if !strings.Contains(taskList, "running test") {
			t.Errorf("expected task list to contain executed task: %s", taskList)
		}
	})

	t.Run("write_to_file and view_file", func(t *testing.T) {
		tmp := filepath.Join(os.TempDir(), "test_write_view.txt")
		defer os.Remove(tmp)

		out, err := registry.Execute(ctx, "write_to_file", map[string]interface{}{
			"TargetFile":  tmp,
			"CodeContent": "line 1\nline 2\nline 3\n",
			"Overwrite":   true,
		})
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out, "Successfully wrote") {
			t.Errorf("expected success message, got %s", out)
		}

		viewOut, err := registry.Execute(ctx, "view_file", map[string]interface{}{
			"AbsolutePath": tmp,
			"StartLine":    float64(2),
			"EndLine":      float64(3),
		})
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(viewOut, "2: line 2") || !strings.Contains(viewOut, "3: line 3") {
			t.Errorf("expected slice lines 2 and 3, got %s", viewOut)
		}
	})

	t.Run("replace_file_content", func(t *testing.T) {
		tmp := filepath.Join(os.TempDir(), "test_replace.txt")
		defer os.Remove(tmp)
		_ = os.WriteFile(tmp, []byte("alpha beta gamma"), 0644)

		out, err := registry.Execute(ctx, "replace_file_content", map[string]interface{}{
			"TargetFile":         tmp,
			"TargetContent":      "beta",
			"ReplacementContent": "omega",
		})
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out, "Successfully replaced") {
			t.Errorf("expected replaced output, got %s", out)
		}

		content, _ := os.ReadFile(tmp)
		if string(content) != "alpha omega gamma" {
			t.Errorf("expected replaced file content, got %s", string(content))
		}
	})

	t.Run("replace_file_content_crlf_multiline", func(t *testing.T) {
		tmp := filepath.Join(os.TempDir(), "test_replace_crlf.txt")
		defer os.Remove(tmp)
		original := "line 1\r\nline 2: target to replace\r\nline 3: second line\r\nline 4: tail\r\n"
		_ = os.WriteFile(tmp, []byte(original), 0644)

		// TargetContent has LF only (standard for LLM output)
		targetLF := "line 2: target to replace\nline 3: second line"
		replacementLF := "line 2: replaced\nline 3: updated"

		out, err := registry.Execute(ctx, "replace_file_content", map[string]interface{}{
			"TargetFile":         tmp,
			"TargetContent":      targetLF,
			"ReplacementContent": replacementLF,
		})
		if err != nil {
			t.Fatalf("replace_file_content failed on CRLF file: %v", err)
		}
		if !strings.Contains(out, "Successfully replaced") {
			t.Errorf("expected replaced output, got %s", out)
		}

		content, err := os.ReadFile(tmp)
		if err != nil {
			t.Fatal(err)
		}
		expected := "line 1\r\nline 2: replaced\r\nline 3: updated\r\nline 4: tail\r\n"
		if string(content) != expected {
			t.Errorf("expected CRLF content:\n%q\ngot:\n%q", expected, string(content))
		}
		if !strings.Contains(string(content), "\r\n") {
			t.Errorf("expected result to preserve CRLF line endings")
		}

		// Also test line-range restricted replacement with CRLF
		originalRange := "first line\r\nblock start\r\nblock end\r\nlast line\r\n"
		_ = os.WriteFile(tmp, []byte(originalRange), 0644)

		outRange, err := registry.Execute(ctx, "replace_file_content", map[string]interface{}{
			"TargetFile":         tmp,
			"TargetContent":      "block start\nblock end",
			"ReplacementContent": "block replaced",
			"StartLine":          2,
			"EndLine":            3,
		})
		if err != nil {
			t.Fatalf("range replacement failed on CRLF file: %v", err)
		}
		if !strings.Contains(outRange, "Successfully replaced") {
			t.Errorf("expected success output, got %s", outRange)
		}
		contentRange, _ := os.ReadFile(tmp)
		expectedRange := "first line\r\nblock replaced\r\nlast line\r\n"
		if string(contentRange) != expectedRange {
			t.Errorf("expected range CRLF content:\n%q\ngot:\n%q", expectedRange, string(contentRange))
		}
	})

	t.Run("find_by_name and list_dir", func(t *testing.T) {
		dir := t.TempDir()
		f1 := filepath.Join(dir, "target.go")
		_ = os.WriteFile(f1, []byte("package main"), 0644)

		findOut, err := registry.Execute(ctx, "find_by_name", map[string]interface{}{
			"SearchDirectory": dir,
			"Pattern":         "*.go",
		})
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(findOut, "target.go") {
			t.Errorf("expected find_by_name to find target.go, got: %s", findOut)
		}

		listOut, err := registry.Execute(ctx, "list_dir", map[string]interface{}{
			"DirectoryPath": dir,
		})
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(listOut, "target.go") {
			t.Errorf("expected list_dir to list target.go, got: %s", listOut)
		}
	})

	t.Run("grep_search", func(t *testing.T) {
		dir := t.TempDir()
		f1 := filepath.Join(dir, "grep_target.txt")
		_ = os.WriteFile(f1, []byte("hello search query world"), 0644)

		grepOut, err := registry.Execute(ctx, "grep_search", map[string]interface{}{
			"SearchPath": dir,
			"Query":      "search query",
		})
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(grepOut, "search query") {
			t.Errorf("expected grep match, got: %s", grepOut)
		}
	})

	t.Run("schedule", func(t *testing.T) {
		res, err := registry.Execute(ctx, "schedule", map[string]interface{}{
			"DurationSeconds": float64(1),
			"Prompt":          "hello scheduler",
		})
		if err != nil {
			t.Fatalf("schedule failed: %v", err)
		}
		if !strings.Contains(res, "Timer scheduled") {
			t.Errorf("expected schedule result message, got: %s", res)
		}
	})

	t.Run("subagent suite and messaging", func(t *testing.T) {
		// Define custom subagent
		defOut, err := registry.Execute(ctx, "define_subagent", map[string]interface{}{
			"name":               "benchmarker",
			"description":        "Performance benchmarking specialist",
			"system_prompt":      "Measure latency and memory efficiency.",
			"enable_write_tools": true,
		})
		if err != nil {
			t.Fatalf("define_subagent failed: %v", err)
		}
		if !strings.Contains(defOut, "benchmarker") {
			t.Errorf("unexpected define output: %s", defOut)
		}

		// Invoke subagents
		invOut, err := registry.Execute(ctx, "invoke_subagent", map[string]interface{}{
			"Subagents": []interface{}{
				map[string]interface{}{
					"TypeName":  "benchmarker",
					"Role":      "Latency Auditor",
					"Prompt":    "Profile token latency",
					"Model":     "inherit",
					"Workspace": "inherit",
				},
			},
		})
		if err != nil {
			t.Fatalf("invoke_subagent failed: %v", err)
		}
		if !strings.Contains(invOut, "Latency Auditor") {
			t.Errorf("unexpected invoke output: %s", invOut)
		}

		// Manage subagents list
		mOut, err := registry.Execute(ctx, "manage_subagents", map[string]interface{}{
			"Action": "list",
		})
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(mOut, "Latency Auditor") {
			t.Errorf("expected list to contain Latency Auditor, got: %s", mOut)
		}
	})

	t.Run("generate_image and ask_question", func(t *testing.T) {
		imgOut, err := registry.Execute(ctx, "generate_image", map[string]interface{}{
			"Prompt":      "Dashboard UI mockup with metrics and graphs",
			"ImageName":   "test_mockup",
			"AspectRatio": "16:9",
		})
		if err != nil {
			t.Fatalf("generate_image failed: %v", err)
		}
		if !strings.Contains(imgOut, "test_mockup") {
			t.Errorf("unexpected generate_image output: %s", imgOut)
		}

		askOut, err := registry.Execute(ctx, "ask_question", map[string]interface{}{
			"questions": []interface{}{
				map[string]interface{}{
					"question": "Which database would you prefer?",
					"options":  []interface{}{"PostgreSQL", "SQLite"},
				},
			},
		})
		if err != nil {
			t.Fatalf("ask_question failed: %v", err)
		}
		if !strings.Contains(askOut, "PostgreSQL") {
			t.Errorf("unexpected ask_question output: %s", askOut)
		}
	})
}

func TestVisionTools(t *testing.T) {
	registry := NewRegistry()
	RegisterVisionTools(registry)

	ctx := context.Background()

	// Verify all 5 vision tools are registered
	visionTools := []string{
		"detect_objects",
		"segment_image",
		"estimate_depth",
		"describe_image",
		"detect_and_segment",
	}
	for _, name := range visionTools {
		if !registry.IsNative(name) {
			t.Errorf("vision tool %s not registered", name)
		}
	}

	t.Run("detect_objects_missing_path", func(t *testing.T) {
		_, err := registry.Execute(ctx, "detect_objects", map[string]interface{}{
			"ImagePath": "",
		})
		if err == nil {
			t.Error("expected error for empty ImagePath")
		}
	})

	t.Run("detect_objects_nonexistent_file", func(t *testing.T) {
		_, err := registry.Execute(ctx, "detect_objects", map[string]interface{}{
			"ImagePath": "/nonexistent/image.jpg",
		})
		if err == nil {
			t.Error("expected error for nonexistent image file")
		}
		if !strings.Contains(err.Error(), "not found") {
			t.Errorf("expected 'not found' in error, got: %v", err)
		}
	})

	t.Run("segment_image_missing_path", func(t *testing.T) {
		_, err := registry.Execute(ctx, "segment_image", map[string]interface{}{})
		if err == nil {
			t.Error("expected error for missing ImagePath")
		}
	})

	t.Run("estimate_depth_missing_path", func(t *testing.T) {
		_, err := registry.Execute(ctx, "estimate_depth", map[string]interface{}{
			"ImagePath": "/nonexistent/depth.png",
		})
		if err == nil {
			t.Error("expected error for nonexistent image file")
		}
	})

	t.Run("describe_image_missing_path", func(t *testing.T) {
		_, err := registry.Execute(ctx, "describe_image", map[string]interface{}{
			"ImagePath": "",
		})
		if err == nil {
			t.Error("expected error for empty ImagePath")
		}
	})

	t.Run("detect_and_segment_nonexistent", func(t *testing.T) {
		_, err := registry.Execute(ctx, "detect_and_segment", map[string]interface{}{
			"ImagePath": "/does/not/exist.jpg",
		})
		if err == nil {
			t.Error("expected error for nonexistent image")
		}
	})

	// Verify tool count matches expected
	tools := registry.GetNativeTools()
	if len(tools) != 5 {
		t.Errorf("expected 5 vision tools registered, got %d", len(tools))
	}
}


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

func setupTestRegistry(t *testing.T) (*Registry, context.Context, func()) {
	t.Helper()
	registry := NewRegistry()
	bus := events.NewEventBus()
	sched := scheduler.New()
	ch := make(chan string, 10)

	dbPath := filepath.Join(t.TempDir(), "test_crlf_rag.db")
	ragStore, err := rag.NewStore(dbPath)
	if err != nil {
		t.Fatalf("failed to create rag store: %v", err)
	}

	RegisterBuiltins(registry, bus, sched, ch, ragStore)
	cleanup := func() {
		sched.Stop()
		ragStore.Close()
	}
	return registry, context.Background(), cleanup
}

func TestCRLFAdversarial(t *testing.T) {
	registry, ctx, cleanup := setupTestRegistry(t)
	defer cleanup()

	// ─── Scenario 1: CRLF file, LF target, LF replacement -> Preserves CRLF ───
	t.Run("Scenario1_CRLFFile_LFTarget_LFReplacement", func(t *testing.T) {
		tmp := filepath.Join(t.TempDir(), "scenario1.txt")
		fileContent := "header\r\nfunc oldFunction() {\r\n    return nil\r\n}\r\nfooter\r\n"
		if err := os.WriteFile(tmp, []byte(fileContent), 0644); err != nil {
			t.Fatal(err)
		}

		targetLF := "func oldFunction() {\n    return nil\n}"
		replacementLF := "func newFunction() {\n    return true\n}"

		res, err := registry.Execute(ctx, "replace_file_content", map[string]interface{}{
			"TargetFile":         tmp,
			"TargetContent":      targetLF,
			"ReplacementContent": replacementLF,
		})
		if err != nil {
			t.Fatalf("replace failed: %v", err)
		}
		if !strings.Contains(res, "Successfully replaced") {
			t.Fatalf("unexpected result: %s", res)
		}

		gotBytes, err := os.ReadFile(tmp)
		if err != nil {
			t.Fatal(err)
		}
		got := string(gotBytes)

		expected := "header\r\nfunc newFunction() {\r\n    return true\r\n}\r\nfooter\r\n"
		if got != expected {
			t.Errorf("CRLF preservation mismatch:\nExpected: %q\nGot:      %q", expected, got)
		}

		// Verify every newline in the file is CRLF
		nonCRLF := strings.ReplaceAll(got, "\r\n", "")
		if strings.Contains(nonCRLF, "\n") || strings.Contains(nonCRLF, "\r") {
			t.Errorf("found lone LF or CR in output: %q", nonCRLF)
		}
	})

	// ─── Scenario 2: Target with trailing CRLF vs trailing LF ───
	t.Run("Scenario2A_TargetWithTrailingCRLF_Global", func(t *testing.T) {
		tmp := filepath.Join(t.TempDir(), "scenario2a.txt")
		fileContent := "line 1\r\nline 2\r\nline 3\r\n"
		if err := os.WriteFile(tmp, []byte(fileContent), 0644); err != nil {
			t.Fatal(err)
		}

		targetCRLF := "line 2\r\n"
		replacement := "line 2 replaced\r\n"

		_, err := registry.Execute(ctx, "replace_file_content", map[string]interface{}{
			"TargetFile":         tmp,
			"TargetContent":      targetCRLF,
			"ReplacementContent": replacement,
		})
		if err != nil {
			t.Fatalf("replace with trailing CRLF failed: %v", err)
		}

		got, _ := os.ReadFile(tmp)
		expected := "line 1\r\nline 2 replaced\r\nline 3\r\n"
		if string(got) != expected {
			t.Errorf("mismatch:\nExpected: %q\nGot:      %q", expected, string(got))
		}
	})

	t.Run("Scenario2B_TargetWithTrailingLF_Global", func(t *testing.T) {
		tmp := filepath.Join(t.TempDir(), "scenario2b.txt")
		fileContent := "line 1\r\nline 2\r\nline 3\r\n"
		if err := os.WriteFile(tmp, []byte(fileContent), 0644); err != nil {
			t.Fatal(err)
		}

		targetLF := "line 2\n"
		replacement := "line 2 replaced\n"

		_, err := registry.Execute(ctx, "replace_file_content", map[string]interface{}{
			"TargetFile":         tmp,
			"TargetContent":      targetLF,
			"ReplacementContent": replacement,
		})
		if err != nil {
			t.Fatalf("replace with trailing LF failed: %v", err)
		}

		got, _ := os.ReadFile(tmp)
		expected := "line 1\r\nline 2 replaced\r\nline 3\r\n"
		if string(got) != expected {
			t.Errorf("mismatch:\nExpected: %q\nGot:      %q", expected, string(got))
		}
	})

	t.Run("Scenario2C_TargetWithNoTrailingNewline_Global", func(t *testing.T) {
		tmp := filepath.Join(t.TempDir(), "scenario2c.txt")
		fileContent := "line 1\r\nline 2\r\nline 3\r\n"
		if err := os.WriteFile(tmp, []byte(fileContent), 0644); err != nil {
			t.Fatal(err)
		}

		target := "line 2"
		replacement := "line 2 replaced"

		_, err := registry.Execute(ctx, "replace_file_content", map[string]interface{}{
			"TargetFile":         tmp,
			"TargetContent":      target,
			"ReplacementContent": replacement,
		})
		if err != nil {
			t.Fatalf("replace with no trailing newline failed: %v", err)
		}

		got, _ := os.ReadFile(tmp)
		expected := "line 1\r\nline 2 replaced\r\nline 3\r\n"
		if string(got) != expected {
			t.Errorf("mismatch:\nExpected: %q\nGot:      %q", expected, string(got))
		}
	})

	// ─── Scenario 3: Multi-line indentation and leading whitespace ───
	t.Run("Scenario3_MultilineIndentationAndWhitespace", func(t *testing.T) {
		tmp := filepath.Join(t.TempDir(), "scenario3.txt")
		fileContent := "class Foo:\r\n    def bar(self):\r\n        \tx = 1\r\n        \ty = 2\r\n        return x + y\r\n"
		if err := os.WriteFile(tmp, []byte(fileContent), 0644); err != nil {
			t.Fatal(err)
		}

		targetLF := "        \tx = 1\n        \ty = 2\n        return x + y"
		replacementLF := "        \tz = 42\n        return z"

		_, err := registry.Execute(ctx, "replace_file_content", map[string]interface{}{
			"TargetFile":         tmp,
			"TargetContent":      targetLF,
			"ReplacementContent": replacementLF,
		})
		if err != nil {
			t.Fatalf("multiline indent replace failed: %v", err)
		}

		got, _ := os.ReadFile(tmp)
		expected := "class Foo:\r\n    def bar(self):\r\n        \tz = 42\r\n        return z\r\n"
		if string(got) != expected {
			t.Errorf("indentation mismatch:\nExpected: %q\nGot:      %q", expected, string(got))
		}
	})

	// ─── Scenario 4: StartLine and EndLine bounds checking on CRLF files ───
	t.Run("Scenario4A_StartEndLine_ExactRange_CRLF", func(t *testing.T) {
		tmp := filepath.Join(t.TempDir(), "scenario4a.txt")
		fileContent := "alpha\r\nbeta\r\ngamma\r\ndelta\r\n"
		if err := os.WriteFile(tmp, []byte(fileContent), 0644); err != nil {
			t.Fatal(err)
		}

		_, err := registry.Execute(ctx, "replace_file_content", map[string]interface{}{
			"TargetFile":         tmp,
			"TargetContent":      "beta\ngamma",
			"ReplacementContent": "BETA\nGAMMA",
			"StartLine":          2,
			"EndLine":            3,
		})
		if err != nil {
			t.Fatalf("line range replace failed: %v", err)
		}

		got, _ := os.ReadFile(tmp)
		expected := "alpha\r\nBETA\r\nGAMMA\r\ndelta\r\n"
		if string(got) != expected {
			t.Errorf("line range mismatch:\nExpected: %q\nGot:      %q", expected, string(got))
		}
	})

	t.Run("Scenario4B_StartEndLine_FirstLine_CRLF", func(t *testing.T) {
		tmp := filepath.Join(t.TempDir(), "scenario4b.txt")
		fileContent := "first\r\nsecond\r\nthird\r\n"
		if err := os.WriteFile(tmp, []byte(fileContent), 0644); err != nil {
			t.Fatal(err)
		}

		_, err := registry.Execute(ctx, "replace_file_content", map[string]interface{}{
			"TargetFile":         tmp,
			"TargetContent":      "first",
			"ReplacementContent": "FIRST_MODIFIED",
			"StartLine":          1,
			"EndLine":            1,
		})
		if err != nil {
			t.Fatalf("first line replace failed: %v", err)
		}

		got, _ := os.ReadFile(tmp)
		expected := "FIRST_MODIFIED\r\nsecond\r\nthird\r\n"
		if string(got) != expected {
			t.Errorf("first line mismatch:\nExpected: %q\nGot:      %q", expected, string(got))
		}
	})

	t.Run("Scenario4C_StartEndLine_LastLine_CRLF", func(t *testing.T) {
		tmp := filepath.Join(t.TempDir(), "scenario4c.txt")
		fileContent := "first\r\nsecond\r\nthird\r\n"
		if err := os.WriteFile(tmp, []byte(fileContent), 0644); err != nil {
			t.Fatal(err)
		}

		_, err := registry.Execute(ctx, "replace_file_content", map[string]interface{}{
			"TargetFile":         tmp,
			"TargetContent":      "third",
			"ReplacementContent": "THIRD_MODIFIED",
			"StartLine":          3,
			"EndLine":            3,
		})
		if err != nil {
			t.Fatalf("last line replace failed: %v", err)
		}

		got, _ := os.ReadFile(tmp)
		expected := "first\r\nsecond\r\nTHIRD_MODIFIED\r\n"
		if string(got) != expected {
			t.Errorf("last line mismatch:\nExpected: %q\nGot:      %q", expected, string(got))
		}
	})

	t.Run("Scenario4D_StartLineExceedsLineCount", func(t *testing.T) {
		tmp := filepath.Join(t.TempDir(), "scenario4d.txt")
		fileContent := "line 1\r\nline 2\r\n"
		if err := os.WriteFile(tmp, []byte(fileContent), 0644); err != nil {
			t.Fatal(err)
		}

		_, err := registry.Execute(ctx, "replace_file_content", map[string]interface{}{
			"TargetFile":         tmp,
			"TargetContent":      "line 2",
			"ReplacementContent": "line 2 replaced",
			"StartLine":          10,
			"EndLine":            12,
		})
		if err == nil {
			t.Fatalf("expected error when StartLine exceeds line count, got nil")
		}
		if !strings.Contains(err.Error(), "exceeds line count") {
			t.Errorf("unexpected error message: %v", err)
		}
	})

	t.Run("Scenario4E_EndLineExceedsClamped", func(t *testing.T) {
		tmp := filepath.Join(t.TempDir(), "scenario4e.txt")
		fileContent := "line 1\r\nline 2\r\nline 3\r\n"
		if err := os.WriteFile(tmp, []byte(fileContent), 0644); err != nil {
			t.Fatal(err)
		}

		_, err := registry.Execute(ctx, "replace_file_content", map[string]interface{}{
			"TargetFile":         tmp,
			"TargetContent":      "line 2\nline 3",
			"ReplacementContent": "line 2+3 combined",
			"StartLine":          2,
			"EndLine":            100,
		})
		if err != nil {
			t.Fatalf("clamped EndLine failed: %v", err)
		}

		got, _ := os.ReadFile(tmp)
		expected := "line 1\r\nline 2+3 combined\r\n"
		if string(got) != expected {
			t.Errorf("clamped EndLine mismatch:\nExpected: %q\nGot:      %q", expected, string(got))
		}
	})

	t.Run("Scenario4F_TargetWithTrailingNewline_In_RangeChunk", func(t *testing.T) {
		tmp := filepath.Join(t.TempDir(), "scenario4f.txt")
		fileContent := "first\r\nsecond\r\nthird\r\n"
		if err := os.WriteFile(tmp, []byte(fileContent), 0644); err != nil {
			t.Fatal(err)
		}

		// When target includes trailing newline across the full range lines 1-2,
		// chunk is "first\nsecond" (no trailing newline on second line).
		// Target "first\nsecond\n" fails to match because chunk does not include trailing newline.
		_, err := registry.Execute(ctx, "replace_file_content", map[string]interface{}{
			"TargetFile":         tmp,
			"TargetContent":      "first\nsecond\n",
			"ReplacementContent": "one and two\n",
			"StartLine":          1,
			"EndLine":            2,
		})
		if err == nil {
			t.Errorf("expected error when range TargetContent includes terminal newline outside chunk, but got nil")
		} else {
			t.Logf("Empirical observation: range replacement with trailing newline correctly returns: %v", err)
		}
	})

	// ─── Scenario 5: Mixed line endings in the same file ───
	t.Run("Scenario5_MixedLineEndings", func(t *testing.T) {
		tmp := filepath.Join(t.TempDir(), "scenario5.txt")
		fileContent := "crlf line 1\r\nlf line 2\ncrlf line 3\r\n"
		if err := os.WriteFile(tmp, []byte(fileContent), 0644); err != nil {
			t.Fatal(err)
		}

		_, err := registry.Execute(ctx, "replace_file_content", map[string]interface{}{
			"TargetFile":         tmp,
			"TargetContent":      "lf line 2",
			"ReplacementContent": "unified line 2",
		})
		if err != nil {
			t.Fatalf("replace on mixed line endings failed: %v", err)
		}

		got, _ := os.ReadFile(tmp)
		expected := "crlf line 1\r\nunified line 2\r\ncrlf line 3\r\n"
		if string(got) != expected {
			t.Errorf("mixed line ending normalization mismatch:\nExpected: %q\nGot:      %q", expected, string(got))
		}

		nonCRLF := strings.ReplaceAll(string(got), "\r\n", "")
		if strings.Contains(nonCRLF, "\n") {
			t.Errorf("unnormalized LF remaining in mixed file output: %q", string(got))
		}
	})

	// ─── Scenario 6: Pure LF file (Unix) should NOT be converted to CRLF ───
	t.Run("Scenario6_PureLFFile_PreservesLF", func(t *testing.T) {
		tmp := filepath.Join(t.TempDir(), "scenario6.txt")
		fileContent := "line 1\nline 2\nline 3\n"
		if err := os.WriteFile(tmp, []byte(fileContent), 0644); err != nil {
			t.Fatal(err)
		}

		_, err := registry.Execute(ctx, "replace_file_content", map[string]interface{}{
			"TargetFile":         tmp,
			"TargetContent":      "line 2\r\n",
			"ReplacementContent": "line 2 modified\r\n",
		})
		if err != nil {
			t.Fatalf("replace failed: %v", err)
		}

		got, _ := os.ReadFile(tmp)
		expected := "line 1\nline 2 modified\nline 3\n"
		if string(got) != expected {
			t.Errorf("expected pure LF output:\nExpected: %q\nGot:      %q", expected, string(got))
		}
		if strings.Contains(string(got), "\r\n") {
			t.Errorf("pure LF file was contaminated with CRLF: %q", string(got))
		}
	})

	// ─── Scenario 7: File without trailing newline ───
	t.Run("Scenario7_FileWithoutTrailingNewline", func(t *testing.T) {
		tmp := filepath.Join(t.TempDir(), "scenario7.txt")
		fileContent := "line 1\r\nline 2"
		if err := os.WriteFile(tmp, []byte(fileContent), 0644); err != nil {
			t.Fatal(err)
		}

		_, err := registry.Execute(ctx, "replace_file_content", map[string]interface{}{
			"TargetFile":         tmp,
			"TargetContent":      "line 2",
			"ReplacementContent": "line 2 modified",
		})
		if err != nil {
			t.Fatalf("replace failed: %v", err)
		}

		got, _ := os.ReadFile(tmp)
		expected := "line 1\r\nline 2 modified"
		if string(got) != expected {
			t.Errorf("no-trailing-newline mismatch:\nExpected: %q\nGot:      %q", expected, string(got))
		}
	})

	// ─── Scenario 8: UTF-8 BOM with CRLF file ───
	t.Run("Scenario8_UTF8BOM_With_CRLF", func(t *testing.T) {
		tmp := filepath.Join(t.TempDir(), "scenario8.txt")
		fileContent := "\xef\xbb\xbfline 1\r\nline 2\r\nline 3\r\n"
		if err := os.WriteFile(tmp, []byte(fileContent), 0644); err != nil {
			t.Fatal(err)
		}

		_, err := registry.Execute(ctx, "replace_file_content", map[string]interface{}{
			"TargetFile":         tmp,
			"TargetContent":      "line 2",
			"ReplacementContent": "line 2 BOM OK",
		})
		if err != nil {
			t.Fatalf("BOM replace failed: %v", err)
		}

		got, _ := os.ReadFile(tmp)
		expected := "\xef\xbb\xbfline 1\r\nline 2 BOM OK\r\nline 3\r\n"
		if string(got) != expected {
			t.Errorf("BOM preservation mismatch:\nExpected: %q\nGot:      %q", expected, string(got))
		}
	})

	// ─── Scenario 9: Deletion via Empty Replacement Content on CRLF ───
	t.Run("Scenario9_Deletion_Global_CRLF", func(t *testing.T) {
		tmp := filepath.Join(t.TempDir(), "scenario9.txt")
		fileContent := "line 1\r\nline 2 to delete\r\nline 3\r\n"
		if err := os.WriteFile(tmp, []byte(fileContent), 0644); err != nil {
			t.Fatal(err)
		}

		_, err := registry.Execute(ctx, "replace_file_content", map[string]interface{}{
			"TargetFile":         tmp,
			"TargetContent":      "line 2 to delete\r\n",
			"ReplacementContent": "",
		})
		if err != nil {
			t.Fatalf("deletion failed: %v", err)
		}

		got, _ := os.ReadFile(tmp)
		expected := "line 1\r\nline 3\r\n"
		if string(got) != expected {
			t.Errorf("deletion mismatch:\nExpected: %q\nGot:      %q", expected, string(got))
		}
	})

	// ─── Scenario 10: AllowMultiple=true with multiple CRLF targets ───
	t.Run("Scenario10_AllowMultiple_True_CRLF", func(t *testing.T) {
		tmp := filepath.Join(t.TempDir(), "scenario10.txt")
		fileContent := "repeat\r\nmiddle\r\nrepeat\r\nend\r\nrepeat\r\n"
		if err := os.WriteFile(tmp, []byte(fileContent), 0644); err != nil {
			t.Fatal(err)
		}

		_, err := registry.Execute(ctx, "replace_file_content", map[string]interface{}{
			"TargetFile":         tmp,
			"TargetContent":      "repeat",
			"ReplacementContent": "UPDATED",
			"AllowMultiple":      true,
		})
		if err != nil {
			t.Fatalf("AllowMultiple=true failed: %v", err)
		}

		got, _ := os.ReadFile(tmp)
		expected := "UPDATED\r\nmiddle\r\nUPDATED\r\nend\r\nUPDATED\r\n"
		if string(got) != expected {
			t.Errorf("AllowMultiple mismatch:\nExpected: %q\nGot:      %q", expected, string(got))
		}
	})

	// ─── Scenario 11: AllowMultiple=false with multiple CRLF targets (error expected) ───
	t.Run("Scenario11_AllowMultiple_False_DuplicateError", func(t *testing.T) {
		tmp := filepath.Join(t.TempDir(), "scenario11.txt")
		fileContent := "dup\r\nmiddle\r\ndup\r\n"
		if err := os.WriteFile(tmp, []byte(fileContent), 0644); err != nil {
			t.Fatal(err)
		}

		_, err := registry.Execute(ctx, "replace_file_content", map[string]interface{}{
			"TargetFile":         tmp,
			"TargetContent":      "dup",
			"ReplacementContent": "UPDATED",
			"AllowMultiple":      false,
		})
		if err == nil {
			t.Fatalf("expected error for duplicate target without AllowMultiple, got nil")
		}
		if !strings.Contains(err.Error(), "found 2 times") {
			t.Errorf("unexpected error message: %v", err)
		}
	})

	// ─── Scenario 12: Large multiline replacement with diverse indentation ───
	t.Run("Scenario12_LargeMultiline_ComplexIndentation_CRLF", func(t *testing.T) {
		tmp := filepath.Join(t.TempDir(), "scenario12.txt")
		var sb strings.Builder
		sb.WriteString("func complex() {\r\n")
		sb.WriteString("\tif true {\r\n")
		sb.WriteString("\t\tfor i := 0; i < 10; i++ {\r\n")
		sb.WriteString("            // 12 spaces comment\r\n")
		sb.WriteString("\t\t\tif i%2 == 0 {\r\n")
		sb.WriteString("\t\t\t\tprintln(i)\r\n")
		sb.WriteString("\t\t\t}\r\n")
		sb.WriteString("\t\t}\r\n")
		sb.WriteString("\t}\r\n")
		sb.WriteString("}\r\n")
		if err := os.WriteFile(tmp, []byte(sb.String()), 0644); err != nil {
			t.Fatal(err)
		}

		// Replace loop body using LF
		targetLF := "\t\tfor i := 0; i < 10; i++ {\n            // 12 spaces comment\n\t\t\tif i%2 == 0 {\n\t\t\t\tprintln(i)\n\t\t\t}\n\t\t}"
		replacementLF := "\t\tfor _, val := range items {\n            // new 12 spaces\n\t\t\tprocess(val)\n\t\t}"

		_, err := registry.Execute(ctx, "replace_file_content", map[string]interface{}{
			"TargetFile":         tmp,
			"TargetContent":      targetLF,
			"ReplacementContent": replacementLF,
		})
		if err != nil {
			t.Fatalf("complex multiline replace failed: %v", err)
		}

		got, _ := os.ReadFile(tmp)
		expected := "func complex() {\r\n\tif true {\r\n\t\tfor _, val := range items {\r\n            // new 12 spaces\r\n\t\t\tprocess(val)\r\n\t\t}\r\n\t}\r\n}\r\n"
		if string(got) != expected {
			t.Errorf("complex multiline mismatch:\nExpected: %q\nGot:      %q", expected, string(got))
		}
	})

	// ─── Scenario 13: Consecutive blank lines (empty CRLFs) ───
	t.Run("Scenario13_ConsecutiveBlankLines_CRLF", func(t *testing.T) {
		tmp := filepath.Join(t.TempDir(), "scenario13.txt")
		fileContent := "start\r\n\r\n\r\nend\r\n"
		if err := os.WriteFile(tmp, []byte(fileContent), 0644); err != nil {
			t.Fatal(err)
		}

		// Replace the 2 blank lines with 1 blank line
		_, err := registry.Execute(ctx, "replace_file_content", map[string]interface{}{
			"TargetFile":         tmp,
			"TargetContent":      "start\n\n\nend",
			"ReplacementContent": "start\n\nend",
		})
		if err != nil {
			t.Fatalf("blank line replace failed: %v", err)
		}

		got, _ := os.ReadFile(tmp)
		expected := "start\r\n\r\nend\r\n"
		if string(got) != expected {
			t.Errorf("blank lines mismatch:\nExpected: %q\nGot:      %q", expected, string(got))
		}
	})

	// ─── Scenario 14: Special characters and Unicode in CRLF file ───
	t.Run("Scenario14_SpecialCharsAndUnicode_CRLF", func(t *testing.T) {
		tmp := filepath.Join(t.TempDir(), "scenario14.txt")
		fileContent := "line 1: \"quotes\" & <tags>\r\nline 2: 🚀 Unicode 日本語\r\nline 3: \\path\\to\\file\r\n"
		if err := os.WriteFile(tmp, []byte(fileContent), 0644); err != nil {
			t.Fatal(err)
		}

		target := "line 2: 🚀 Unicode 日本語"
		replacement := "line 2: ✨ Success 成功"

		_, err := registry.Execute(ctx, "replace_file_content", map[string]interface{}{
			"TargetFile":         tmp,
			"TargetContent":      target,
			"ReplacementContent": replacement,
		})
		if err != nil {
			t.Fatalf("unicode replace failed: %v", err)
		}

		got, _ := os.ReadFile(tmp)
		expected := "line 1: \"quotes\" & <tags>\r\nline 2: ✨ Success 成功\r\nline 3: \\path\\to\\file\r\n"
		if string(got) != expected {
			t.Errorf("unicode mismatch:\nExpected: %q\nGot:      %q", expected, string(got))
		}
	})
}

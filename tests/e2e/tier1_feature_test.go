package e2e_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	mgcontext "github.com/CmfH009/microgravity/internal/context"
	"github.com/CmfH009/microgravity/internal/events"
	"github.com/CmfH009/microgravity/internal/git"
	"github.com/CmfH009/microgravity/internal/policy"
	"github.com/CmfH009/microgravity/internal/pruner"
	"github.com/CmfH009/microgravity/internal/subagent"
)

// ══════════════════════════════════════════════════════════════════
// TIER 1: FEATURE F15 — @file Context Reference Ingestion
// ══════════════════════════════════════════════════════════════════

func TestTier1_F15_SingleFileInjection(t *testing.T) {
	tempDir := t.TempDir()
	filePath := filepath.Join(tempDir, "config.json")
	fileContent := `{"port": 8080, "host": "127.0.0.1"}`
	if err := os.WriteFile(filePath, []byte(fileContent), 0644); err != nil {
		t.Fatalf("failed to create sample file: %v", err)
	}

	prompt := fmt.Sprintf("Analyze configuration in @%s please", filePath)
	cleanPrompt, injectedContext, err := mgcontext.ProcessFileReferences(prompt)
	if err != nil {
		t.Fatalf("ProcessFileReferences error: %v", err)
	}

	expectedClean := fmt.Sprintf("Analyze configuration in [%s] please", filePath)
	if cleanPrompt != expectedClean {
		t.Fatalf("cleanPrompt mismatch: expected %q, got %q", expectedClean, cleanPrompt)
	}

	if !strings.Contains(injectedContext, fileContent) {
		t.Fatalf("expected injectedContext to contain %q, got %q", fileContent, injectedContext)
	}

	// Verify file content appears exactly once in the injected context (deduplication)
	count := strings.Count(injectedContext, fileContent)
	if count != 1 {
		t.Fatalf("expected file content to be injected exactly 1 time, got %d times", count)
	}
}

func TestTier1_F15_MultipleDifferentFiles(t *testing.T) {
	tempDir := t.TempDir()
	file1 := filepath.Join(tempDir, "server.go")
	content1 := "package main\nfunc StartServer() {}"
	if err := os.WriteFile(file1, []byte(content1), 0644); err != nil {
		t.Fatalf("failed to write file1: %v", err)
	}

	file2 := filepath.Join(tempDir, "client.go")
	content2 := "package main\nfunc ConnectClient() {}"
	if err := os.WriteFile(file2, []byte(content2), 0644); err != nil {
		t.Fatalf("failed to write file2: %v", err)
	}

	prompt := fmt.Sprintf("Compare @%s with @%s", file1, file2)
	cleanPrompt, injectedContext, err := mgcontext.ProcessFileReferences(prompt)
	if err != nil {
		t.Fatalf("ProcessFileReferences failed: %v", err)
	}

	if !strings.Contains(cleanPrompt, fmt.Sprintf("[%s]", file1)) || !strings.Contains(cleanPrompt, fmt.Sprintf("[%s]", file2)) {
		t.Fatalf("clean prompt did not replace references properly: %s", cleanPrompt)
	}

	if strings.Count(injectedContext, content1) != 1 {
		t.Fatalf("file1 content should be injected exactly once")
	}
	if strings.Count(injectedContext, content2) != 1 {
		t.Fatalf("file2 content should be injected exactly once")
	}
}

func TestTier1_F15_NonExistentFilePreservation(t *testing.T) {
	tempDir := t.TempDir()
	missingPath := filepath.Join(tempDir, "does_not_exist_xyz.go")

	prompt := fmt.Sprintf("Check status of @%s now", missingPath)
	cleanPrompt, injectedContext, err := mgcontext.ProcessFileReferences(prompt)
	if err != nil {
		t.Fatalf("ProcessFileReferences returned unexpected error: %v", err)
	}

	// Non-existent file token must remain untouched in the prompt
	if !strings.Contains(cleanPrompt, fmt.Sprintf("@%s", missingPath)) {
		t.Fatalf("expected missing file token to remain in prompt, got %s", cleanPrompt)
	}

	if injectedContext != "" {
		t.Fatalf("expected empty injectedContext for missing file, got %q", injectedContext)
	}
}

func TestTier1_F15_QuotedPathHandling(t *testing.T) {
	tempDir := t.TempDir()
	filePath := filepath.Join(tempDir, "quoted_data.txt")
	content := "alpha beta gamma delta"
	if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write data: %v", err)
	}

	prompt := fmt.Sprintf("Inspect @\"%s\" carefully", filePath)
	cleanPrompt, injectedContext, err := mgcontext.ProcessFileReferences(prompt)
	if err != nil {
		t.Fatalf("ProcessFileReferences failed: %v", err)
	}

	if !strings.Contains(cleanPrompt, fmt.Sprintf("[%s]", filePath)) {
		t.Fatalf("expected quotes to be stripped and path resolved: %s", cleanPrompt)
	}
	if !strings.Contains(injectedContext, content) {
		t.Fatalf("expected injectedContext to contain %q", content)
	}
}

func TestTier1_F15_DirectoryContextInjection(t *testing.T) {
	tempDir := t.TempDir()
	subDir := filepath.Join(tempDir, "pkg_test")
	if err := os.MkdirAll(subDir, 0755); err != nil {
		t.Fatalf("failed to create dir: %v", err)
	}

	_ = os.WriteFile(filepath.Join(subDir, "main.go"), []byte("package main"), 0644)
	_ = os.WriteFile(filepath.Join(subDir, "util.go"), []byte("package main"), 0644)
	_ = os.MkdirAll(filepath.Join(subDir, "nested"), 0755)

	prompt := fmt.Sprintf("List files in @%s", subDir)
	cleanPrompt, injectedContext, err := mgcontext.ProcessFileReferences(prompt)
	if err != nil {
		t.Fatalf("ProcessFileReferences failed: %v", err)
	}

	if !strings.Contains(cleanPrompt, fmt.Sprintf("[%s]", subDir)) {
		t.Fatalf("expected clean prompt to reference directory")
	}

	if !strings.Contains(injectedContext, fmt.Sprintf("[Directory Context from @%s]:", subDir)) {
		t.Fatalf("directory context header missing: %s", injectedContext)
	}
	if !strings.Contains(injectedContext, "- [FILE] main.go") || !strings.Contains(injectedContext, "- [FILE] util.go") {
		t.Fatalf("file listing missing from directory context: %s", injectedContext)
	}
	if !strings.Contains(injectedContext, "- [DIR]  nested") {
		t.Fatalf("nested directory listing missing: %s", injectedContext)
	}
}

func TestTier1_F15_SingleInjectionDeduplicationInTurn(t *testing.T) {
	tempDir := t.TempDir()
	filePath := filepath.Join(tempDir, "shared.txt")
	fileContent := "SINGLE_INJECTION_UNIQUE_TOKEN_12345"
	_ = os.WriteFile(filePath, []byte(fileContent), 0644)

	prompt := fmt.Sprintf("Read @%s once", filePath)
	_, injectedContext, err := mgcontext.ProcessFileReferences(prompt)
	if err != nil {
		t.Fatalf("ProcessFileReferences failed: %v", err)
	}

	// Verify only 1 injection block is produced
	contextHeader := fmt.Sprintf("[File Context from @%s]:", filePath)
	if strings.Count(injectedContext, contextHeader) != 1 {
		t.Fatalf("expected exactly 1 file context header, got %d", strings.Count(injectedContext, contextHeader))
	}
	if strings.Count(injectedContext, fileContent) != 1 {
		t.Fatalf("expected file content to be injected exactly 1 time, got %d", strings.Count(injectedContext, fileContent))
	}
}

// ══════════════════════════════════════════════════════════════════
// TIER 1: FEATURE F16 — CRLF Normalization in File Editing
// ══════════════════════════════════════════════════════════════════

func TestTier1_F16_ExactCRLFFileMatching(t *testing.T) {
	tempDir := t.TempDir()
	targetFile := filepath.Join(tempDir, "service.go")
	initialContent := "package service\r\n\r\nfunc Init() {\r\n\tprintln(\"old\")\r\n}\r\n"
	if err := os.WriteFile(targetFile, []byte(initialContent), 0644); err != nil {
		t.Fatalf("failed to write initial file: %v", err)
	}

	pol := policy.New([]string{tempDir}, true, false)
	registry, _ := createTestRegistry(t, pol)

	ctx := context.Background()
	_, err := registry.Execute(ctx, "replace_file_content", map[string]interface{}{
		"TargetFile":         targetFile,
		"TargetContent":      "func Init() {\r\n\tprintln(\"old\")\r\n}",
		"ReplacementContent": "func Init() {\r\n\tprintln(\"new\")\r\n}",
	})
	if err != nil {
		t.Fatalf("replace_file_content failed: %v", err)
	}

	assertCRLF(t, targetFile)
	data, _ := os.ReadFile(targetFile)
	if !strings.Contains(string(data), "println(\"new\")") {
		t.Fatalf("replacement text not found in updated file: %s", string(data))
	}
}

func TestTier1_F16_LFQueryAgainstCRLFFile(t *testing.T) {
	tempDir := t.TempDir()
	targetFile := filepath.Join(tempDir, "handler.go")
	initialContent := "package handler\r\n\r\nfunc HandleRequest() error {\r\n\treturn nil\r\n}\r\n"
	_ = os.WriteFile(targetFile, []byte(initialContent), 0644)

	pol := policy.New([]string{tempDir}, true, false)
	registry, _ := createTestRegistry(t, pol)

	// TargetContent uses Unix LF (\n), while file on disk uses Windows CRLF (\r\n)
	ctx := context.Background()
	_, err := registry.Execute(ctx, "replace_file_content", map[string]interface{}{
		"TargetFile":         targetFile,
		"TargetContent":      "func HandleRequest() error {\n\treturn nil\n}",
		"ReplacementContent": "func HandleRequest() error {\n\treturn fmt.Errorf(\"handled\")\n}",
	})
	if err != nil {
		t.Fatalf("replace_file_content failed with LF query against CRLF file: %v", err)
	}

	// Verify line endings remain CRLF on disk after normalization
	assertCRLF(t, targetFile)
	data, _ := os.ReadFile(targetFile)
	if !strings.Contains(string(data), "fmt.Errorf(\"handled\")") {
		t.Fatalf("replacement content missing: %s", string(data))
	}
}

func TestTier1_F16_CRLFQueryAgainstLFFile(t *testing.T) {
	tempDir := t.TempDir()
	targetFile := filepath.Join(tempDir, "unix_file.go")
	initialContent := "package unix\n\nfunc Run() {\n\tprintln(\"unix\")\n}\n"
	_ = os.WriteFile(targetFile, []byte(initialContent), 0644)

	pol := policy.New([]string{tempDir}, true, false)
	registry, _ := createTestRegistry(t, pol)

	// TargetContent uses Windows CRLF (\r\n), while file on disk uses Unix LF (\n)
	ctx := context.Background()
	_, err := registry.Execute(ctx, "replace_file_content", map[string]interface{}{
		"TargetFile":         targetFile,
		"TargetContent":      "func Run() {\r\n\tprintln(\"unix\")\r\n}",
		"ReplacementContent": "func Run() {\r\n\tprintln(\"modern\")\r\n}",
	})
	if err != nil {
		t.Fatalf("replace_file_content failed with CRLF query against LF file: %v", err)
	}

	// Verify line endings remain LF on disk (not converted to CRLF)
	assertLF(t, targetFile)
	data, _ := os.ReadFile(targetFile)
	if !strings.Contains(string(data), "println(\"modern\")") {
		t.Fatalf("replacement content missing: %s", string(data))
	}
}

func TestTier1_F16_LineRangeRestrictedCRLF(t *testing.T) {
	tempDir := t.TempDir()
	targetFile := filepath.Join(tempDir, "ranged.go")
	lines := []string{
		"package main",
		"func A() {}",
		"func Target() {",
		"\t// body 1",
		"}",
		"func Target() {",
		"\t// body 2",
		"}",
	}
	initialContent := strings.Join(lines, "\r\n") + "\r\n"
	_ = os.WriteFile(targetFile, []byte(initialContent), 0644)

	pol := policy.New([]string{tempDir}, true, false)
	registry, _ := createTestRegistry(t, pol)

	ctx := context.Background()
	// Replace only within lines 3 to 5
	_, err := registry.Execute(ctx, "replace_file_content", map[string]interface{}{
		"TargetFile":         targetFile,
		"TargetContent":      "func Target() {\n\t// body 1\n}",
		"ReplacementContent": "func TargetReplaced() {\n\t// replaced body\n}",
		"StartLine":          3,
		"EndLine":            5,
	})
	if err != nil {
		t.Fatalf("range restricted replace failed: %v", err)
	}

	assertCRLF(t, targetFile)
	data, _ := os.ReadFile(targetFile)
	str := string(data)
	if !strings.Contains(str, "func TargetReplaced()") {
		t.Fatalf("first target not replaced")
	}
	if !strings.Contains(str, "// body 2") {
		t.Fatalf("second target outside range should remain untouched")
	}
}

func TestTier1_F16_AllowMultipleCRLFReplacement(t *testing.T) {
	tempDir := t.TempDir()
	targetFile := filepath.Join(tempDir, "multi.go")
	initialContent := "const MaxAttempts = 3\r\nconst Retries = 3\r\n"
	_ = os.WriteFile(targetFile, []byte(initialContent), 0644)

	pol := policy.New([]string{tempDir}, true, false)
	registry, _ := createTestRegistry(t, pol)

	ctx := context.Background()
	_, err := registry.Execute(ctx, "replace_file_content", map[string]interface{}{
		"TargetFile":         targetFile,
		"TargetContent":      "= 3",
		"ReplacementContent": "= 10",
		"AllowMultiple":      true,
	})
	if err != nil {
		t.Fatalf("replace with AllowMultiple failed: %v", err)
	}

	assertCRLF(t, targetFile)
	data, _ := os.ReadFile(targetFile)
	if strings.Contains(string(data), "= 3") {
		t.Fatalf("expected all occurrences to be replaced: %s", string(data))
	}
	if strings.Count(string(data), "= 10") != 2 {
		t.Fatalf("expected 2 replacements of '= 10', got: %s", string(data))
	}
}

func TestTier1_F16_DuplicateTargetWithoutAllowMultipleFails(t *testing.T) {
	tempDir := t.TempDir()
	targetFile := filepath.Join(tempDir, "dup.go")
	initialContent := "val = 42\r\nval = 42\r\n"
	_ = os.WriteFile(targetFile, []byte(initialContent), 0644)

	pol := policy.New([]string{tempDir}, true, false)
	registry, _ := createTestRegistry(t, pol)

	ctx := context.Background()
	_, err := registry.Execute(ctx, "replace_file_content", map[string]interface{}{
		"TargetFile":         targetFile,
		"TargetContent":      "val = 42",
		"ReplacementContent": "val = 99",
		"AllowMultiple":      false,
	})
	if err == nil {
		t.Fatalf("expected error for duplicate target without AllowMultiple=true")
	}
	if !strings.Contains(err.Error(), "found 2 times") {
		t.Fatalf("expected error mentioning count, got: %v", err)
	}
}

// ══════════════════════════════════════════════════════════════════
// TIER 1: FEATURE F4 — Policy Engine Roots & Sandbox Isolation
// ══════════════════════════════════════════════════════════════════

func TestTier1_F4_DefaultPolicyAllowsCwdAndTemp(t *testing.T) {
	cwd, _ := os.Getwd()
	temp := os.TempDir()

	pol := policy.New([]string{cwd, temp}, true, false)

	// File inside cwd must resolve
	sampleCwdFile := filepath.Join(cwd, "test_file.go")
	resolved, err := pol.ResolvePath(sampleCwdFile)
	if err != nil {
		t.Fatalf("expected cwd file to resolve, got: %v", err)
	}
	if resolved != filepath.Clean(sampleCwdFile) {
		t.Fatalf("resolved mismatch: %s vs %s", resolved, sampleCwdFile)
	}

	// File inside temp must resolve
	sampleTempFile := filepath.Join(temp, "test_temp.txt")
	resolvedTemp, err := pol.ResolvePath(sampleTempFile)
	if err != nil {
		t.Fatalf("expected temp file to resolve, got: %v", err)
	}
	if resolvedTemp != filepath.Clean(sampleTempFile) {
		t.Fatalf("resolved temp mismatch: %s vs %s", resolvedTemp, sampleTempFile)
	}
}

func TestTier1_F4_OutsidePathRejectedByDefault(t *testing.T) {
	safeDir := t.TempDir()
	pol := policy.New([]string{safeDir}, true, false)

	// Outside path completely isolated from safeDir
	outsidePath := filepath.Join(os.TempDir(), "completely_unrelated_dir", "secret.env")
	_, err := pol.ResolvePath(outsidePath)
	if err == nil {
		t.Fatalf("expected outside path %s to be rejected by policy", outsidePath)
	}
	if !strings.Contains(err.Error(), "outside approved workspace roots") {
		t.Fatalf("expected error 'outside approved workspace roots', got: %v", err)
	}
}

func TestTier1_F4_AddDirAllowsExternalDirectory(t *testing.T) {
	baseDir := t.TempDir()
	extraDir := t.TempDir()

	// Initially without extraDir
	pol := policy.New([]string{baseDir}, true, false)
	testFile := filepath.Join(extraDir, "shared_lib.go")
	if _, err := pol.ResolvePath(testFile); err == nil {
		t.Fatalf("expected file in extraDir to be rejected initially")
	}

	// Add extraDir to approved roots (simulating --add-dir flag)
	polWithExtra := policy.New([]string{baseDir, extraDir}, true, false)
	resolved, err := polWithExtra.ResolvePath(testFile)
	if err != nil {
		t.Fatalf("expected file in extraDir to be allowed after add-dir, got: %v", err)
	}
	if resolved != filepath.Clean(testFile) {
		t.Fatalf("resolved path mismatch: %s vs %s", resolved, testFile)
	}
}

func TestTier1_F4_MultipleAddDirs(t *testing.T) {
	dir1 := t.TempDir()
	dir2 := t.TempDir()
	dir3 := t.TempDir()

	roots := []string{dir1, dir2, dir3}
	pol := policy.New(roots, true, false)

	for _, d := range roots {
		target := filepath.Join(d, "file.txt")
		if _, err := pol.ResolvePath(target); err != nil {
			t.Fatalf("root %s should be permitted: %v", d, err)
		}
	}
}

func TestTier1_F4_SandboxFlagDisablesHostCommands(t *testing.T) {
	safeDir := t.TempDir()
	// Sandbox active: allowHostCommands = false, sandboxEnabled = true
	sandboxPolicy := policy.New([]string{safeDir}, false, true)

	if !sandboxPolicy.SandboxEnabled() {
		t.Fatalf("expected SandboxEnabled() to be true")
	}

	err := sandboxPolicy.CheckCommand("echo 'exploit'")
	if err == nil {
		t.Fatalf("expected CheckCommand to reject host execution when sandboxed")
	}
	if !strings.Contains(err.Error(), "host command execution is disabled by policy") {
		t.Fatalf("expected 'host command execution is disabled by policy', got: %v", err)
	}
}

func TestTier1_F4_PolicyWorkingDirectoryValidation(t *testing.T) {
	safeDir := t.TempDir()
	pol := policy.New([]string{safeDir}, true, false)

	// Valid working directory
	validSub := filepath.Join(safeDir, "project_a")
	_ = os.MkdirAll(validSub, 0755)
	checked, err := pol.CheckWorkingDirectory(validSub)
	if err != nil {
		t.Fatalf("expected valid working directory, got: %v", err)
	}
	if checked != filepath.Clean(validSub) {
		t.Fatalf("mismatch checked working dir: %s", checked)
	}

	// Non-directory file passed as working dir
	filePath := filepath.Join(safeDir, "not_a_dir.txt")
	_ = os.WriteFile(filePath, []byte("text"), 0644)
	if _, err := pol.CheckWorkingDirectory(filePath); err == nil {
		t.Fatalf("expected error when file passed as working directory")
	}
}

// ══════════════════════════════════════════════════════════════════
// TIER 1: FEATURE F5 — Pruner Preservation of Turn 0 & Directives
// ══════════════════════════════════════════════════════════════════

func TestTier1_F5_Turn0RootIntentPreserved(t *testing.T) {
	turn0 := "User: CRITICAL_GOAL: Build secure microservice architecture with Go\nAgent: Understood, beginning implementation."
	
	var turns []string
	turns = append(turns, turn0)
	for i := 1; i <= 20; i++ {
		turns = append(turns, fmt.Sprintf("User: Iteration %d log report data lorem ipsum dolor sit amet\nAgent: Step %d processed successfully.", i, i))
	}
	fullConvo := strings.Join(turns, "\n")

	// Set maxTokens budget smaller than full conversation
	budget := 100 // tight budget to force aggressive pruning
	pruned, removed := pruner.PruneContext(fullConvo, budget)

	if removed <= 0 {
		t.Fatalf("expected tokens to be pruned, removed=%d", removed)
	}

	// Turn 0 user intent MUST be preserved at the beginning
	if !strings.Contains(pruned, "CRITICAL_GOAL: Build secure microservice architecture with Go") {
		t.Fatalf("Turn 0 root objective was evicted by pruner: %s", pruned)
	}

	// Verify pruned marker is present
	if !strings.Contains(pruned, "[Earlier conversation context pruned for token efficiency]") {
		t.Fatalf("missing pruner marker in output: %s", pruned)
	}
}

func TestTier1_F5_PruningEfficiencyUnderBudget(t *testing.T) {
	convo := "User: Quick query\nAgent: Quick response."
	pruned, removed := pruner.PruneContext(convo, 5000)

	if removed != 0 {
		t.Fatalf("expected 0 tokens removed when under budget, got %d", removed)
	}
	if pruned != convo {
		t.Fatalf("conversation should not be altered when under budget")
	}
}

func TestTier1_F5_MiddleTurnsPrunedWithMarker(t *testing.T) {
	turn0 := "User: Objective: Optimize SQL queries\nAgent: Commencing database audit."
	turnMiddle := "User: Middle step with lots of verbose debugging output that should be evicted\nAgent: Detailed debug stack trace."
	turnRecent := "User: Final status check\nAgent: All queries optimized."

	convo := strings.Join([]string{turn0, turnMiddle, turnRecent}, "\n")
	budget := pruner.EstimateTokens(turn0) + pruner.EstimateTokens(turnRecent) + 10

	pruned, _ := pruner.PruneContext(convo, budget)

	if !strings.Contains(pruned, "Optimize SQL queries") {
		t.Fatalf("Turn 0 must be preserved")
	}
	if !strings.Contains(pruned, "Final status check") {
		t.Fatalf("Recent turn must be preserved")
	}
	if !strings.Contains(pruned, "[Earlier conversation context pruned for token efficiency]") {
		t.Fatalf("Pruner marker must separate Turn 0 from recent turns")
	}
}

func TestTier1_F5_ModelDynamicContextBudgetScaling(t *testing.T) {
	testCases := []struct {
		model    string
		expected int
	}{
		{"gemini-3.1-pro", 256000},
		{"gemini-3.6-flash", 128000},
		{"flash", 128000},
		{"pro", 128000},
		{"gemma-4", 8192},
		{"orbit-sentinel", 8192},
		{"codegemma", 8192},
		{"llama-3-8b", 64000},
		{"custom-unknown-model", 16000},
	}

	for _, tc := range testCases {
		got := pruner.MaxTokensForModel(tc.model)
		if got != tc.expected {
			t.Errorf("MaxTokensForModel(%q) = %d, expected %d", tc.model, got, tc.expected)
		}
	}
}

func TestTier1_F5_ExtremeTurn0SizeCapped(t *testing.T) {
	// Under M2 specifications, Turn 0 root user prompt is strictly preserved without truncation
	hugePayload := strings.Repeat("A", 2000)
	turn0 := fmt.Sprintf("User: Huge initial specification: %s\nAgent: Ack.", hugePayload)
	recent := "User: Recent task\nAgent: Done."
	convo := turn0 + "\n" + recent

	budget := 100
	pruned, _ := pruner.PruneContext(convo, budget)

	if !strings.Contains(pruned, "Huge initial specification") {
		t.Fatalf("expected Turn 0 root prompt to be preserved, got: %s", pruned)
	}
	if strings.Contains(pruned, "[Turn 0 truncated]") {
		t.Fatalf("Turn 0 must not be truncated under M2 specification")
	}
}

func TestTier1_F5_FallbackWithoutExplicitUserTurns(t *testing.T) {
	// Arbitrary conversation text without "User: " prefixes
	var lines []string
	for i := 0; i < 50; i++ {
		lines = append(lines, fmt.Sprintf("system telemetry line %d data entry", i))
	}
	rawText := strings.Join(lines, "\n")

	budget := 40
	pruned, removed := pruner.PruneContext(rawText, budget)

	if removed <= 0 {
		t.Fatalf("expected pruning to remove tokens from fallback text")
	}
	if !strings.Contains(pruned, "[Earlier conversation context pruned for token efficiency]") {
		t.Fatalf("expected fallback pruning to include pruner marker")
	}
	if !strings.Contains(pruned, "system telemetry line 49") {
		t.Fatalf("expected latest lines to be preserved in fallback pruning")
	}
}

// ══════════════════════════════════════════════════════════════════
// TIER 1: FEATURE F17 — Subagent Worktree Isolation Execution
// ══════════════════════════════════════════════════════════════════

func TestTier1_F17_SubagentBranchWorktreeCreation(t *testing.T) {
	repoDir := initGitRepo(t)
	mgr := subagent.NewManager(nil, t.TempDir())

	mgr.SetWorktreeProvider(
		func(cwd, name string) (string, error) {
			return git.CreateWorktree(repoDir, name)
		},
		func(cwd, name string) error {
			return git.RemoveWorktree(repoDir, name)
		},
	)

	ctx := context.Background()
	info, err := mgr.Spawn(ctx, "sub-branch-test", "QA Specialist", subagent.ProfileQAAuditor, "Audit code", "", subagent.WorkspaceBranch,
		func(ctx context.Context, sub *subagent.SubagentInfo) (string, error) {
			return "Audit completed inside worktree", nil
		},
	)
	if err != nil {
		t.Fatalf("Spawn failed: %v", err)
	}

	snap := waitForSubagent(t, mgr, info.ID, 10*time.Second)
	if snap.Status != "COMPLETED" {
		t.Fatalf("expected subagent to complete, got status %s", snap.Status)
	}
}

func TestTier1_F17_SubagentBranchWorktreeCleanup(t *testing.T) {
	repoDir := initGitRepo(t)
	mgr := subagent.NewManager(nil, t.TempDir())

	removedCalled := false
	var mu sync.Mutex

	mgr.SetWorktreeProvider(
		func(cwd, name string) (string, error) {
			return git.CreateWorktree(repoDir, name)
		},
		func(cwd, name string) error {
			mu.Lock()
			removedCalled = true
			mu.Unlock()
			return git.RemoveWorktree(repoDir, name)
		},
	)

	ctx := context.Background()
	info, err := mgr.Spawn(ctx, "cleanup-test", "Cleaner", subagent.ProfileGeneral, "Clean workspace", "", subagent.WorkspaceBranch,
		func(ctx context.Context, sub *subagent.SubagentInfo) (string, error) {
			return "Cleaned", nil
		},
	)
	if err != nil {
		t.Fatalf("Spawn failed: %v", err)
	}

	snap := waitForSubagent(t, mgr, info.ID, 10*time.Second)
	if snap.Status != "COMPLETED" {
		t.Fatalf("expected subagent to complete, got %s", snap.Status)
	}

	mu.Lock()
	defer mu.Unlock()
	if !removedCalled {
		t.Fatalf("expected worktree remover to be called after clean completion")
	}
}

func TestTier1_F17_SubagentInheritWorkspace(t *testing.T) {
	mgr := subagent.NewManager(nil, t.TempDir())
	creatorCalled := false

	mgr.SetWorktreeProvider(
		func(cwd, name string) (string, error) {
			creatorCalled = true
			return "", nil
		},
		nil,
	)

	ctx := context.Background()
	_, err := mgr.Spawn(ctx, "inherit-test", "General", subagent.ProfileGeneral, "Run inherit", "", subagent.WorkspaceInherit,
		func(ctx context.Context, sub *subagent.SubagentInfo) (string, error) {
			return "Inherited executed", nil
		},
	)
	if err != nil {
		t.Fatalf("Spawn failed: %v", err)
	}

	time.Sleep(100 * time.Millisecond)

	if creatorCalled {
		t.Fatalf("WorkspaceInherit should NOT trigger worktree creation")
	}
}

func TestTier1_F17_SubagentIsolatedFileModification(t *testing.T) {
	repoDir := initGitRepo(t)
	mgr := subagent.NewManager(nil, t.TempDir())

	var wtDir string
	mgr.SetWorktreeProvider(
		func(cwd, name string) (string, error) {
			dir, err := git.CreateWorktree(repoDir, name)
			wtDir = dir
			return dir, err
		},
		nil, // Do not remove immediately so we can inspect
	)
	defer func() {
		if wtDir != "" {
			_ = git.RemoveWorktree(repoDir, filepath.Base(wtDir))
		}
	}()

	isolatedFileName := "worktree_exclusive_artifact.txt"

	ctx := context.Background()
	_, err := mgr.Spawn(ctx, "isolate-mod", "Feature Writer", subagent.ProfileGeneral, "Write artifact", "", subagent.WorkspaceBranch,
		func(ctx context.Context, sub *subagent.SubagentInfo) (string, error) {
			filePath := filepath.Join(wtDir, isolatedFileName)
			return "written", os.WriteFile(filePath, []byte("exclusive content"), 0644)
		},
	)
	if err != nil {
		t.Fatalf("Spawn failed: %v", err)
	}

	snap := waitForSubagent(t, mgr, "isolate-mod", 10*time.Second)
	if snap.Status != "COMPLETED" {
		t.Fatalf("expected subagent to complete, got %s", snap.Status)
	}

	// File MUST NOT exist in parent repo
	parentFilePath := filepath.Join(repoDir, isolatedFileName)
	if _, err := os.Stat(parentFilePath); !os.IsNotExist(err) {
		t.Fatalf("isolated file leaked into parent repository directory!")
	}

	// File MUST exist in isolated worktree directory
	wtFilePath := filepath.Join(wtDir, isolatedFileName)
	if _, err := os.Stat(wtFilePath); err != nil {
		t.Fatalf("isolated file was not created in worktree directory: %v", err)
	}
}

func TestTier1_F17_SubagentEventBusNotification(t *testing.T) {
	eb := events.NewEventBus()
	mgr := subagent.NewManager(eb, t.TempDir())

	subChan, subID := eb.Subscribe("subagent_responses")
	defer eb.Unsubscribe("subagent_responses", subID)

	ctx := context.Background()
	_, err := mgr.Spawn(ctx, "event-test-1", "Notifier", subagent.ProfileGeneral, "Report event", "", subagent.WorkspaceInherit,
		func(ctx context.Context, sub *subagent.SubagentInfo) (string, error) {
			return "Mission Success", nil
		},
	)
	if err != nil {
		t.Fatalf("Spawn failed: %v", err)
	}

	select {
	case msg := <-subChan:
		if !strings.Contains(msg, "event-test-1") || !strings.Contains(msg, "COMPLETED") || !strings.Contains(msg, "Mission Success") {
			t.Fatalf("unexpected event message content: %s", msg)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("timeout waiting for subagent_responses event on EventBus")
	}
}

func TestTier1_F17_SubagentCancellationHandling(t *testing.T) {
	mgr := subagent.NewManager(nil, t.TempDir())
	ctx, cancel := context.WithCancel(context.Background())

	info, err := mgr.Spawn(ctx, "cancel-test", "Long Worker", subagent.ProfileGeneral, "Work forever", "", subagent.WorkspaceInherit,
		func(subCtx context.Context, sub *subagent.SubagentInfo) (string, error) {
			select {
			case <-subCtx.Done():
				return "", subCtx.Err()
			case <-time.After(5 * time.Second):
				return "Finished", nil
			}
		},
	)
	if err != nil {
		t.Fatalf("Spawn failed: %v", err)
	}

	// Trigger cancellation
	time.Sleep(50 * time.Millisecond)
	cancel()

	time.Sleep(200 * time.Millisecond)

	snap, _ := mgr.Get(info.ID)
	if snap.Status != "KILLED" {
		t.Fatalf("expected status to be KILLED, got: %s", snap.Status)
	}
	if snap.State != subagent.StateKilled {
		t.Fatalf("expected state to be StateKilled, got: %s", snap.State)
	}
}

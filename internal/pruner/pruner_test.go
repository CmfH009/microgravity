package pruner

import (
	"fmt"
	"strings"
	"testing"
)

func TestEstimateTokens(t *testing.T) {
	text := "Hello world, this is a test string for token estimation."
	tokens := EstimateTokens(text)
	if tokens < 5 || tokens > 20 {
		t.Errorf("Expected token count around 10-15, got %d", tokens)
	}
}

func TestPruneContext(t *testing.T) {
	// Create long conversation with multiple turns
	var sb strings.Builder
	for i := 1; i <= 50; i++ {
		sb.WriteString(strings.Repeat("Turn message content line ", 5))
		sb.WriteString("\n")
	}

	longConvo := sb.String()
	originalTokens := EstimateTokens(longConvo)

	// Set low max tokens
	maxTokens := 100
	pruned, removed := PruneContext(longConvo, maxTokens)

	if EstimateTokens(pruned) > maxTokens {
		t.Errorf("Pruned context tokens %d still exceeds max %d", EstimateTokens(pruned), maxTokens)
	}

	if removed == 0 {
		t.Errorf("Expected tokens to be pruned/removed")
	}

	if !strings.Contains(pruned, "[Earlier conversation context pruned for token efficiency]") {
		t.Errorf("Expected pruning notice in output")
	}

	_ = originalTokens
}

func TestPruneContextPinsTurn0(t *testing.T) {
	rootGoal := "User: Build a distributed raft consensus cluster in Go with automated leader election.\nAgent: I will begin designing the raft cluster architecture.\n"
	var sb strings.Builder
	sb.WriteString(rootGoal)
	for i := 1; i <= 40; i++ {
		sb.WriteString(fmt.Sprintf("User: Iteration check %d\nAgent: Executing tool call step %d\nTool Output: success\n", i, i))
	}

	longConvo := sb.String()
	pruned, removed := PruneContext(longConvo, 200)

	if removed == 0 {
		t.Errorf("Expected tokens to be pruned")
	}
	if !strings.Contains(pruned, "Build a distributed raft consensus cluster") {
		t.Errorf("Expected Turn 0 root user goal to be preserved in pruned context, but was dropped! Result: %s", pruned)
	}
	if !strings.Contains(pruned, "[Earlier conversation context pruned for token efficiency]") {
		t.Errorf("Expected pruning notice marker")
	}
}

func TestPruneContextSingleTurnAutonomousLoop(t *testing.T) {
	rootGoal := "User: Implement distributed raft cluster in Go with automated leader election and snapshot compaction.\nAgent: I will execute tools to implement this.\n"
	var sb strings.Builder
	sb.WriteString(rootGoal)
	// 50 tool outputs and intermediate agent steps, with NO second User: turn
	for i := 1; i <= 50; i++ {
		sb.WriteString(fmt.Sprintf("Tool Output:\nStep %d intermediate result output data line...\nAgent: Continuing with step %d execution.\n", i, i))
	}

	singleTurnConvo := sb.String()
	pruned, removed := PruneContext(singleTurnConvo, 250)

	if removed == 0 {
		t.Errorf("Expected tokens to be pruned from single-turn autonomous tool loop")
	}
	if !strings.Contains(pruned, "Implement distributed raft cluster in Go with automated leader election and snapshot compaction.") {
		t.Errorf("Expected Turn 0 root user prompt to be strictly preserved in single-turn tool loop, but was dropped! Result: %s", pruned)
	}
	if strings.Contains(pruned, "[Turn 0 truncated]") {
		t.Errorf("Turn 0 must never be truncated in single-turn tool loops")
	}
	if !strings.Contains(pruned, "[Earlier conversation context pruned for token efficiency]") {
		t.Errorf("Expected pruning notice marker")
	}
}

func TestPruneContextDirectivePreservation(t *testing.T) {
	rootGoal := "User: Refactor database schema and authentication subsystem.\nAgent: Starting migration analysis.\n"
	var sb strings.Builder
	sb.WriteString(rootGoal)

	for i := 1; i <= 20; i++ {
		sb.WriteString(fmt.Sprintf("Tool Output:\nLog data chunk %d\nAgent: Analyzing chunk %d\n", i, i))
	}

	// Inject directives and Letta blocks in the middle of conversation
	sb.WriteString("\n[System Directive]: Never modify production credentials in vault.\n")
	sb.WriteString("[Project Directive]: Enforce 100% test pass rate across all modules.\n")
	sb.WriteString("<human>\nUser prefers concise idiomatic Go without third-party frameworks.\n</human>\n")
	sb.WriteString("<persona>\nPrincipal distributed systems architect and security specialist.\n</persona>\n")
	sb.WriteString("<scratchpad>\nActive focus: ensure zero-downtime schema migrations.\n</scratchpad>\n")

	for i := 21; i <= 40; i++ {
		sb.WriteString(fmt.Sprintf("Tool Output:\nHeavy migration log output %d with lots of verbose debugging details\nAgent: Done with step %d\n", i, i))
	}

	convo := sb.String()
	pruned, removed := PruneContext(convo, 350)

	if removed == 0 {
		t.Errorf("Expected tokens to be pruned")
	}
	if !strings.Contains(pruned, "Refactor database schema and authentication subsystem.") {
		t.Errorf("Expected Turn 0 root user prompt to be preserved")
	}
	if !strings.Contains(pruned, "[System Directive]: Never modify production credentials in vault.") {
		t.Errorf("Expected [System Directive] to be preserved verbatim under token pressure")
	}
	if !strings.Contains(pruned, "[Project Directive]: Enforce 100% test pass rate across all modules.") {
		t.Errorf("Expected [Project Directive] to be preserved verbatim under token pressure")
	}
	if !strings.Contains(pruned, "<human>\nUser prefers concise idiomatic Go without third-party frameworks.\n</human>") {
		t.Errorf("Expected <human> Letta block to be preserved verbatim")
	}
	if !strings.Contains(pruned, "<persona>\nPrincipal distributed systems architect and security specialist.\n</persona>") {
		t.Errorf("Expected <persona> Letta block to be preserved verbatim")
	}
	if !strings.Contains(pruned, "<scratchpad>\nActive focus: ensure zero-downtime schema migrations.\n</scratchpad>") {
		t.Errorf("Expected <scratchpad> Letta block to be preserved verbatim")
	}
}

func TestPruneContextNestedCoreMemory(t *testing.T) {
	rootGoal := "User: Implement memory subsystem.\nAgent: Initializing...\n"
	var sb strings.Builder
	sb.WriteString(rootGoal)
	for i := 1; i <= 20; i++ {
		sb.WriteString(fmt.Sprintf("Tool Output:\nStep %d intermediate debug output...\nAgent: Step %d processed.\n", i, i))
	}

	coreMemoryBlock := `<core_memory>
<human>
User is a staff engineer specializing in distributed systems and Go.
</human>
<persona>
Microgravity core AI assistant with autonomous tool capability.
</persona>
<scratchpad>
Migrating to structured []Message protocol and native function calling.
</scratchpad>
</core_memory>
`
	sb.WriteString(coreMemoryBlock)

	for i := 21; i <= 40; i++ {
		sb.WriteString(fmt.Sprintf("Tool Output:\nStep %d verbose logs...\nAgent: Finished step %d.\n", i, i))
	}

	convo := sb.String()
	pruned, removed := PruneContext(convo, 300)

	if removed == 0 {
		t.Errorf("Expected tokens to be pruned")
	}
	if !strings.Contains(pruned, "<core_memory>") {
		t.Errorf("Expected <core_memory> opening tag to be preserved")
	}
	if !strings.Contains(pruned, "</core_memory>") {
		t.Errorf("Expected </core_memory> closing tag to be preserved, not prematurely cut off by child tags")
	}
	if !strings.Contains(pruned, "<human>") || !strings.Contains(pruned, "</human>") {
		t.Errorf("Expected nested <human> block to be preserved")
	}
	if !strings.Contains(pruned, "<persona>") || !strings.Contains(pruned, "</persona>") {
		t.Errorf("Expected nested <persona> block to be preserved")
	}
	if !strings.Contains(pruned, "<scratchpad>") || !strings.Contains(pruned, "</scratchpad>") {
		t.Errorf("Expected nested <scratchpad> block to be preserved")
	}
}

func TestPruneMessages(t *testing.T) {
	msgs := []Message{
		{Role: "user", Content: "Root user mission: refactor memory architecture"},
		{Role: "assistant", Content: "Understood, beginning task execution"},
	}
	for i := 1; i <= 30; i++ {
		msgs = append(msgs, Message{
			Role:    "tool",
			Name:    "bash",
			Content: fmt.Sprintf("Command execution output chunk %d with extensive output data...", i),
		})
	}
	// Add core_memory directive
	msgs = append(msgs, Message{
		Role: "system",
		Content: `<core_memory>
<human>User is a distributed systems architect</human>
</core_memory>`,
	})
	for i := 31; i <= 50; i++ {
		msgs = append(msgs, Message{
			Role:    "tool",
			Name:    "bash",
			Content: fmt.Sprintf("Recent command execution output chunk %d...", i),
		})
	}

	prunedMsgs, removed := PruneMessages(msgs, 200)
	if removed == 0 {
		t.Errorf("Expected tokens to be pruned from message slice")
	}
	if len(prunedMsgs) >= len(msgs) {
		t.Errorf("Expected pruned message count (%d) to be less than original (%d)", len(prunedMsgs), len(msgs))
	}

	// Verify Turn 0 is preserved
	if len(prunedMsgs) == 0 || prunedMsgs[0].Content != "Root user mission: refactor memory architecture" {
		t.Errorf("Expected Turn 0 user message to be preserved at index 0")
	}

	// Verify directive is preserved
	hasCoreMemory := false
	hasMarker := false
	for _, m := range prunedMsgs {
		if strings.Contains(m.Content, "<core_memory>") {
			hasCoreMemory = true
		}
		if strings.Contains(m.Content, "[Earlier conversation context pruned for token efficiency]") {
			hasMarker = true
		}
	}
	if !hasCoreMemory {
		t.Errorf("Expected <core_memory> directive message to be preserved in pruned messages")
	}
	if !hasMarker {
		t.Errorf("Expected pruner notice marker message to be present")
	}
}


func TestPruneContextTurn0NoTruncation(t *testing.T) {
	// Turn 0 is long (~150 tokens)
	longTurn0 := "User: " + strings.Repeat("Detailed task requirement specification with rigorous constraints. ", 15) + "\nAgent: Acknowledged.\n"
	var sb strings.Builder
	sb.WriteString(longTurn0)
	for i := 1; i <= 30; i++ {
		sb.WriteString(fmt.Sprintf("Tool Output: Output %d with more data\nAgent: Step %d complete\n", i, i))
	}

	convo := sb.String()
	// Set maxTokens to 200, so Turn 0 takes up > 70% of maxTokens
	pruned, removed := PruneContext(convo, 200)

	if removed == 0 {
		t.Errorf("Expected tokens to be pruned")
	}
	if strings.Contains(pruned, "[Turn 0 truncated]") {
		t.Errorf("Turn 0 must never be truncated even when taking a large fraction of token budget")
	}
	if !strings.Contains(pruned, "Detailed task requirement specification with rigorous constraints.") {
		t.Errorf("Expected entire Turn 0 specification to be preserved")
	}
}

func TestMaxTokensForModelScaling(t *testing.T) {
	tests := []struct {
		model    string
		expected int
	}{
		{"gemini-1.5-pro", 256000},
		{"gemini-2.5-pro", 256000},
		{"gemini-3.1-pro", 256000},
		{"gemini-2.5-flash", 128000},
		{"gemini-3.8-flash", 128000},
		{"llama-3.3-70b", 64000},
		{"qwen2.5-coder:32b", 32000},
		{"gemma4:e2b", 8192},
		{"orbit-coder", 8192},
		{"codegemma:7b", 8192},
		{"unknown-custom-model", 16000},
	}

	for _, tt := range tests {
		got := MaxTokensForModel(tt.model)
		if got != tt.expected {
			t.Errorf("MaxTokensForModel(%q) = %d, expected %d", tt.model, got, tt.expected)
		}
	}
}

func BenchmarkEstimateTokens(b *testing.B) {
	text := strings.Repeat("Microgravity autonomous AI agent context line. ", 100)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = EstimateTokens(text)
	}
}

func BenchmarkPruneContext(b *testing.B) {
	text := strings.Repeat("Long turn conversation line for pruning test. ", 500)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = PruneContext(text, 500)
	}
}

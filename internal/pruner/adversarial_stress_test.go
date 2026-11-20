package pruner

import (
	"fmt"
	"strings"
	"testing"
)

// TestAdversarial_SingleTurnAutonomousLoop verifies that in a single-turn autonomous tool loop
// with 30 tool iterations (Tool Output: ...) and NO second user message, Turn 0 is preserved
// 100% in full without truncation.
func TestAdversarial_SingleTurnAutonomousLoop(t *testing.T) {
	rootPrompt := "User: Refactor the microgravity consensus engine to implement Raft snapshot compaction with zero data loss under Byzantine faults."
	initialAgentThought := "Agent: Analyzing consensus state machine and setting up test harnesses."

	var sb strings.Builder
	sb.WriteString(rootPrompt)
	sb.WriteString("\n")
	sb.WriteString(initialAgentThought)
	sb.WriteString("\n")

	// Generate exactly 30 distinct tool iterations with substantial output
	for i := 1; i <= 30; i++ {
		sb.WriteString(fmt.Sprintf("Tool Output:\n[Iteration %02d]: Ran test_raft_cluster_partition -- nodes=5 partition_size=2 status=PASS logs_bytes=%d payload=%s\n",
			i, i*1024, strings.Repeat("raft_entry_chunk_data ", 10)))
		sb.WriteString(fmt.Sprintf("Agent: Completed step %d. Proceeding to snapshot generation for iteration %d.\n", i, i+1))
	}

	convo := sb.String()
	initialTokens := EstimateTokens(convo)
	if initialTokens < 1500 {
		t.Fatalf("Expected large conversation (>1500 tokens), got %d", initialTokens)
	}

	// Prune under aggressive budgets: 800 tokens, 500 tokens, 300 tokens
	budgets := []int{800, 500, 300}
	for _, maxTokens := range budgets {
		t.Run(fmt.Sprintf("Budget_%d_Tokens", maxTokens), func(t *testing.T) {
			pruned, removed := PruneContext(convo, maxTokens)

			if removed == 0 {
				t.Fatalf("Expected token removal under budget %d", maxTokens)
			}

			// Invariant 1: Turn 0 root prompt must be preserved 100% in full
			expectedPrompt := "Refactor the microgravity consensus engine to implement Raft snapshot compaction with zero data loss under Byzantine faults."
			if !strings.Contains(pruned, expectedPrompt) {
				t.Errorf("VIOLATION: Turn 0 root prompt was dropped or modified! Pruned text:\n%s", pruned)
			}

			// Invariant 2: No truncation marker on Turn 0
			if strings.Contains(pruned, "[Turn 0 truncated]") {
				t.Errorf("VIOLATION: Turn 0 was truncated with [Turn 0 truncated] marker!")
			}

			// Invariant 3: Pruning notice must be present when pruning occurred
			if !strings.Contains(pruned, "[Earlier conversation context pruned for token efficiency]") {
				t.Errorf("Expected pruning notice marker in pruned context")
			}

			// Invariant 4: Recent body should contain the latest iterations (e.g. Iteration 30)
			if !strings.Contains(pruned, "[Iteration 30]") {
				t.Errorf("Expected latest tool iteration (Iteration 30) to be preserved in recent body")
			}
		})
	}
}

// TestAdversarial_DirectivesAndLettaPreservation verifies that under extreme token pruning,
// all directives ([System Directive], [Project Directive], and Letta blocks) are preserved
// 100% in full verbatim.
func TestAdversarial_DirectivesAndLettaPreservation(t *testing.T) {
	rootPrompt := "User: Implement persistent agent memory subsystem with Letta blocks and automated schema migrations.\nAgent: Starting memory architecture inspection.\n"

	var sb strings.Builder
	sb.WriteString(rootPrompt)

	// Interleave 15 heavy tool iterations before directives
	for i := 1; i <= 15; i++ {
		sb.WriteString(fmt.Sprintf("Tool Output:\n[Phase 1 Iteration %d]: scanning sqlite memory tables... found %d records payload=%s\nAgent: Step %d processed.\n",
			i, i*50, strings.Repeat("verbose_database_dump_row_data ", 15), i))
	}

	// Directives and Letta memory blocks
	systemDirective := "[System Directive]: Production vault encryption keys must never be logged or echoed in plaintext."
	projectDirective := "[Project Directive]: Enforce strict SQLite WAL mode with zero database lock timeouts."
	humanBlock := "<human>\nUser is a principal systems architect preferring concise, idiomatic Go with zero external telemetry.\n</human>"
	personaBlock := "<persona>\nAutonomous systems specialist focused on fault-tolerant distributed protocols.\n</persona>"
	scratchpadBlock := "<scratchpad>\nCurrent goal: verify invariant preservation in context pruner under extreme compression.\n</scratchpad>"
	coreMemoryBlock := "<core_memory>\nActive project: Microgravity (mg) CLI engine.\nWorkspace: A:\\projects\\microgravity\n</core_memory>"

	sb.WriteString("\n" + systemDirective + "\n")
	sb.WriteString(projectDirective + "\n")
	sb.WriteString(humanBlock + "\n")
	sb.WriteString(personaBlock + "\n")
	sb.WriteString(scratchpadBlock + "\n")
	sb.WriteString(coreMemoryBlock + "\n\n")

	// Interleave 15 more heavy tool iterations after directives
	for i := 16; i <= 30; i++ {
		sb.WriteString(fmt.Sprintf("Tool Output:\n[Phase 2 Iteration %d]: running benchmark suite on WAL mode commit rate: %d tx/sec payload=%s\nAgent: Iteration %d recorded.\n",
			i, i*250, strings.Repeat("benchmark_telemetry_metrics_record ", 15), i))
	}

	convo := sb.String()
	totalTokens := EstimateTokens(convo)
	if totalTokens < 2000 {
		t.Fatalf("Expected conversation to exceed 2000 tokens, got %d", totalTokens)
	}

	// Extreme pruning budgets: 500 tokens, 350 tokens, 250 tokens
	budgets := []int{500, 350, 250}
	for _, maxTokens := range budgets {
		t.Run(fmt.Sprintf("Budget_%d_Extreme", maxTokens), func(t *testing.T) {
			pruned, removed := PruneContext(convo, maxTokens)

			if removed == 0 {
				t.Fatalf("Expected token removal under extreme budget %d", maxTokens)
			}

			// Invariant 1: Turn 0 root prompt preserved
			if !strings.Contains(pruned, "Implement persistent agent memory subsystem with Letta blocks") {
				t.Errorf("VIOLATION: Turn 0 was dropped in extreme pruning %d! Pruned:\n%s", maxTokens, pruned)
			}

			// Invariant 2: [System Directive] preserved verbatim
			if !strings.Contains(pruned, systemDirective) {
				t.Errorf("VIOLATION: [System Directive] was dropped or mutated! Pruned:\n%s", pruned)
			}

			// Invariant 3: [Project Directive] preserved verbatim
			if !strings.Contains(pruned, projectDirective) {
				t.Errorf("VIOLATION: [Project Directive] was dropped or mutated! Pruned:\n%s", pruned)
			}

			// Invariant 4: <human> Letta block preserved verbatim
			if !strings.Contains(pruned, humanBlock) {
				t.Errorf("VIOLATION: <human> block was dropped or mutated! Pruned:\n%s", pruned)
			}

			// Invariant 5: <persona> Letta block preserved verbatim
			if !strings.Contains(pruned, personaBlock) {
				t.Errorf("VIOLATION: <persona> block was dropped or mutated! Pruned:\n%s", pruned)
			}

			// Invariant 6: <scratchpad> Letta block preserved verbatim
			if !strings.Contains(pruned, scratchpadBlock) {
				t.Errorf("VIOLATION: <scratchpad> block was dropped or mutated! Pruned:\n%s", pruned)
			}

			// Invariant 7: <core_memory> Letta block preserved verbatim
			if !strings.Contains(pruned, coreMemoryBlock) {
				t.Errorf("VIOLATION: <core_memory> block was dropped or mutated! Pruned:\n%s", pruned)
			}
		})
	}
}

// TestAdversarial_DirectivesOrderingAndPosition verifies that directives are preserved
// regardless of whether they appear at the beginning, middle, or end of remaining conversation.
func TestAdversarial_DirectivesOrderingAndPosition(t *testing.T) {
	rootPrompt := "User: Master objective prompt.\n"
	directive1 := "[System Directive]: First directive."
	directive2 := "<scratchpad>\nIntermediate scratchpad note.\n</scratchpad>"
	directive3 := "[Project Directive]: Final directive."

	var sb strings.Builder
	sb.WriteString(rootPrompt)
	sb.WriteString(directive1 + "\n")
	for i := 1; i <= 20; i++ {
		sb.WriteString(fmt.Sprintf("Tool Output: step %d output with padding data...\n", i))
	}
	sb.WriteString(directive2 + "\n")
	for i := 21; i <= 40; i++ {
		sb.WriteString(fmt.Sprintf("Tool Output: step %d output with padding data...\n", i))
	}
	sb.WriteString(directive3 + "\n")

	convo := sb.String()
	pruned, _ := PruneContext(convo, 250)

	if !strings.Contains(pruned, directive1) {
		t.Errorf("Expected directive1 to be preserved")
	}
	if !strings.Contains(pruned, directive2) {
		t.Errorf("Expected directive2 to be preserved")
	}
	if !strings.Contains(pruned, directive3) {
		t.Errorf("Expected directive3 to be preserved")
	}
}

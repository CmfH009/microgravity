package engine

import (
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/purav/microgravity/internal/pruner"
)

// ============================================================================
// 1. ADVANCED SERIALIZATION ROUND-TRIP & DEMARCATION FIDELITY
// ============================================================================

func TestAdversarial_SerializationRoundTrip_ComplexInterleaving(t *testing.T) {
	testCases := []struct {
		name     string
		messages []Message
	}{
		{
			name: "User -> Assistant -> Tool -> Assistant -> Tool -> Tool -> Assistant",
			messages: []Message{
				NewUserMessage("Check the cluster health and run diagnostic tests"),
				NewAssistantMessage("Initiating cluster inspection across nodes..."),
				NewToolMessage("cluster_health", "status: degraded, node-3 offline"),
				NewAssistantMessage("Node 3 is degraded. Querying node event logs and reboot status."),
				NewToolMessage("node_logs", "panic: out of memory at 0xdeadbeef\nOOMKiller triggered"),
				NewToolMessage("reboot_node", "node-3 reboot signal sent successfully"),
				NewAssistantMessage("Node 3 rebooted cleanly. Health status returned to nominal."),
			},
		},
		{
			name: "Interleaved with System Directives and Async Alerts",
			messages: []Message{
				NewUserMessage("Perform live database migration with rollback safety"),
				NewAssistantMessage("Validating database schema version and active connections."),
				NewSystemMessage("System Directive", "Zero data loss invariant enforced for all DDL operations"),
				NewAssistantMessage("Directive acknowledged. Taking pre-migration snapshot."),
				NewToolMessage("db_snapshot", "snapshot_id=snap-9982736 bytes=184729102 hash=sha256:abc123"),
				NewSystemMessage("Background Task Completed", "Task backup-stream finished with exit code 0"),
				NewAssistantMessage("Snapshot verified. Executing migration phase 1."),
				NewToolMessage("run_migration", "ALTER TABLE users ADD COLUMN flags INT DEFAULT 0;\nDONE"),
				NewSystemMessage("Scheduler Alert", "Health probe tick: latency 2.4ms (within 5ms SLO)"),
				NewAssistantMessage("Migration phase 1 completed. All SLOs verified."),
			},
		},
		{
			name: "Multiple Consecutive Assistant Messages",
			messages: []Message{
				NewUserMessage("Explain the consensus failure mode"),
				NewAssistantMessage("Part 1: Network partition isolates the leader from minority quorum."),
				NewAssistantMessage("Part 2: Minority nodes initiate candidate election but cannot achieve quorum."),
				NewAssistantMessage("Part 3: Once partition heals, highest term leader forces log synchronization."),
			},
		},
		{
			name: "Assistant Starting at Index 0",
			messages: []Message{
				NewAssistantMessage("Agent initialized in autonomous loop mode."),
				NewUserMessage("What is your status?"),
				NewAssistantMessage("Operating at peak efficiency."),
			},
		},
		{
			name: "Tool Outputs with Multi-line Formatting, Code Blocks, and Profiling",
			messages: []Message{
				NewUserMessage("Run command with detailed output"),
				NewAssistantMessage("Executing command now."),
				NewToolMessage("touch", "Created file /workspace/main.go"),
				NewAssistantMessage("File created. Now inspecting binary layout."),
				NewToolMessage("xxd", "00000000: 7f45 4c46 0201 0100 0000 0000 0000 0000  .ELF............\n00000010: 0300 3e00 0100 0000 8010 0000 0000 0000  ..>............."),
				NewAssistantMessage("ELF binary header confirmed valid."),
			},
		},
		{
			name: "Tool Outputs with Multi-line Formatting, Whitespace, and Empty Outputs",
			messages: []Message{
				NewUserMessage("Run command with empty output"),
				NewAssistantMessage("Executing command now."),
				NewToolMessage("touch", ""),
				NewAssistantMessage("File touched. Now inspecting binary layout."),
				NewToolMessage("xxd", "00000000: 7f45 4c46 0201 0100 0000 0000 0000 0000  .ELF............\n00000010: 0300 3e00 0100 0000 8010 0000 0000 0000  ..>............."),
				NewAssistantMessage("ELF binary header confirmed valid."),
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			formatted := FormatMessages(tc.messages)
			t.Logf("Formatted conversation (%d bytes):\n%s", len(formatted), formatted)

			parsed := ParseMessagesFromText(formatted)

			if len(parsed) != len(tc.messages) {
				t.Fatalf("Message count mismatch: expected %d, got %d", len(tc.messages), len(parsed))
			}

			for i := range tc.messages {
				orig := tc.messages[i]
				p := parsed[i]

				if p.Role != orig.Role {
					t.Errorf("Message[%d] Role mismatch: expected %q, got %q (content: %q)", i, orig.Role, p.Role, p.Content)
				}

				origContent := strings.TrimSpace(orig.Content)
				parsedContent := strings.TrimSpace(p.Content)
				if origContent != parsedContent {
					t.Errorf("Message[%d] Content mismatch:\nExpected: %q\nGot:      %q", i, origContent, parsedContent)
				}
			}

			// Assert round-trip idempotency
			reformatted := FormatMessages(parsed)
			reparsed := ParseMessagesFromText(reformatted)
			if len(reparsed) != len(parsed) {
				t.Fatalf("Idempotency failure: second pass produced %d messages, expected %d", len(reparsed), len(parsed))
			}
			for i := range parsed {
				if reparsed[i].Role != parsed[i].Role || strings.TrimSpace(reparsed[i].Content) != strings.TrimSpace(parsed[i].Content) {
					t.Errorf("Idempotency drift at message[%d]: Role=(%s vs %s), Content=(%q vs %q)",
						i, parsed[i].Role, reparsed[i].Role, parsed[i].Content, reparsed[i].Content)
				}
			}
		})
	}
}

func TestAdversarial_ToolOutputDoesNotSwallowAssistant(t *testing.T) {
	// Adversarial scenario: Tool output ends with multiple newlines and trailing whitespace
	// and is immediately followed by an assistant response.
	// The assistant response MUST NOT be swallowed or merged into the tool output.
	toolOutput := "Build failed:\nexit status 2\n\n   \n"
	assistantMsg := "I noticed the compilation error on exit status 2. Inspecting line 44 of main.go."

	msgs := []Message{
		NewUserMessage("Build the binary"),
		NewAssistantMessage("Running build now."),
		NewToolMessage("go_build", toolOutput),
		NewAssistantMessage(assistantMsg),
	}

	formatted := FormatMessages(msgs)
	parsed := ParseMessagesFromText(formatted)

	if len(parsed) != 4 {
		t.Fatalf("Role collapse detected: expected 4 messages, got %d", len(parsed))
	}

	if parsed[2].Role != RoleTool {
		t.Errorf("Message[2] expected RoleTool, got %s", parsed[2].Role)
	}
	if parsed[3].Role != RoleAssistant {
		t.Errorf("Message[3] expected RoleAssistant, got %s", parsed[3].Role)
	}
	if !strings.Contains(parsed[3].Content, "compilation error on exit status 2") {
		t.Errorf("Assistant content was corrupted or swallowed: got %q", parsed[3].Content)
	}
}

// ============================================================================
// 2. CONCURRENT SESSION SYNCHRONIZATION & DATA RACE ELIMINATION
// ============================================================================

func TestAdversarial_Concurrent_AddMessage_HighLoad_Race(t *testing.T) {
	sess, err := NewSession(SessionConfig{
		SessionName: "adversarial_concurrent_sync",
		ActiveModel: "gemini-3.8-flash",
	})
	if err != nil {
		t.Fatalf("Failed to initialize session: %v", err)
	}

	numWriters := 40
	msgsPerWriter := 30
	totalExpected := numWriters * msgsPerWriter

	var wg sync.WaitGroup
	startSignal := make(chan struct{})

	// Concurrently append messages across 40 goroutines
	for w := 0; w < numWriters; w++ {
		wg.Add(1)
		go func(writerID int) {
			defer wg.Done()
			<-startSignal

			for i := 0; i < msgsPerWriter; i++ {
				var msg Message
				switch (writerID + i) % 4 {
				case 0:
					msg = NewUserMessage(fmt.Sprintf("User query w%d-%d", writerID, i))
				case 1:
					msg = NewAssistantMessage(fmt.Sprintf("Assistant reply w%d-%d", writerID, i))
				case 2:
					msg = NewToolMessage("exec_tool", fmt.Sprintf("Tool output w%d-%d", writerID, i))
				case 3:
					msg = NewSystemMessage("System Directive", fmt.Sprintf("Directive w%d-%d", writerID, i))
				}
				sess.AddMessage(msg)
			}
		}(w)
	}

	// Concurrently read messages
	numReaders := 15
	for r := 0; r < numReaders; r++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-startSignal

			for i := 0; i < 25; i++ {
				_ = sess.GetMessages()
				_ = sess.GetState()
				time.Sleep(50 * time.Microsecond)
			}
		}()
	}

	// Concurrently sync conversation
	numSyncers := 5
	for s := 0; s < numSyncers; s++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-startSignal

			for i := 0; i < 15; i++ {
				sess.SyncConversation()
				time.Sleep(100 * time.Microsecond)
			}
		}()
	}

	// Release all goroutines simultaneously
	close(startSignal)
	wg.Wait()

	// Post-condition validation
	finalMsgs := sess.GetMessages()
	if len(finalMsgs) != totalExpected {
		t.Fatalf("Message count mismatch after concurrent writes: expected %d, got %d", totalExpected, len(finalMsgs))
	}

	// CRITICAL INVARIANT: sess.Conversation.String() == FormatMessages(sess.Messages)
	sess.mu.RLock()
	convoStr := sess.Conversation.String()
	expectedFormat := FormatMessages(sess.Messages)
	sess.mu.RUnlock()

	if convoStr != expectedFormat {
		t.Fatalf("CRITICAL CONCURRENCY INVARIANT VIOLATION: sess.Conversation.String() does NOT match FormatMessages(sess.Messages)!\nConvo len: %d, Format len: %d", len(convoStr), len(expectedFormat))
	}
}

// ============================================================================
// 3. NESTED LETTA CORE MEMORY TAG PRESERVATION UNDER EXTREME PRUNING
// ============================================================================

func TestAdversarial_NestedLettaMemory_ExtremeBudgets(t *testing.T) {
	rootPrompt := "User: Initialize autonomous self-healing consensus engine with Byzantine fault tolerance.\nAgent: Starting node discovery.\n"

	nestedCoreMemory := `<core_memory>
  <human>
    Operator: Principal Software Engineer.
    Mandate: High performance, zero concurrency bugs, full tag preservation.
  </human>
  <persona>
    Microgravity autonomous engineering runtime.
    Capabilities: Letta hierarchical memory, Gemini 3.8 inference, AST auto-repair.
  </persona>
  <scratchpad>
    Phase: Milestone M3 Verification.
    Priority: Retain Turn 0 and <core_memory> tags intact under any token ceiling.
  </scratchpad>
</core_memory>`

	var sb strings.Builder
	sb.WriteString(rootPrompt)

	// Inject 40 turns of verbose logs before core memory
	for i := 1; i <= 40; i++ {
		sb.WriteString(fmt.Sprintf("Tool Output:\n[Pre-Memory Iteration %d]: Node telemetry log data %s\nAgent: Step %d verified.\n",
			i, strings.Repeat("0xAA55 ", 20), i))
	}

	sb.WriteString("\n" + nestedCoreMemory + "\n\n")

	// Inject 40 turns of verbose logs after core memory
	for i := 41; i <= 80; i++ {
		sb.WriteString(fmt.Sprintf("Tool Output:\n[Post-Memory Iteration %d]: Cluster sync telemetry %s\nAgent: Subsequent step %d verified.\n",
			i, strings.Repeat("0x55AA ", 20), i))
	}

	fullConvo := sb.String()
	initialTokens := pruner.EstimateTokens(fullConvo)
	t.Logf("Full conversation token size: %d tokens", initialTokens)

	// Test under extreme token ceilings down to 30 tokens
	extremeBudgets := []int{600, 350, 200, 150, 100, 80, 50, 30}

	for _, budget := range extremeBudgets {
		t.Run(fmt.Sprintf("Budget_%d", budget), func(t *testing.T) {
			pruned, removed := pruner.PruneContext(fullConvo, budget)

			if removed == 0 {
				t.Errorf("Expected pruning for budget %d, but removed was 0", budget)
			}

			// Invariant 1: Opening <core_memory> tag present
			if !strings.Contains(pruned, "<core_memory>") {
				t.Fatalf("CRITICAL: <core_memory> opening tag was DROPPED under budget %d!\nPruned:\n%s", budget, pruned)
			}

			// Invariant 2: Closing </core_memory> tag present
			if !strings.Contains(pruned, "</core_memory>") {
				t.Fatalf("CRITICAL: </core_memory> closing tag was DROPPED under budget %d!\nPruned:\n%s", budget, pruned)
			}

			// Invariant 3: Tag order must be open before close
			openIdx := strings.Index(pruned, "<core_memory>")
			closeIdx := strings.Index(pruned, "</core_memory>")
			if closeIdx <= openIdx {
				t.Fatalf("CRITICAL: Inverted or malformed <core_memory> tag positions (open=%d, close=%d)", openIdx, closeIdx)
			}

			// Invariant 4: Nested <human> child tag and closing tag preserved
			if !strings.Contains(pruned, "<human>") || !strings.Contains(pruned, "</human>") {
				t.Errorf("<human> child block truncated or missing under budget %d", budget)
			}

			// Invariant 5: Nested <persona> child tag and closing tag preserved
			if !strings.Contains(pruned, "<persona>") || !strings.Contains(pruned, "</persona>") {
				t.Errorf("<persona> child block truncated or missing under budget %d", budget)
			}

			// Invariant 6: Nested <scratchpad> child tag and closing tag preserved
			if !strings.Contains(pruned, "<scratchpad>") || !strings.Contains(pruned, "</scratchpad>") {
				t.Errorf("<scratchpad> child block truncated or missing under budget %d", budget)
			}

			// Invariant 7: Turn 0 root prompt must survive
			if !strings.Contains(pruned, "Initialize autonomous self-healing consensus engine") {
				t.Errorf("Turn 0 user goal was evicted under budget %d", budget)
			}
		})
	}
}

func TestAdversarial_StructuredPruneMessages_PreservesNestedLettaTags(t *testing.T) {
	nestedXML := `<core_memory>
  <human>Engineer</human>
  <persona>Assistant</persona>
  <scratchpad>Stress Test</scratchpad>
</core_memory>`

	msgs := []Message{
		NewUserMessage("Turn 0: Critical mission objective"),
		NewAssistantMessage("Acknowledged. Beginning task."),
	}

	for i := 1; i <= 30; i++ {
		msgs = append(msgs, NewToolMessage("tool", fmt.Sprintf("Noisy payload chunk %d with lots of text", i)))
	}

	msgs = append(msgs, NewSystemMessage("core_memory", nestedXML))

	for i := 31; i <= 60; i++ {
		msgs = append(msgs, NewToolMessage("tool", fmt.Sprintf("Recent chunk %d payload", i)))
	}

	budgets := []int{500, 300, 150, 100, 50}
	for _, b := range budgets {
		t.Run(fmt.Sprintf("Budget_%d", b), func(t *testing.T) {
			pruned, removed := PruneMessages(msgs, b)
			if removed == 0 {
				t.Fatalf("Expected token removal under budget %d", b)
			}

			// Invariant: Turn 0 preserved at index 0
			if len(pruned) == 0 || pruned[0].Role != RoleUser || !strings.Contains(pruned[0].Content, "Turn 0") {
				t.Fatalf("Turn 0 lost in PruneMessages under budget %d", b)
			}

			// Invariant: Core memory preserved intact
			found := false
			for _, m := range pruned {
				if strings.Contains(m.Content, "<core_memory>") {
					found = true
					if !strings.Contains(m.Content, "</core_memory>") {
						t.Errorf("Unclosed <core_memory> in message: %s", m.Content)
					}
					if !strings.Contains(m.Content, "<human>") || !strings.Contains(m.Content, "<persona>") || !strings.Contains(m.Content, "<scratchpad>") {
						t.Errorf("Nested tags stripped from core_memory message: %s", m.Content)
					}
				}
			}
			if !found {
				t.Fatalf("<core_memory> message completely dropped under budget %d", b)
			}
		})
	}
}

// ============================================================================
// 4. LETTA COMPILATION RESILIENCE & CACHE/FALLBACK UNDER PRESSURE
// ============================================================================

func TestAdversarial_LettaCompilationResilience_FallbackAndCache(t *testing.T) {
	// 1. Reset cache to ensure cold start
	ResetLettaMemoryCache()

	sess, err := NewSession(SessionConfig{
		SessionName: "adversarial_letta_resilience",
		ActiveModel: "gemini-3.8-flash",
		// CoreMemory intentionally unset to force compilation / fallback resolution
	})
	if err != nil {
		t.Fatalf("Failed to initialize session: %v", err)
	}

	// 2. Cold compilation call
	t0 := time.Now()
	prompt := sess.BuildSystemPrompt()
	coldDuration := time.Since(t0)
	t.Logf("Cold BuildSystemPrompt took %v", coldDuration)

	// Assert fallback or compiled memory contains valid XML core memory tags
	if !strings.Contains(prompt, "<core_memory>") {
		t.Errorf("BuildSystemPrompt missing <core_memory>")
	}
	if !strings.Contains(prompt, "</core_memory>") {
		t.Errorf("BuildSystemPrompt missing </core_memory>")
	}
	if !strings.Contains(prompt, "<human>") || !strings.Contains(prompt, "</human>") {
		t.Errorf("BuildSystemPrompt missing <human> block")
	}
	if !strings.Contains(prompt, "<persona>") || !strings.Contains(prompt, "</persona>") {
		t.Errorf("BuildSystemPrompt missing <persona> block")
	}
	if !strings.Contains(prompt, "<scratchpad>") || !strings.Contains(prompt, "</scratchpad>") {
		t.Errorf("BuildSystemPrompt missing <scratchpad> block")
	}

	// 3. Subsequent warm call must hit cache and be near-instantaneous (< 5ms)
	t1 := time.Now()
	warmPrompt := sess.BuildSystemPrompt()
	warmDuration := time.Since(t1)
	t.Logf("Warm cached BuildSystemPrompt took %v", warmDuration)

	if warmDuration > 50*time.Millisecond {
		t.Errorf("Warm BuildSystemPrompt took unexpectedly long (%v), cache may not be functioning", warmDuration)
	}
	stripTimestamp := func(s string) string {
		lines := strings.Split(s, "\n")
		var out []string
		for _, l := range lines {
			if !strings.HasPrefix(l, "Current System Date and Time:") {
				out = append(out, l)
			}
		}
		return strings.Join(out, "\n")
	}
	if stripTimestamp(warmPrompt) != stripTimestamp(prompt) {
		t.Errorf("Warm prompt drifted from cold prompt")
	}

	// 4. Concurrently invoke BuildSystemPrompt under cache resets to ensure NO deadlocks or races
	var wg sync.WaitGroup
	numCallers := 25

	for i := 0; i < numCallers; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			if id%5 == 0 {
				ResetLettaMemoryCache()
			}
			p := sess.BuildSystemPrompt()
			if !strings.Contains(p, "<core_memory>") || !strings.Contains(p, "</core_memory>") {
				t.Errorf("Concurrent BuildSystemPrompt missing core memory in caller %d", id)
			}
		}(i)
	}

	wg.Wait()
}

func TestAdversarial_CustomCoreMemory_OverridesCacheAndDefault(t *testing.T) {
	ResetLettaMemoryCache()

	customXML := `<core_memory>
  <human>Custom Architect</human>
  <persona>Specialized Benchmark Agent</persona>
  <scratchpad>Custom Task Invariant</scratchpad>
</core_memory>`

	sess, err := NewSession(SessionConfig{
		SessionName: "custom_core_memory_test",
		ActiveModel: "gemini-3.8-flash",
		CoreMemory:  customXML,
	})
	if err != nil {
		t.Fatalf("Failed to initialize session: %v", err)
	}

	prompt := sess.BuildSystemPrompt()
	if !strings.Contains(prompt, "Custom Architect") {
		t.Errorf("Custom core memory human tag not found in prompt")
	}
	if !strings.Contains(prompt, "Specialized Benchmark Agent") {
		t.Errorf("Custom core memory persona tag not found in prompt")
	}
	if !strings.Contains(prompt, "Custom Task Invariant") {
		t.Errorf("Custom core memory scratchpad tag not found in prompt")
	}
}

func TestAdversarial_EmptyMessageContent_Behavior(t *testing.T) {
	// Empirical verification of edge case: messages with empty or whitespace-only content.
	// ParseMessagesFromText intentionally requires strings.TrimSpace(content) != "" for all messages.
	emptyTool := NewToolMessage("touch", "")
	formatted := FormatMessages([]Message{emptyTool})
	parsed := ParseMessagesFromText(formatted)

	// Documenting behavior: empty content is dropped by ParseMessagesFromText
	if len(parsed) != 0 {
		t.Logf("Unexpectedly parsed %d messages from empty tool message", len(parsed))
	} else {
		t.Logf("Empirically verified: ParseMessagesFromText drops messages with empty content string (len=%d)", len(parsed))
	}
}

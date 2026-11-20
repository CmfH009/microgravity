package engine

import (
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/CmfH009/microgravity/internal/pruner"
)

// Condition 1: Serialization Round-Trip Fidelity & Concurrent Synchronization

func TestChallenger_RoundTrip_InterleavedMessages(t *testing.T) {
	originalMsgs := []Message{
		NewUserMessage("Deploy the microservice cluster to staging"),
		NewAssistantMessage("Analyzing repository structure and dependencies..."),
		NewToolMessage("list_files", "main.go\nDockerfile\ngo.mod\ngo.sum"),
		NewAssistantMessage("Found Dockerfile. Proceeding to verify local tests."),
		NewToolMessage("run_command", "=== RUN   TestCluster\n--- PASS: TestCluster (0.12s)\nPASS"),
		NewAssistantMessage("Tests passed cleanly. Verifying system directives."),
		NewSystemMessage("System Directive", "Preserve zero-downtime invariant during deployment"),
		NewAssistantMessage("Zero-downtime policy confirmed. Triggering background container build."),
		NewSystemMessage("Background Task Completed", "Task build-981 exited with code 0: image tag v1.4.2 created"),
		NewAssistantMessage("Deployment successfully completed. All pods healthy."),
	}

	formatted := FormatMessages(originalMsgs)
	t.Logf("--- Formatted Conversation ---\n%s\n--- End Formatted ---", formatted)

	parsed := ParseMessagesFromText(formatted)

	if len(parsed) != len(originalMsgs) {
		t.Errorf("Round-trip message count mismatch: expected %d, got %d", len(originalMsgs), len(parsed))
		for i, m := range parsed {
			t.Logf("parsed[%d]: Role=%s, Content=%q", i, m.Role, m.Content)
		}
	}

	for i := 0; i < len(originalMsgs) && i < len(parsed); i++ {
		orig := originalMsgs[i]
		p := parsed[i]
		if p.Role != orig.Role {
			t.Errorf("Message[%d] Role mismatch: expected %q, got %q (content: %q)", i, orig.Role, p.Role, p.Content)
		}
		if strings.TrimSpace(p.Content) != strings.TrimSpace(orig.Content) {
			t.Errorf("Message[%d] Content mismatch:\nExpected: %q\nGot:      %q", i, orig.Content, p.Content)
		}
	}

	// Test idempotency: parsing and reformatting again should not drift
	reformatted := FormatMessages(parsed)
	reparsed := ParseMessagesFromText(reformatted)
	if len(reparsed) != len(parsed) {
		t.Errorf("Second round-trip drift: expected %d messages, got %d", len(parsed), len(reparsed))
	}
}

func TestChallenger_SetMessages_ToolAssistant_LossOfRole(t *testing.T) {
	sess, err := NewSession(SessionConfig{
		SessionName: "tool_assistant_loss_test",
		ActiveModel: "gemini-3.8-flash",
	})
	if err != nil {
		t.Fatalf("Failed to initialize session: %v", err)
	}

	inputMsgs := []Message{
		NewUserMessage("What is the git status?"),
		NewAssistantMessage("Checking git status now..."),
		NewToolMessage("git_status", "On branch main\nnothing to commit, working tree clean"),
		NewAssistantMessage("The repository is clean and on branch main."),
	}

	sess.SetMessages(inputMsgs)

	// Now simulate reloading or parsing Conversation
	convoText := sess.Conversation.String()
	t.Logf("SetMessages Conversation text:\n%s", convoText)

	reconstructed := ParseMessagesFromText(convoText)
	if len(reconstructed) != len(inputMsgs) {
		t.Fatalf("Role collapse bug reproduced: expected %d messages, got %d. Assistant message after Tool was swallowed into Tool content!", len(inputMsgs), len(reconstructed))
	}
}

func TestChallenger_Concurrent_Session_AddMessage_Race(t *testing.T) {
	sess, err := NewSession(SessionConfig{
		SessionName: "challenger_concurrent_test",
		ActiveModel: "gemini-3.8-flash",
	})
	if err != nil {
		t.Fatalf("Failed to initialize session: %v", err)
	}

	numWriters := 20
	msgsPerWriter := 25
	var wg sync.WaitGroup

	// Launch concurrent writers
	for w := 0; w < numWriters; w++ {
		wg.Add(1)
		go func(writerID int) {
			defer wg.Done()
			for i := 0; i < msgsPerWriter; i++ {
				switch (writerID + i) % 4 {
				case 0:
					sess.AddMessage(NewUserMessage(fmt.Sprintf("Writer %d user query %d", writerID, i)))
				case 1:
					sess.AddMessage(NewAssistantMessage(fmt.Sprintf("Writer %d assistant response %d", writerID, i)))
				case 2:
					sess.AddMessage(NewToolMessage("tool", fmt.Sprintf("Writer %d tool output %d", writerID, i)))
				case 3:
					sess.AddMessage(NewSystemMessage("System Directive", fmt.Sprintf("Writer %d directive %d", writerID, i)))
				}
			}
		}(w)
	}

	// Launch concurrent readers
	numReaders := 10
	for r := 0; r < numReaders; r++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 20; i++ {
				_ = sess.GetMessages()
				_ = sess.GetState()
			}
		}()
	}

	// Launch concurrent syncer
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 10; i++ {
			sess.SyncConversation()
		}
	}()

	wg.Wait()

	finalMsgs := sess.GetMessages()
	expectedCount := numWriters * msgsPerWriter
	if len(finalMsgs) != expectedCount {
		t.Errorf("Concurrent AddMessage lost messages: expected %d, got %d", expectedCount, len(finalMsgs))
	}

	// Verify conversation string is synchronized with messages
	sess.mu.RLock()
	convoLen := sess.Conversation.Len()
	sess.mu.RUnlock()

	if convoLen == 0 {
		t.Errorf("Conversation buffer is empty after concurrent AddMessage")
	}

	formattedExpected := FormatMessages(finalMsgs)
	sess.mu.RLock()
	actualConvo := sess.Conversation.String()
	sess.mu.RUnlock()

	if actualConvo != formattedExpected {
		t.Errorf("Conversation buffer out of sync with Messages after concurrent execution")
	}
}

// Condition 2: Nested Letta Core Memory Tag Preservation Under Pruning Pressure

func TestChallenger_NestedLettaMemory_SeverePruning(t *testing.T) {
	rootGoal := "User: Implement fault-tolerant consensus engine with Letta memory integration.\nAgent: Starting implementation.\n"

	coreMemoryXML := `<core_memory>
<human>
User is a principal systems engineer requiring strict memory tag preservation.
Preferences: pure standard library where possible, zero memory corruption.
</human>
<persona>
Microgravity core autonomous agent with Letta episodic and hierarchical memory.
Role: resilient code synthesis and automated self-healing.
</persona>
<scratchpad>
Active Milestone: M3 Protocol & Memory Modernization.
Invariants: never truncate Turn 0, protect <core_memory> tags under all token budgets.
</scratchpad>
</core_memory>
`

	var sb strings.Builder
	sb.WriteString(rootGoal)

	// Append 30 turns of verbose tool and agent activity before core_memory
	for i := 1; i <= 30; i++ {
		sb.WriteString(fmt.Sprintf("Tool Output:\nVerbose execution logs for step %d with detailed stack trace and debug info...\nAgent: Completed step %d analysis.\n", i, i))
	}

	sb.WriteString(coreMemoryXML)

	// Append 30 turns of verbose activity after core_memory
	for i := 31; i <= 60; i++ {
		sb.WriteString(fmt.Sprintf("Tool Output:\nSubsequent execution output %d with extended profiling data...\nAgent: Completed subsequent step %d.\n", i, i))
	}

	fullConvo := sb.String()
	initialTokens := pruner.EstimateTokens(fullConvo)
	t.Logf("Initial conversation tokens: %d", initialTokens)

	// Test against aggressive token ceilings
	testBudgets := []int{500, 300, 200, 150, 100, 80}

	for _, budget := range testBudgets {
		t.Run(fmt.Sprintf("Budget_%d", budget), func(t *testing.T) {
			pruned, removed := pruner.PruneContext(fullConvo, budget)

			if removed == 0 {
				t.Errorf("Expected pruning for budget %d, but 0 tokens removed", budget)
			}

			// Invariant 1: <core_memory> opening tag must be present
			if !strings.Contains(pruned, "<core_memory>") {
				t.Fatalf("CRITICAL: <core_memory> opening tag was DROPPED under budget %d!\nPruned:\n%s", budget, pruned)
			}

			// Invariant 2: </core_memory> closing tag must be present
			if !strings.Contains(pruned, "</core_memory>") {
				t.Fatalf("CRITICAL: </core_memory> closing tag was DROPPED or severed under budget %d!\nPruned:\n%s", budget, pruned)
			}

			// Invariant 3: Tag order and completeness
			openIdx := strings.Index(pruned, "<core_memory>")
			closeIdx := strings.Index(pruned, "</core_memory>")
			if closeIdx <= openIdx {
				t.Fatalf("CRITICAL: Malformed <core_memory> tag order: open at %d, close at %d", openIdx, closeIdx)
			}

			// Invariant 4: Nested child tags must not be prematurely closed
			if !strings.Contains(pruned, "<human>") || !strings.Contains(pruned, "</human>") {
				t.Errorf("Nested <human> block missing or truncated under budget %d", budget)
			}
			if !strings.Contains(pruned, "<persona>") || !strings.Contains(pruned, "</persona>") {
				t.Errorf("Nested <persona> block missing or truncated under budget %d", budget)
			}
			if !strings.Contains(pruned, "<scratchpad>") || !strings.Contains(pruned, "</scratchpad>") {
				t.Errorf("Nested <scratchpad> block missing or truncated under budget %d", budget)
			}

			// Invariant 5: Turn 0 root prompt must survive
			if !strings.Contains(pruned, "Implement fault-tolerant consensus engine") {
				t.Errorf("Turn 0 root user prompt was evicted under budget %d", budget)
			}

			// Invariant 6: Pruning notice marker present
			if !strings.Contains(pruned, "[Earlier conversation context pruned for token efficiency]") {
				t.Errorf("Pruning notice marker missing under budget %d", budget)
			}
		})
	}
}

func TestChallenger_NestedLettaMemory_StructuredPruneMessages(t *testing.T) {
	coreMemoryXML := `<core_memory>
<human>
User preferences: high performance Go code.
</human>
<persona>
Microgravity core assistant.
</persona>
<scratchpad>
Milestone M3 verification.
</scratchpad>
</core_memory>`

	msgs := []Message{
		NewUserMessage("Turn 0: Execute architectural audit"),
		NewAssistantMessage("Starting audit process"),
	}

	for i := 1; i <= 25; i++ {
		msgs = append(msgs, NewToolMessage("tool", fmt.Sprintf("Audit output chunk %d with lots of data", i)))
	}

	msgs = append(msgs, NewSystemMessage("core_memory", coreMemoryXML))

	for i := 26; i <= 50; i++ {
		msgs = append(msgs, NewToolMessage("tool", fmt.Sprintf("Recent chunk %d with data", i)))
	}

	testBudgets := []int{400, 250, 150, 100}
	for _, budget := range testBudgets {
		t.Run(fmt.Sprintf("Budget_%d", budget), func(t *testing.T) {
			pruned, removed := PruneMessages(msgs, budget)
			if removed == 0 {
				t.Errorf("Expected tokens removed for budget %d", budget)
			}

			// Assert Turn 0 preserved at index 0
			if len(pruned) == 0 || pruned[0].Role != RoleUser || !strings.Contains(pruned[0].Content, "Turn 0") {
				t.Fatalf("Turn 0 lost in PruneMessages under budget %d", budget)
			}

			// Assert <core_memory> preserved in full
			foundCoreMem := false
			for _, m := range pruned {
				if strings.Contains(m.Content, "<core_memory>") && strings.Contains(m.Content, "</core_memory>") {
					foundCoreMem = true
					if !strings.Contains(m.Content, "<human>") || !strings.Contains(m.Content, "<persona>") || !strings.Contains(m.Content, "<scratchpad>") {
						t.Errorf("Core memory block lost nested tags under budget %d: %s", budget, m.Content)
					}
				}
			}
			if !foundCoreMem {
				t.Fatalf("<core_memory> was not preserved under budget %d in PruneMessages", budget)
			}
		})
	}
}

// Condition 3: System Prompt Letta Memory Invariants & Dynamic Token Budgeting

func TestChallenger_SystemPrompt_LettaInvariants(t *testing.T) {
	customCoreMemory := `<core_memory>
<human>Senior Go Architect</human>
<persona>Microgravity Assistant</persona>
<scratchpad>Executing stress tests</scratchpad>
</core_memory>`

	sess, err := NewSession(SessionConfig{
		SessionName: "system_prompt_test",
		ActiveModel: "gemini-3.8-flash",
		CoreMemory:  customCoreMemory,
	})
	if err != nil {
		t.Fatalf("Failed to create session: %v", err)
	}

	prompt := sess.BuildSystemPrompt()

	// Invariant 1: Must contain <core_memory>
	if !strings.Contains(prompt, "<core_memory>") {
		t.Errorf("BuildSystemPrompt() missing <core_memory> block")
	}
	if !strings.Contains(prompt, "</core_memory>") {
		t.Errorf("BuildSystemPrompt() missing </core_memory> closing tag")
	}
	if !strings.Contains(prompt, "<human>Senior Go Architect</human>") {
		t.Errorf("BuildSystemPrompt() missing nested <human> tag")
	}
	if !strings.Contains(prompt, "<persona>Microgravity Assistant</persona>") {
		t.Errorf("BuildSystemPrompt() missing nested <persona> tag")
	}
	if !strings.Contains(prompt, "<scratchpad>Executing stress tests</scratchpad>") {
		t.Errorf("BuildSystemPrompt() missing nested <scratchpad> tag")
	}

	// Invariant 2: Token budget check
	sysTokens := pruner.EstimateTokens(prompt)
	t.Logf("BuildSystemPrompt token count: %d", sysTokens)

	modelsToTest := []string{
		"gemini-1.5-pro",
		"gemini-3.8-flash",
		"llama-3.3-70b",
		"qwen2.5-coder:32b",
		"gemma4:e2b",
	}

	for _, model := range modelsToTest {
		maxTokens := pruner.MaxTokensForModel(model)
		outputReserve := 4096
		if maxTokens <= 8192 {
			outputReserve = 1536
		}

		maxConvoTokens := maxTokens - sysTokens - outputReserve
		if maxConvoTokens < 2000 {
			maxConvoTokens = 2000
		}

		totalAllocated := sysTokens + maxConvoTokens + outputReserve
		t.Logf("Model %-18s: maxTokens=%6d, sysTokens=%4d, maxConvoTokens=%6d, totalAllocated=%6d",
			model, maxTokens, sysTokens, maxConvoTokens, totalAllocated)

		// Check whether the context window allows positive conversational headroom
		if maxTokens > 8192 && maxConvoTokens < 5000 {
			t.Errorf("Model %s with %d tokens has unexpectedly constrained convo tokens: %d", model, maxTokens, maxConvoTokens)
		}
	}
}

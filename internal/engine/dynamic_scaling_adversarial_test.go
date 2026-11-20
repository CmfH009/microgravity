package engine

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/CmfH009/microgravity/internal/events"
	"github.com/CmfH009/microgravity/internal/mcp"
	"github.com/CmfH009/microgravity/internal/models"
	"github.com/CmfH009/microgravity/internal/pruner"
	"github.com/CmfH009/microgravity/internal/rules"
	"github.com/CmfH009/microgravity/internal/skills"
	"github.com/CmfH009/microgravity/internal/tools"
)

// TestAdversarial_DynamicContextScaling verifies that across all catalog models,
// BuildSystemPrompt() tokens are accurately accounted for, and the resulting
// fullPrompt (sysPrompt + separator + prunedConvo) strictly fits within the model's ContextWindow.
func TestAdversarial_DynamicContextScaling(t *testing.T) {
	scanner := models.NewScanner()
	catalog, err := scanner.Scan(context.Background())
	if err != nil {
		t.Fatalf("Failed to scan models catalog: %v", err)
	}
	if len(catalog) == 0 {
		t.Fatalf("Catalog returned 0 models")
	}

	// Set up a session with loaded tools, rules, and skills
	reg := tools.NewRegistry()
	for i := 1; i <= 15; i++ {
		reg.Register(mcp.Tool{
			Name:        fmt.Sprintf("tool_%d", i),
			Description: fmt.Sprintf("Autonomous tool number %d for systems development and verification testing.", i),
		}, func(ctx context.Context, args map[string]interface{}) (string, error) {
			return "output", nil
		})
	}

	loadedRules := &rules.Rules{
		GlobalContent: strings.Repeat("Enforce strict empirical verification and zero regression policies across all workspaces. ", 5),
	}

	var testSkills []*skills.Skill
	for i := 1; i <= 10; i++ {
		testSkills = append(testSkills, &skills.Skill{
			Name:        fmt.Sprintf("skill_%d", i),
			Description: fmt.Sprintf("Domain capability for microgravity architecture and engineering track %d.", i),
			Type:        "instruction",
		})
	}

	sessCfg := SessionConfig{
		SessionName:  "dynamic-scaling-adversarial-test",
		Registry:     reg,
		EventBus:     events.NewEventBus(),
		LoadedRules:  loadedRules,
		LoadedSkills: testSkills,
		Mode:         "plan",
		CavemanLevel: "lite",
	}

	sess, err := NewSession(sessCfg)
	if err != nil {
		t.Fatalf("Failed to create session: %v", err)
	}
	defer sess.Close()

	sysPrompt := sess.BuildSystemPrompt()
	sysTokens := pruner.EstimateTokens(sysPrompt)
	if sysTokens < 100 {
		t.Fatalf("System prompt unexpectedly small: %d tokens", sysTokens)
	}

	// Build a massive conversation with 30 tool iterations (50,000+ characters / ~15,000 tokens)
	var sb strings.Builder
	sb.WriteString("User: Implement comprehensive Paxos consensus protocol with disk WAL storage.\nAgent: Starting implementation.\n")
	for i := 1; i <= 30; i++ {
		sb.WriteString(fmt.Sprintf("Tool Output:\n[Iteration %d]: Executing test_paxos_leader_election... output data=%s\n",
			i, strings.Repeat("paxos_cluster_state_debug_telemetry_stream ", 20)))
		sb.WriteString(fmt.Sprintf("Agent: Step %d complete. Proceeding to next step.\n", i))
	}
	sb.WriteString("\n[System Directive]: Never drop consensus state.\n")
	sb.WriteString("<scratchpad>\nCurrent focus: Paxos leader lease timeout verification.\n</scratchpad>\n")
	largeConvo := sb.String()

	convoTokens := pruner.EstimateTokens(largeConvo)
	if convoTokens < 3000 {
		t.Fatalf("Expected large conversation, got %d tokens", convoTokens)
	}

	for _, model := range catalog {
		t.Run(fmt.Sprintf("Model_%s_CW_%d", model.ID, model.ContextWindow), func(t *testing.T) {
			maxModelTokens := pruner.MaxTokensForModel(model.ID)

			outputReserve := 4096
			if maxModelTokens <= 8192 {
				outputReserve = 1536
			}

			maxConvoTokens := maxModelTokens - sysTokens - outputReserve
			if maxConvoTokens < 2000 {
				maxConvoTokens = 2000
			}

			// Verify system tokens are deducted from model budget
			if maxConvoTokens > maxModelTokens {
				t.Fatalf("maxConvoTokens (%d) exceeded maxModelTokens (%d)", maxConvoTokens, maxModelTokens)
			}

			prunedConvo, _ := pruner.PruneContext(largeConvo, maxConvoTokens)
			fullPrompt := sysPrompt + "\n---CONVERSATION---\n" + prunedConvo
			fullPromptTokens := pruner.EstimateTokens(fullPrompt)

			// Invariant 1: Total prompt must fit within model's ContextWindow
			if fullPromptTokens > model.ContextWindow {
				t.Errorf("VIOLATION: Full prompt tokens (%d) exceeds active model ContextWindow (%d) for model %s!",
					fullPromptTokens, model.ContextWindow, model.ID)
			}

			// Invariant 2: Turn 0 root user prompt preserved
			if !strings.Contains(fullPrompt, "Implement comprehensive Paxos consensus protocol with disk WAL storage.") {
				t.Errorf("VIOLATION: Turn 0 dropped from full prompt for model %s!", model.ID)
			}

			// Invariant 3: Directives preserved
			if !strings.Contains(fullPrompt, "[System Directive]: Never drop consensus state.") {
				t.Errorf("VIOLATION: Directive dropped from full prompt for model %s!", model.ID)
			}
			if !strings.Contains(fullPrompt, "<scratchpad>\nCurrent focus: Paxos leader lease timeout verification.\n</scratchpad>") {
				t.Errorf("VIOLATION: Scratchpad block dropped from full prompt for model %s!", model.ID)
			}
		})
	}
}

// TestAdversarial_DynamicScalingUnderOllamaSmallContext specifically stress-tests local
// 8k models (gemma4:e2b, orbit-coder) under heavy system prompts and massive conversations.
func TestAdversarial_DynamicScalingUnderOllamaSmallContext(t *testing.T) {
	localModels := []string{
		"gemma4:e2b",
		"gemma4:e4b",
		"gemma:2b",
		"gemma:7b",
		"orbit-coder:latest",
		"orbit-thinker:latest",
		"orbit-sentinel:latest",
		"codegemma:7b",
	}

	// Create a heavy session prompt (~2000 tokens)
	reg := tools.NewRegistry()
	for i := 1; i <= 25; i++ {
		reg.Register(mcp.Tool{
			Name:        fmt.Sprintf("heavy_tool_%d", i),
			Description: fmt.Sprintf("Detailed documentation for heavy tool %d: accepts file inputs, parses AST, runs linters, checks formatting, validates security boundaries.", i),
		}, func(ctx context.Context, args map[string]interface{}) (string, error) {
			return "ok", nil
		})
	}

	sessCfg := SessionConfig{
		SessionName:  "heavy-prompt-8k-test",
		Registry:     reg,
		EventBus:     events.NewEventBus(),
		LoadedRules:  &rules.Rules{GlobalContent: strings.Repeat("Critical safety invariant: all database mutations must be transactionally isolated and reversible. ", 15)},
		CavemanLevel: "strict",
		Mode:         "plan",
	}

	sess, err := NewSession(sessCfg)
	if err != nil {
		t.Fatalf("Failed to create session: %v", err)
	}
	defer sess.Close()

	sysPrompt := sess.BuildSystemPrompt()
	sysTokens := pruner.EstimateTokens(sysPrompt)

	// Massive conversation: 50 tool outputs
	var sb strings.Builder
	sb.WriteString("User: Refactor distributed raft node discovery and peer gossiping.\nAgent: Starting.\n")
	for i := 1; i <= 50; i++ {
		sb.WriteString(fmt.Sprintf("Tool Output:\n[Step %d]: gossip packet received from peer %d... %s\n",
			i, i, strings.Repeat("peer_membership_hash_table_state ", 10)))
		sb.WriteString(fmt.Sprintf("Agent: Processed gossip round %d.\n", i))
	}
	convo := sb.String()

	for _, modelID := range localModels {
		t.Run(modelID, func(t *testing.T) {
			maxModelTokens := pruner.MaxTokensForModel(modelID)
			if maxModelTokens != 8192 {
				t.Fatalf("Expected 8192 maxModelTokens for %s, got %d", modelID, maxModelTokens)
			}

			outputReserve := 1536
			maxConvoTokens := maxModelTokens - sysTokens - outputReserve
			if maxConvoTokens < 2000 {
				maxConvoTokens = 2000
			}

			prunedConvo, removed := pruner.PruneContext(convo, maxConvoTokens)
			if removed == 0 {
				t.Fatalf("Expected pruning for model %s", modelID)
			}

			fullPrompt := sysPrompt + "\n---CONVERSATION---\n" + prunedConvo
			fullPromptTokens := pruner.EstimateTokens(fullPrompt)

			// The fullPrompt + outputReserve must fit within 8192 context window!
			if fullPromptTokens+outputReserve > 8192+100 { // Allow minor rounding slack
				t.Errorf("VIOLATION: Total tokens (%d full + %d reserve = %d) overflows 8192 context window for %s!",
					fullPromptTokens, outputReserve, fullPromptTokens+outputReserve, modelID)
			}

			if fullPromptTokens > 8192 {
				t.Errorf("VIOLATION: fullPromptTokens (%d) strictly exceeds 8192 ContextWindow for %s!",
					fullPromptTokens, modelID)
			}

			// Root user prompt intact
			if !strings.Contains(fullPrompt, "Refactor distributed raft node discovery and peer gossiping.") {
				t.Errorf("VIOLATION: Root prompt dropped in 8k model %s!", modelID)
			}
		})
	}
}

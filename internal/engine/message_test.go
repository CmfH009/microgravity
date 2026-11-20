package engine

import (
	"strings"
	"testing"
)

func TestMessage_FormatText(t *testing.T) {
	uMsg := NewUserMessage("hello world")
	if uMsg.FormatText() != "\nUser: hello world\nAgent: " {
		t.Errorf("unexpected user message format: %q", uMsg.FormatText())
	}

	aMsg := NewAssistantMessage("I will assist you.")
	if aMsg.FormatText() != "I will assist you." {
		t.Errorf("unexpected assistant message format: %q", aMsg.FormatText())
	}

	tMsg := NewToolMessage("tool", "output content")
	if tMsg.FormatText() != "\nTool Output:\noutput content\nAgent: " {
		t.Errorf("unexpected tool message format: %q", tMsg.FormatText())
	}

	sMsg := NewSystemMessage("System Directive", "Please continue.")
	if sMsg.FormatText() != "\n[System Directive]: Please continue.\nAgent: " {
		t.Errorf("unexpected system directive format: %q", sMsg.FormatText())
	}

	asyncMsg := NewSystemMessage("Background Task Completed", "build finished")
	if asyncMsg.FormatText() != "\n[Background Task Completed]: build finished\nAgent: " {
		t.Errorf("unexpected async event message format: %q", asyncMsg.FormatText())
	}
}

func TestFormatAndParseMessagesRoundTrip(t *testing.T) {
	original := "\nUser: List files in workspace\nAgent: I will list files.\nTool Output:\nmain.go\ngo.mod\nAgent: Here are the files.\n"
	msgs := ParseMessagesFromText(original)

	if len(msgs) < 3 {
		t.Fatalf("expected at least 3 messages, got %d", len(msgs))
	}
	if msgs[0].Role != RoleUser || msgs[0].Content != "List files in workspace" {
		t.Errorf("unexpected msg[0]: %+v", msgs[0])
	}
	if msgs[1].Role != RoleAssistant || msgs[1].Content != "I will list files." {
		t.Errorf("unexpected msg[1]: %+v", msgs[1])
	}
	if msgs[2].Role != RoleTool || !strings.Contains(msgs[2].Content, "main.go") {
		t.Errorf("unexpected msg[2]: %+v", msgs[2])
	}

	reformatted := FormatMessages(msgs)
	if !strings.Contains(reformatted, "User: List files in workspace") {
		t.Errorf("reformatted missing user turn: %s", reformatted)
	}
	if !strings.Contains(reformatted, "Tool Output:\nmain.go") {
		t.Errorf("reformatted missing tool output: %s", reformatted)
	}
}

func TestFormatAndParseMessagesRoundTrip_EmptyToolOutput(t *testing.T) {
	originalMsgs := []Message{
		NewUserMessage("touch newfile.txt"),
		NewAssistantMessage("Creating file."),
		NewToolMessage("touch", ""),
		NewAssistantMessage("File created."),
	}

	formatted := FormatMessages(originalMsgs)
	parsed := ParseMessagesFromText(formatted)

	if len(parsed) != len(originalMsgs) {
		t.Fatalf("expected %d messages, got %d", len(originalMsgs), len(parsed))
	}
	if parsed[2].Role != RoleTool {
		t.Fatalf("expected message[2] to be RoleTool, got %v", parsed[2].Role)
	}
	if parsed[2].Content != "" {
		t.Errorf("expected empty content for RoleTool, got %q", parsed[2].Content)
	}

	// Verify idempotency
	reformatted := FormatMessages(parsed)
	reparsed := ParseMessagesFromText(reformatted)
	if len(reparsed) != len(originalMsgs) {
		t.Fatalf("idempotency failed: expected %d messages on second parse, got %d", len(originalMsgs), len(reparsed))
	}
}

func TestSession_StructuredMessagesManagement(t *testing.T) {
	sess, err := NewSession(SessionConfig{
		SessionName: "test_msg_mgmt",
		ActiveModel: "gemini-3.8-flash",
	})
	if err != nil {
		t.Fatalf("failed to create session: %v", err)
	}

	sess.AddMessage(NewUserMessage("Write code"))
	sess.AddMessage(NewAssistantMessage("Working on it"))
	sess.AddMessage(NewToolMessage("tool", "compile passed"))

	msgs := sess.GetMessages()
	if len(msgs) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(msgs))
	}
	if msgs[0].Role != RoleUser || msgs[1].Role != RoleAssistant || msgs[2].Role != RoleTool {
		t.Errorf("unexpected roles: %v, %v, %v", msgs[0].Role, msgs[1].Role, msgs[2].Role)
	}

	// Verify Conversation mirror was updated
	convo := sess.Conversation.String()
	if !strings.Contains(convo, "User: Write code") || !strings.Contains(convo, "Tool Output:\ncompile passed") {
		t.Errorf("Conversation mirror out of sync: %s", convo)
	}

	// Test SetMessages
	newMsgs := []Message{
		NewUserMessage("Second goal"),
	}
	sess.SetMessages(newMsgs)
	if len(sess.GetMessages()) != 1 {
		t.Fatalf("expected 1 message after SetMessages, got %d", len(sess.GetMessages()))
	}
	if !strings.Contains(sess.Conversation.String(), "User: Second goal") {
		t.Errorf("expected updated Conversation mirror: %s", sess.Conversation.String())
	}
}

func TestEngine_PruneMessages(t *testing.T) {
	msgs := []Message{
		NewUserMessage("Turn 0: Implement the distributed protocol"),
		NewAssistantMessage("Understood, proceeding"),
		NewToolMessage("tool", "File created"),
		NewSystemMessage("System Directive", "Preserve all directives"),
		NewUserMessage("Middle turn 1"),
		NewAssistantMessage("Middle response 1"),
		NewUserMessage("Middle turn 2"),
		NewAssistantMessage("Middle response 2"),
		NewUserMessage("Recent turn: Finalize tests"),
		NewAssistantMessage("Tests finalized"),
	}

	// Prune with a budget that forces middle turn eviction
	pruned, count := PruneMessages(msgs, 60)
	if count == 0 {
		t.Logf("Tokens removed: %d", count)
	}

	// Verify Turn 0 is preserved
	if len(pruned) == 0 || pruned[0].Role != RoleUser || !strings.Contains(pruned[0].Content, "Turn 0") {
		t.Fatalf("Turn 0 was not strictly preserved! Pruned: %+v", pruned)
	}

	// Verify directive is preserved
	hasDirective := false
	for _, m := range pruned {
		if strings.Contains(m.Content, "Preserve all directives") {
			hasDirective = true
			break
		}
	}
	if !hasDirective {
		t.Errorf("Directive was not preserved in pruned messages")
	}

	// Verify recent turn is preserved
	hasRecent := false
	for _, m := range pruned {
		if strings.Contains(m.Content, "Finalize tests") {
			hasRecent = true
			break
		}
	}
	if !hasRecent {
		t.Errorf("Recent turn was not preserved in pruned messages")
	}
}

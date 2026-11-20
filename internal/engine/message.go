package engine

import (
	"fmt"
	"strings"
	"time"

	"github.com/purav/microgravity/internal/pruner"
)

// MessageRole defines the participant role in a conversation turn.
type MessageRole string

const (
	RoleSystem    MessageRole = "system"
	RoleUser      MessageRole = "user"
	RoleAssistant MessageRole = "assistant"
	RoleTool      MessageRole = "tool"
)

// ToolCall represents a structured tool invocation requested by the assistant.
type ToolCall struct {
	ID        string                 `json:"id,omitempty"`
	Name      string                 `json:"name"`
	Arguments map[string]interface{} `json:"arguments"`
}

// Message represents a discrete, typed conversation message in Microgravity.
type Message struct {
	Role       MessageRole       `json:"role"`
	Content    string            `json:"content"`
	Name       string            `json:"name,omitempty"`
	ToolCallID string            `json:"tool_call_id,omitempty"`
	ToolCalls  []ToolCall        `json:"tool_calls,omitempty"`
	Images     []string          `json:"images,omitempty"`
	Timestamp  time.Time         `json:"timestamp,omitempty"`
	Metadata   map[string]string `json:"metadata,omitempty"`
}

// NewUserMessage constructs a user message with optional images.
func NewUserMessage(content string, images ...string) Message {
	return Message{
		Role:      RoleUser,
		Content:   content,
		Images:    images,
		Timestamp: time.Now().UTC(),
	}
}

// NewAssistantMessage constructs an assistant response message with optional tool calls.
func NewAssistantMessage(content string, toolCalls ...ToolCall) Message {
	return Message{
		Role:      RoleAssistant,
		Content:   content,
		ToolCalls: toolCalls,
		Timestamp: time.Now().UTC(),
	}
}

// NewToolMessage constructs a tool output message correlated with a tool call.
func NewToolMessage(toolName, output string, toolCallID ...string) Message {
	id := ""
	if len(toolCallID) > 0 {
		id = toolCallID[0]
	}
	return Message{
		Role:       RoleTool,
		Name:       toolName,
		Content:    output,
		ToolCallID: id,
		Timestamp:  time.Now().UTC(),
	}
}

// NewSystemMessage constructs a system directive or notification message.
func NewSystemMessage(name, directive string) Message {
	return Message{
		Role:      RoleSystem,
		Name:      name,
		Content:   directive,
		Timestamp: time.Now().UTC(),
	}
}

// FormatText renders the message into Microgravity's canonical string representation.
func (m Message) FormatText() string {
	switch m.Role {
	case RoleUser:
		return fmt.Sprintf("\nUser: %s\nAgent: ", m.Content)
	case RoleAssistant:
		clean := m.Content
		if strings.HasPrefix(clean, "Agent: ") {
			clean = strings.TrimPrefix(clean, "Agent: ")
		} else if strings.HasPrefix(clean, "Agent:") {
			clean = strings.TrimPrefix(clean, "Agent:")
		}
		return clean
	case RoleTool:
		return fmt.Sprintf("\nTool Output:\n%s\nAgent: ", strings.TrimRight(m.Content, "\r\n"))
	case RoleSystem:
		if m.Name != "" && m.Name != "System Directive" {
			return fmt.Sprintf("\n[%s]: %s\nAgent: ", m.Name, m.Content)
		}
		if m.Name == "System Directive" || strings.HasPrefix(m.Content, "[System Directive]") {
			if strings.HasPrefix(m.Content, "[System Directive]") {
				return fmt.Sprintf("\n%s\nAgent: ", m.Content)
			}
			return fmt.Sprintf("\n[System Directive]: %s\nAgent: ", m.Content)
		}
		return fmt.Sprintf("\n%s\n", m.Content)
	default:
		return m.Content
	}
}

// FormatMessages converts a slice of structured messages into the canonical conversation string.
func FormatMessages(msgs []Message) string {
	var sb strings.Builder
	for i, m := range msgs {
		if i == 0 && m.Role == RoleAssistant {
			sb.WriteString("\nAgent: ")
		} else if i > 0 && msgs[i-1].Role == RoleAssistant && m.Role == RoleAssistant {
			sb.WriteString("\nAgent: ")
		}
		sb.WriteString(m.FormatText())
	}
	return sb.String()
}

// ParseMessagesFromText parses legacy concatenated conversation strings into structured []Message.
func ParseMessagesFromText(convo string) []Message {
	if strings.TrimSpace(convo) == "" {
		return nil
	}

	var messages []Message
	lines := strings.Split(convo, "\n")
	var currentRole MessageRole = ""
	var currentName string = ""
	var currentBuf strings.Builder

	flush := func() {
		if currentRole != "" {
			content := strings.TrimRight(currentBuf.String(), "\r\n")
			if strings.TrimSpace(content) != "" || currentRole == RoleTool {
				messages = append(messages, Message{
					Role:      currentRole,
					Name:      currentName,
					Content:   content,
					Timestamp: time.Now().UTC(),
				})
			}
			currentBuf.Reset()
			currentRole = ""
			currentName = ""
		}
	}

	inToolOutput := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		// Check for Tool Output marker
		if trimmed == "Tool Output:" {
			flush()
			currentRole = RoleTool
			inToolOutput = true
			continue
		}

		// Check for User marker
		if strings.HasPrefix(trimmed, "User: ") {
			flush()
			inToolOutput = false
			currentRole = RoleUser
			currentBuf.WriteString(strings.TrimPrefix(trimmed, "User: "))
			continue
		}

		// Check for Agent marker
		if trimmed == "Agent:" || strings.HasPrefix(trimmed, "Agent:") {
			flush()
			inToolOutput = false
			currentRole = RoleAssistant
			if trimmed != "Agent:" {
				content := strings.TrimPrefix(trimmed, "Agent:")
				currentBuf.WriteString(strings.TrimSpace(content))
			}
			continue
		}

		// Check for System Directives / Async Events: e.g. [System Directive]: ..., [Async Event]: ...
		if strings.HasPrefix(trimmed, "[") && strings.Contains(trimmed, "]: ") {
			closeBracket := strings.Index(trimmed, "]: ")
			label := trimmed[1:closeBracket]
			payload := trimmed[closeBracket+3:]
			isSystemLabel := !inToolOutput ||
				label == "System Directive" ||
				strings.HasPrefix(label, "Background Task") ||
				strings.HasPrefix(label, "Scheduler") ||
				strings.HasPrefix(label, "Subagent") ||
				strings.HasPrefix(label, "Async Event") ||
				strings.HasPrefix(label, "Doubt-Driven Verification")
			if isSystemLabel {
				flush()
				inToolOutput = false
				currentRole = RoleSystem
				currentName = label
				currentBuf.WriteString(payload)
				continue
			}
		}

		// Pruner Marker
		if trimmed == "[Earlier conversation context pruned for token efficiency]" {
			flush()
			inToolOutput = false
			messages = append(messages, Message{
				Role:      RoleSystem,
				Name:      "Pruner",
				Content:   "[Earlier conversation context pruned for token efficiency]",
				Timestamp: time.Now().UTC(),
			})
			continue
		}

		// Continued content
		if currentRole != "" {
			if currentBuf.Len() > 0 {
				currentBuf.WriteString("\n")
			}
			currentBuf.WriteString(line)
		} else {
			// Uncategorized text (default to User)
			currentRole = RoleUser
			currentBuf.WriteString(line)
		}
	}
	flush()

	return messages
}

// ToPrunerMessage converts an engine.Message to a pruner.Message.
func (m Message) ToPrunerMessage() pruner.Message {
	return pruner.Message{
		Role:       string(m.Role),
		Content:    m.Content,
		Name:       m.Name,
		ToolCallID: m.ToolCallID,
		Images:     m.Images,
		Timestamp:  m.Timestamp,
		Metadata:   m.Metadata,
	}
}

// FromPrunerMessage converts a pruner.Message to an engine.Message.
func FromPrunerMessage(pm pruner.Message) Message {
	return Message{
		Role:       MessageRole(pm.Role),
		Content:    pm.Content,
		Name:       pm.Name,
		ToolCallID: pm.ToolCallID,
		Images:     pm.Images,
		Timestamp:  pm.Timestamp,
		Metadata:   pm.Metadata,
	}
}

// PruneMessages prunes a slice of structured messages to fit maxTokens,
// strictly preserving Turn 0 (root user intent) and all directives.
func PruneMessages(msgs []Message, maxTokens int) ([]Message, int) {
	if len(msgs) == 0 {
		return nil, 0
	}
	pmsgs := make([]pruner.Message, len(msgs))
	for i, m := range msgs {
		pmsgs[i] = m.ToPrunerMessage()
	}
	pruned, removed := pruner.PruneMessages(pmsgs, maxTokens)
	result := make([]Message, len(pruned))
	for i, pm := range pruned {
		result[i] = FromPrunerMessage(pm)
	}
	return result, removed
}


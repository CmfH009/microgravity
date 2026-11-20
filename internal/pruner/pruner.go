package pruner

import (
	"strings"
	"time"
)

// EstimateTokens provides a fast heuristic token count (~4 chars per token).
func EstimateTokens(text string) int {
	if len(text) == 0 {
		return 0
	}
	return (len(text) + 3) / 4
}

// MaxTokensForModel returns the optimal working pruning context budget for a given model.
func MaxTokensForModel(model string) int {
	m := strings.ToLower(model)
	if strings.Contains(m, "gemini-1.5-pro") || strings.Contains(m, "gemini-2.5-pro") || strings.Contains(m, "gemini-3.1-pro") {
		return 256000
	}
	if strings.Contains(m, "gemini") || strings.Contains(m, "flash") || strings.Contains(m, "pro") {
		return 128000
	}
	if strings.Contains(m, "llama") {
		return 64000
	}
	if strings.Contains(m, "qwen") {
		return 32000
	}
	if strings.Contains(m, "gemma") || strings.Contains(m, "orbit") || strings.Contains(m, "codegemma") {
		return 8192
	}
	return 16000
}

// PruneContext strictly preserves Turn 0 (root user goal) and all directives
// (e.g. [System Directive], [Project Directive], <human>, <persona>, <scratchpad>),
// pruning middle conversation turns and tool outputs to fit maxTokens.
func PruneContext(convo string, maxTokens int) (string, int) {
	initialTokens := EstimateTokens(convo)
	if initialTokens <= maxTokens {
		return convo, 0
	}

	marker := "\n[Earlier conversation context pruned for token efficiency]\n"
	markerTokens := EstimateTokens(marker)

	// 1. Identify Turn 0 User Prompt
	var turn0 string
	var remainingConvo string

	firstUserIdx := strings.Index(convo, "User: ")
	if firstUserIdx != -1 {
		rest := convo[firstUserIdx+6:]
		secondUserIdx := strings.Index(rest, "\nUser: ")
		if secondUserIdx != -1 {
			turn0 = strings.TrimSpace(convo[firstUserIdx : firstUserIdx+6+secondUserIdx])
			remainingConvo = convo[firstUserIdx+6+secondUserIdx:]
		} else {
			// Single-turn autonomous tool loop: Turn 0 root user prompt ends at the first Tool Output or Agent response
			endIdx := len(convo)
			if toolIdx := strings.Index(rest, "\nTool Output:"); toolIdx != -1 {
				endIdx = firstUserIdx + 6 + toolIdx
			} else if agentIdx := strings.Index(rest, "\nAgent: "); agentIdx != -1 {
				endIdx = firstUserIdx + 6 + agentIdx
			}
			turn0 = strings.TrimSpace(convo[firstUserIdx:endIdx])
			remainingConvo = convo[endIdx:]
		}
	} else {
		// Fallback for conversation text without explicit "User: " prefix
		parts := strings.SplitN(convo, "\n", 2)
		turn0 = strings.TrimSpace(parts[0])
		if len(parts) > 1 {
			remainingConvo = parts[1]
		}
		// In fallback mode, clamp turn0 if it alone exceeds the budget
		if turn0Tokens := EstimateTokens(turn0); turn0Tokens > maxTokens-markerTokens && maxTokens > markerTokens {
			charLimit := (maxTokens - markerTokens) * 4
			if charLimit < len(turn0) && charLimit > 0 {
				turn0 = turn0[:charLimit]
			}
		}
	}

	turn0Tokens := EstimateTokens(turn0)

	// 2. Extract and protect all Directives from remainingConvo
	var directives []string
	var pruneableLines []string
	lines := strings.Split(remainingConvo, "\n")
	inDirectiveBlock := false
	isXmlTag := false
	var activeXmlTag string
	var currentDirective strings.Builder

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		isDirectiveStart := strings.HasPrefix(trimmed, "[System Directive]") ||
			strings.HasPrefix(trimmed, "[Project Directive]") ||
			strings.HasPrefix(trimmed, "<human>") ||
			strings.HasPrefix(trimmed, "<persona>") ||
			strings.HasPrefix(trimmed, "<scratchpad>") ||
			strings.HasPrefix(trimmed, "<core_memory>")

		if inDirectiveBlock {
			if isXmlTag {
				currentDirective.WriteString(line + "\n")
				if activeXmlTag != "" && strings.Contains(trimmed, "</"+activeXmlTag+">") {
					directives = append(directives, currentDirective.String())
					currentDirective.Reset()
					inDirectiveBlock = false
					isXmlTag = false
					activeXmlTag = ""
				}
				continue
			} else {
				// Bracket directive block ([System Directive], [Project Directive])
				// Ends if next line is empty, is another directive, or is a turn marker
				if trimmed == "" || strings.HasPrefix(trimmed, "Agent:") || strings.HasPrefix(trimmed, "User:") || strings.HasPrefix(trimmed, "Tool Output:") || isDirectiveStart {
					directives = append(directives, currentDirective.String())
					currentDirective.Reset()
					inDirectiveBlock = false
					// fall through to process this line
				} else {
					currentDirective.WriteString(line + "\n")
					continue
				}
			}
		}

		if isDirectiveStart {
			inDirectiveBlock = true
			currentDirective.WriteString(line + "\n")
			if strings.HasPrefix(trimmed, "<") {
				isXmlTag = true
				tag := strings.TrimPrefix(trimmed, "<")
				if idx := strings.IndexAny(tag, "> \t"); idx != -1 {
					activeXmlTag = tag[:idx]
				} else {
					activeXmlTag = strings.TrimSuffix(tag, ">")
				}
				if activeXmlTag != "" && strings.Contains(trimmed, "</"+activeXmlTag+">") {
					directives = append(directives, currentDirective.String())
					currentDirective.Reset()
					inDirectiveBlock = false
					isXmlTag = false
					activeXmlTag = ""
				}
			}
			continue
		}

		pruneableLines = append(pruneableLines, line)
	}

	if inDirectiveBlock && currentDirective.Len() > 0 {
		directives = append(directives, currentDirective.String())
	}

	directivesContent := strings.Join(directives, "")
	directivesTokens := EstimateTokens(directivesContent)

	// Turn 0 and Directives are strictly protected invariants: never evicted or truncated.
	protectedTokens := turn0Tokens + directivesTokens + markerTokens
	targetRecentTokens := maxTokens - protectedTokens
	if targetRecentTokens < 20 {
		targetRecentTokens = 20
	}

	// 3. Prune middle/older content from pruneableLines in chronological order (from top down)
	for len(pruneableLines) > 2 && EstimateTokens(strings.Join(pruneableLines, "\n")) > targetRecentTokens {
		dropCount := len(pruneableLines) / 4
		if dropCount < 1 {
			dropCount = 1
		}
		pruneableLines = pruneableLines[dropCount:]
	}

	recentBody := strings.Join(pruneableLines, "\n")
	if EstimateTokens(recentBody) > targetRecentTokens {
		charLimit := targetRecentTokens * 4
		if charLimit < len(recentBody) {
			recentBody = recentBody[len(recentBody)-charLimit:]
		}
	}

	// 4. Reassemble: Turn 0 + Directives + Marker + Recent Body
	var result strings.Builder
	result.WriteString(turn0)
	result.WriteString("\n")
	if directivesContent != "" {
		result.WriteString(directivesContent)
		if !strings.HasSuffix(directivesContent, "\n") {
			result.WriteString("\n")
		}
	}
	result.WriteString(marker)
	result.WriteString(recentBody)

	pruned := result.String()
	removed := initialTokens - EstimateTokens(pruned)
	if removed < 0 {
		removed = 0
	}
	return pruned, removed
}

// Message represents a structured message in the conversation pruner.
type Message struct {
	Role       string            `json:"role"`
	Content    string            `json:"content"`
	Name       string            `json:"name,omitempty"`
	ToolCallID string            `json:"tool_call_id,omitempty"`
	Images     []string          `json:"images,omitempty"`
	Timestamp  time.Time         `json:"timestamp,omitempty"`
	Metadata   map[string]string `json:"metadata,omitempty"`
}

// PruneMessages prunes a slice of structured messages to fit maxTokens,
// strictly preserving Turn 0 (root user intent) and all directives.
func PruneMessages(msgs []Message, maxTokens int) ([]Message, int) {
	if len(msgs) == 0 {
		return nil, 0
	}

	initialTokens := 0
	for _, m := range msgs {
		initialTokens += EstimateTokens(m.Content)
	}
	if initialTokens <= maxTokens {
		return msgs, 0
	}

	markerMsg := Message{
		Role:    "system",
		Name:    "Pruner",
		Content: "[Earlier conversation context pruned for token efficiency]",
	}
	markerTokens := EstimateTokens(markerMsg.Content)

	// 1. Identify Turn 0 (first user message or index 0)
	turn0Idx := 0
	for i, m := range msgs {
		if m.Role == "user" {
			turn0Idx = i
			break
		}
	}
	turn0 := msgs[turn0Idx]
	turn0Tokens := EstimateTokens(turn0.Content)

	// In fallback, clamp turn0 if it alone exceeds the budget
	if turn0Tokens > maxTokens-markerTokens && maxTokens > markerTokens {
		charLimit := (maxTokens - markerTokens) * 4
		if charLimit < len(turn0.Content) && charLimit > 0 {
			turn0.Content = turn0.Content[:charLimit]
			turn0Tokens = EstimateTokens(turn0.Content)
		}
	}

	// 2. Identify directives and protect them
	protectedIndices := make(map[int]bool)
	protectedIndices[turn0Idx] = true

	var directives []Message
	for i, m := range msgs {
		if i == turn0Idx {
			continue
		}
		trimmed := strings.TrimSpace(m.Content)
		isDirective := m.Role == "system" ||
			strings.HasPrefix(trimmed, "[System Directive]") ||
			strings.HasPrefix(trimmed, "[Project Directive]") ||
			strings.HasPrefix(trimmed, "<human>") ||
			strings.HasPrefix(trimmed, "<persona>") ||
			strings.HasPrefix(trimmed, "<scratchpad>") ||
			strings.HasPrefix(trimmed, "<core_memory>")
		if isDirective {
			directives = append(directives, m)
			protectedIndices[i] = true
		}
	}

	directivesTokens := 0
	for _, d := range directives {
		directivesTokens += EstimateTokens(d.Content)
	}

	protectedTokens := turn0Tokens + directivesTokens + markerTokens
	targetRecentTokens := maxTokens - protectedTokens
	if targetRecentTokens < 20 {
		targetRecentTokens = 20
	}

	// 3. Accumulate recent non-protected messages from end backwards
	var recentMessages []Message
	accumulatedRecentTokens := 0

	for i := len(msgs) - 1; i >= 0; i-- {
		if protectedIndices[i] {
			continue
		}
		msgTokens := EstimateTokens(msgs[i].Content)
		if accumulatedRecentTokens+msgTokens > targetRecentTokens && len(recentMessages) > 0 {
			break
		}
		recentMessages = append([]Message{msgs[i]}, recentMessages...)
		accumulatedRecentTokens += msgTokens
	}

	// 4. Assemble: Turn 0 + Directives + (Marker if pruned) + Recent
	var result []Message
	result = append(result, turn0)
	result = append(result, directives...)

	numPruned := len(msgs) - (1 + len(directives) + len(recentMessages))
	if numPruned > 0 {
		result = append(result, markerMsg)
	}
	result = append(result, recentMessages...)

	finalTokens := 0
	for _, m := range result {
		finalTokens += EstimateTokens(m.Content)
	}
	prunedCount := initialTokens - finalTokens
	if prunedCount < 0 {
		prunedCount = 0
	}

	return result, prunedCount
}

package engine

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/briandowns/spinner"
	"github.com/CmfH009/microgravity/internal/cmdparse"
	ctxPkg "github.com/CmfH009/microgravity/internal/context"
	"github.com/CmfH009/microgravity/internal/plan"
	"github.com/CmfH009/microgravity/internal/pruner"
	"github.com/CmfH009/microgravity/internal/router"
	"github.com/CmfH009/microgravity/internal/trajectory"
	"github.com/CmfH009/microgravity/internal/vision"
)

type streamingWriter struct {
	spinnerStarted bool
	spinner        *spinner.Spinner
	buf            strings.Builder
	passthrough    io.Writer
}

func (w *streamingWriter) Write(p []byte) (n int, err error) {
	if w.spinner != nil && w.spinnerStarted {
		w.spinner.Stop()
		w.spinnerStarted = false
		if w.passthrough == nil {
			fmt.Print("\r\033[K")
		}
	}
	if w.passthrough != nil {
		_, _ = w.passthrough.Write(p)
	} else {
		fmt.Print(string(p))
	}
	return w.buf.Write(p)
}

// RunPrompt executes a single turn or non-interactive prompt through the full engine pipeline.
func (s *Session) RunPrompt(ctx context.Context, prompt string, images []string, writer io.Writer) (string, error) {
	return s.ExecuteTurn(ctx, prompt, images, writer)
}

// ExecuteTurn processes a user turn, handles tool calling loops, and returns the final agent response.
func (s *Session) ExecuteTurn(ctx context.Context, userInput string, turnImages []string, writer io.Writer) (string, error) {
	s.SetState(StateThinking)
	defer s.SetState(StateIdle)

	cleanInput := strings.TrimSpace(userInput)

	// Extract images from text if passed inline (e.g. image paths)
	extractedInput, extractedImgs, err := vision.ExtractImages(cleanInput)
	if err == nil {
		cleanInput = extractedInput
		turnImages = append(turnImages, extractedImgs...)
	}

	// Process @file and @dir context references
	cleanInput, injectedContext, _ := ctxPkg.ProcessFileReferences(cleanInput)

	s.mu.Lock()
	s.Conversation.WriteString("\nUser: " + cleanInput + injectedContext + "\nAgent: ")
	s.Messages = append(s.Messages, NewUserMessage(cleanInput+injectedContext, turnImages...))
	s.mu.Unlock()

	if s.TrajLogger != nil {
		_ = s.TrajLogger.LogStep("USER_EXPLICIT", "USER_INPUT", "DONE", cleanInput+injectedContext, "", nil)
	}

	autoToolExecuted := false
	var finalResponse string
	turnCounter := 0
	healingAttempts := 0
	maxHealingAttempts := 3

	// Inner Agentic Loop: loops while tools are being executed
	for {
		turnCounter++
		s.mu.RLock()
		convoStr := s.Conversation.String()
		s.mu.RUnlock()

		maxModelTokens := pruner.MaxTokensForModel(s.Config.ActiveModel)
		sysPrompt := s.BuildSystemPrompt()
		sysTokens := pruner.EstimateTokens(sysPrompt)
		outputReserve := 4096
		if maxModelTokens <= 8192 {
			outputReserve = 1536
		}
		maxConvoTokens := maxModelTokens - sysTokens - outputReserve
		if maxConvoTokens < 2000 {
			maxConvoTokens = 2000
		}

		prunedConvo, _ := pruner.PruneContext(convoStr, maxConvoTokens)
		fullPrompt := sysPrompt + "\n---CONVERSATION---\n" + prunedConvo
		complexity := s.Router.Classify(cleanInput)

		var spin *spinner.Spinner
		if writer == nil {
			spin = spinner.New(spinner.CharSets[11], 100*time.Millisecond)
			spin.Suffix = " 🌌 Microgravity is thinking..."
			spin.Color("magenta", "bold")
			spin.Start()
		}

		sw := &streamingWriter{
			spinnerStarted: spin != nil,
			spinner:        spin,
			passthrough:    writer,
		}

		availableTools := s.GetAvailableTools()

		type genOutcome struct {
			text  string
			calls []cmdparse.ToolCall
		}

		ctxCancel, cancel := context.WithCancel(ctx)
		llmCh := make(chan genOutcome, 1)
		errCh := make(chan error, 1)

		go func() {
			defer cancel()
			activeModel := s.Config.ActiveModel
			provider := s.Config.ExplicitProvider

			// Check explicit provider preference
			if provider == "ollama" {
				res, err := s.OllamaClient.GenerateWithTools(ctxCancel, s.Config.LocalModelName, fullPrompt, turnImages, availableTools, sw)
				if err == nil {
					llmCh <- genOutcome{text: strings.TrimSpace(res.Text), calls: res.ToolCalls}
					return
				}
				resCloud, errCloud := s.CloudClient.GenerateWithTools(ctxCancel, s.Config.CloudModelName, fullPrompt, turnImages, availableTools, sw)
				if errCloud == nil {
					llmCh <- genOutcome{text: strings.TrimSpace(resCloud.Text), calls: resCloud.ToolCalls}
					return
				}
				errCh <- err
				return
			} else if provider == "cloud" {
				res, err := s.CloudClient.GenerateWithTools(ctxCancel, activeModel, fullPrompt, turnImages, availableTools, sw)
				if err == nil {
					llmCh <- genOutcome{text: strings.TrimSpace(res.Text), calls: res.ToolCalls}
					return
				}
				if sw.spinner != nil {
					sw.spinner.Stop()
				}
				resLocal, errLocal := s.OllamaClient.GenerateWithTools(ctxCancel, s.Config.LocalModelName, fullPrompt, turnImages, availableTools, sw)
				if errLocal == nil {
					llmCh <- genOutcome{text: strings.TrimSpace(resLocal.Text), calls: resLocal.ToolCalls}
					return
				}
				errCh <- err
				return
			}

			// Automatic Routing
			if complexity == router.ComplexitySimple {
				res, err := s.OllamaClient.GenerateWithTools(ctxCancel, s.Config.LocalModelName, fullPrompt, turnImages, availableTools, sw)
				if err == nil {
					llmCh <- genOutcome{text: strings.TrimSpace(res.Text), calls: res.ToolCalls}
					return
				}
			}

			resCloud, errCloud := s.CloudClient.GenerateWithTools(ctxCancel, activeModel, fullPrompt, turnImages, availableTools, sw)
			if errCloud == nil {
				llmCh <- genOutcome{text: strings.TrimSpace(resCloud.Text), calls: resCloud.ToolCalls}
				return
			}

			if sw.spinner != nil {
				sw.spinner.Stop()
			}
			resLocal, errLocal := s.OllamaClient.GenerateWithTools(ctxCancel, s.Config.LocalModelName, fullPrompt, turnImages, availableTools, sw)
			if errLocal == nil {
				llmCh <- genOutcome{text: strings.TrimSpace(resLocal.Text), calls: resLocal.ToolCalls}
				return
			}
			errCh <- errCloud
		}()

		var responseText string
		var nativeToolCalls []cmdparse.ToolCall
		var genErr error

		select {
		case err := <-errCh:
			if spin != nil {
				spin.Stop()
			}
			genErr = err
		case ev := <-s.AsyncEvents:
			if spin != nil {
				spin.Stop()
			}
			label := "Async Event"
			switch ev.Topic {
			case "task_completed":
				label = "Background Task Completed"
			case "scheduler_alerts":
				label = "Scheduler Alert"
			case "subagent_responses":
				label = "Subagent Response"
			}
			s.mu.Lock()
			s.Conversation.WriteString(fmt.Sprintf("\n[%s]: %s\nAgent: ", label, ev.Payload))
			s.Messages = append(s.Messages, NewSystemMessage(label, ev.Payload))
			s.mu.Unlock()
			_ = s.SaveHistory()
			continue // re-trigger model with async event update
		case outcome := <-llmCh:
			responseText = outcome.text
			nativeToolCalls = outcome.calls
			if spin != nil && sw.spinnerStarted {
				spin.Stop()
			}
			if writer == nil {
				fmt.Println()
			}
		case <-ctx.Done():
			if spin != nil {
				spin.Stop()
			}
			return "", ctx.Err()
		}

		if genErr != nil {
			return "", genErr
		}

		if strings.TrimSpace(responseText) == "" && len(nativeToolCalls) == 0 && turnCounter > 1 && turnCounter <= 4 {
			s.mu.Lock()
			s.Conversation.WriteString("\n[System Directive]: Please proceed with the autonomous implementation. Use tools to create or edit files and verify tests.\nAgent: ")
			s.Messages = append(s.Messages, NewSystemMessage("System Directive", "Please proceed with the autonomous implementation. Use tools to create or edit files and verify tests."))
			s.mu.Unlock()
			continue
		}

		finalResponse = responseText

		// Parse and track interactive execution plans
		if parsedPlan, err := plan.ParsePlan(responseText); err == nil && len(parsedPlan.Tasks) > 0 {
			s.ActivePlan = parsedPlan
			if writer == nil {
				fmt.Println(s.ActivePlan.RenderTUI())
			}
		}

		s.mu.Lock()
		s.Conversation.WriteString(responseText)
		s.Messages = append(s.Messages, NewAssistantMessage(responseText))
		s.mu.Unlock()
		_ = s.SaveHistory()

		// Extract tool calls (prioritize native tool calls from model, fallback to JSON text blocks)
		var reqs []cmdparse.ToolCall
		var hasToolCalls bool
		var parseErr error

		if len(nativeToolCalls) > 0 {
			reqs = nativeToolCalls
			hasToolCalls = true
		} else {
			extracted, ok, err := cmdparse.ExtractToolCalls(responseText)
			if ok && err == nil && len(extracted) > 0 {
				reqs = extracted
				hasToolCalls = true
			} else {
				parseErr = err
			}
		}
		var loggedTools []trajectory.ToolCallInfo
		toolExecuted := false

		if hasToolCalls && parseErr == nil && len(reqs) > 0 {
			for _, req := range reqs {
				loggedTools = append(loggedTools, trajectory.ToolCallInfo{
					ToolName:  req.ToolCall,
					Arguments: req.Arguments,
				})
			}
		}

		if s.TrajLogger != nil {
			_ = s.TrajLogger.LogStep("MODEL", "PLANNER_RESPONSE", "DONE", responseText, "", loggedTools)
		}

		if hasToolCalls && parseErr == nil && len(reqs) > 0 {
			toolExecuted = true
			s.SetState(StateExecutingTools)

			// 1. Detect if any tool modifies files
			isModifyingFiles := false
			for _, req := range reqs {
				tName := strings.ToLower(req.ToolCall)
				if tName == "write_to_file" || tName == "replace_file_content" ||
					tName == "edit_file" || tName == "write_file" {
					isModifyingFiles = true
					break
				}
			}

			// 2. Pre-execution git checkpointing
			if isModifyingFiles {
				_, _ = s.CreateCheckpoint(turnCounter, fmt.Sprintf("Turn %d pre-edit", turnCounter))
			}

			toolOutput, _ := s.Config.Executor.ExecuteToolCalls(ctx, reqs, s.Config.Cwd, s.ActivePlan)

			s.mu.Lock()
			s.Conversation.WriteString(fmt.Sprintf("\nTool Output:\n%s\n", toolOutput))
			s.Messages = append(s.Messages, NewToolMessage("tool", toolOutput))
			s.mu.Unlock()
			_ = s.SaveHistory()

			// 3. In-Turn Doubt-Driven Verification & Self-Correction
			if isModifyingFiles && s.Verifier != nil {
				s.SetState(StateVerifying)
				verResult, _ := s.Verifier.Verify(ctx, s.Config.Cwd)
				if verResult != nil && !verResult.Passed {
					if healingAttempts < maxHealingAttempts {
						healingAttempts++
						if s.TrajLogger != nil {
							_ = s.TrajLogger.LogStep("SYSTEM", "VERIFICATION_FAILURE", "ERROR", verResult.Output, "", nil)
						}
						diagStr := verResult.Output
						if len(verResult.Diagnoses) > 0 {
							var diagLines []string
							for _, d := range verResult.Diagnoses {
								diagLines = append(diagLines, fmt.Sprintf("- [%s] %s: %s", d.Framework, d.TestName, d.Message))
							}
							diagStr = strings.Join(diagLines, "\n") + "\n\nTest Output:\n" + verResult.Output
						}
						repairFeedback := fmt.Sprintf("\n[DOUBT-DRIVEN VERIFICATION FAILED (Pass %d/%d)] Automated tests failed after code modifications:\n%s\nPlease analyze the test failure and use file edit tools to correct the root cause immediately.\nAgent: ", healingAttempts, maxHealingAttempts, diagStr)
						s.mu.Lock()
						s.Conversation.WriteString(repairFeedback)
						s.Messages = append(s.Messages, NewSystemMessage(fmt.Sprintf("Doubt-Driven Verification Failed (Pass %d/%d)", healingAttempts, maxHealingAttempts), diagStr))
						s.mu.Unlock()
						_ = s.SaveHistory()
						continue // Self-correct!
					}
				}
			}

			s.mu.Lock()
			s.Conversation.WriteString("Agent: ")
			s.mu.Unlock()
			_ = s.SaveHistory()
			continue // Re-trigger LLM with tool outputs
		}

		// Fallback for hallucinated tool calls
		if !toolExecuted && !autoToolExecuted {
			lowerInput := strings.ToLower(cleanInput)
			lowerResp := strings.ToLower(responseText)

			hasPlaceholder := strings.Contains(lowerResp, "[link") || strings.Contains(lowerResp, "web search access") || strings.Contains(lowerResp, "[current time")
			wantsTime := strings.Contains(lowerInput, "date") || strings.Contains(lowerInput, "time") || strings.Contains(lowerInput, "today")
			wantsSearch := strings.Contains(lowerInput, "search") || strings.Contains(lowerInput, "look up")

			if hasPlaceholder || wantsTime || wantsSearch {
				autoToolExecuted = true
				var autoTool string
				var autoArgs map[string]interface{}
				if wantsTime {
					autoTool = "get_current_time"
					autoArgs = map[string]interface{}{}
				} else {
					autoTool = "search_web"
					autoArgs = map[string]interface{}{"query": cleanInput}
				}

				toolOutput, ok := s.Config.Executor.HandleToolCall(ctx, autoTool, autoArgs, s.Config.Cwd, s.ActivePlan)
				if ok && toolOutput != "" {
					s.mu.Lock()
					cur := s.Conversation.String()
					if strings.HasSuffix(cur, responseText) {
						s.Conversation.Reset()
						s.Conversation.WriteString(strings.TrimSuffix(cur, responseText))
					}
					if len(s.Messages) > 0 && s.Messages[len(s.Messages)-1].Role == RoleAssistant {
						s.Messages = s.Messages[:len(s.Messages)-1]
					}
					s.Messages = append(s.Messages, NewToolMessage(autoTool, toolOutput))
					s.Conversation.WriteString(fmt.Sprintf("Tool Output:\n%s\nAgent: ", toolOutput))
					s.mu.Unlock()
					_ = s.SaveHistory()
					continue
				}
			}
		}

		break
	}

	return finalResponse, nil
}

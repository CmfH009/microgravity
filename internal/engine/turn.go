package engine

import (
	"context"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/briandowns/spinner"
	"github.com/CmfH009/microgravity/internal/cloud"
	"github.com/CmfH009/microgravity/internal/cmdparse"
	ctxPkg "github.com/CmfH009/microgravity/internal/context"
	"github.com/CmfH009/microgravity/internal/executor"
	"github.com/CmfH009/microgravity/internal/mcp"
	"github.com/CmfH009/microgravity/internal/ollama"
	"github.com/CmfH009/microgravity/internal/openai"
	"github.com/CmfH009/microgravity/internal/plan"
	"github.com/CmfH009/microgravity/internal/pruner"
	"github.com/CmfH009/microgravity/internal/router"
	"github.com/CmfH009/microgravity/internal/trajectory"
	"github.com/CmfH009/microgravity/internal/verifier"
	"github.com/CmfH009/microgravity/internal/vision"
)

const defaultMaxTurns = 64

type streamingWriter struct {
	mu             sync.Mutex
	spinnerStarted bool
	spinner        *spinner.Spinner
	buf            strings.Builder
	passthrough    io.Writer
	closed         bool
}

func (w *streamingWriter) Write(p []byte) (n int, err error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return len(p), nil
	}
	if w.spinner != nil && w.spinnerStarted {
		w.spinner.Stop()
		w.spinnerStarted = false
		if w.passthrough == nil {
			fmt.Print("\r\033[K")
		}
	}
	if w.passthrough != nil {
		if _, wErr := w.passthrough.Write(p); wErr != nil {
			return 0, wErr
		}
	} else {
		fmt.Print(string(p))
	}
	return w.buf.Write(p)
}

func (w *streamingWriter) StopSpinner() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.spinner != nil && w.spinnerStarted {
		w.spinner.Stop()
		w.spinnerStarted = false
	}
}

func (w *streamingWriter) Disable() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.closed = true
	if w.spinner != nil && w.spinnerStarted {
		w.spinner.Stop()
		w.spinnerStarted = false
	}
}

type genOutcome struct {
	text  string
	calls []cmdparse.ToolCall
}

type turnDispatchParams struct {
	explicitProvider string
	routingMode      router.RoutingMode
	activeModel      string
	localModelName   string
	localFastModel   string
	localHeavyModel  string
	cloudModelName   string
	openAIBaseURL    string
	openAIModel      string
	routeDecision    router.RouteDecision
	complexity       router.Complexity
	fullPrompt       string
	turnImages       []string
	availableTools   []mcp.Tool
	ollamaClient     *ollama.Client
	openAIClient     *openai.Client
	cloudClient      *cloud.Client
	sw               *streamingWriter
}

func selectOpenAIModel(openAIModel, activeModel, localModelName, localFastModel, localHeavyModel string, routingMode router.RoutingMode, decision router.RouteDecision) string {
	if m := strings.TrimSpace(openAIModel); m != "" {
		return m
	}
	if decision.Complexity == router.ComplexityComplex && strings.TrimSpace(localHeavyModel) != "" {
		return strings.TrimSpace(localHeavyModel)
	}
	if strings.TrimSpace(localFastModel) != "" {
		return strings.TrimSpace(localFastModel)
	}
	if m := strings.TrimSpace(activeModel); m != "" {
		lower := strings.ToLower(m)
		if !strings.HasPrefix(lower, "gemini") && lower != "flash" && lower != "pro" {
			return m
		}
	}
	if routingMode == router.ModeLocalOnly && decision.Model != "" {
		return decision.Model
	}
	if decision.Model != "" {
		return decision.Model
	}
	return localModelName
}

func generateWithFailover(ctx context.Context, p turnDispatchParams) (genOutcome, error) {
	callOllama := func(modelName string) (genOutcome, error) {
		if p.ollamaClient == nil {
			return genOutcome{}, fmt.Errorf("ollama client not initialized")
		}
		if strings.TrimSpace(modelName) == "" {
			modelName = p.localModelName
		}
		res, err := p.ollamaClient.GenerateWithTools(ctx, modelName, p.fullPrompt, p.turnImages, p.availableTools, p.sw)
		if err != nil {
			return genOutcome{}, err
		}
		if res == nil {
			return genOutcome{}, fmt.Errorf("ollama returned nil result")
		}
		return genOutcome{text: strings.TrimSpace(res.Text), calls: res.ToolCalls}, nil
	}

	callOpenAI := func(modelName string) (genOutcome, error) {
		if p.openAIClient == nil {
			return genOutcome{}, fmt.Errorf("openai client not initialized")
		}
		res, err := p.openAIClient.GenerateWithTools(ctx, modelName, p.fullPrompt, p.turnImages, p.availableTools, p.sw)
		if err != nil {
			return genOutcome{}, err
		}
		if res == nil {
			return genOutcome{}, fmt.Errorf("openai returned nil result")
		}
		return genOutcome{text: strings.TrimSpace(res.Text), calls: res.ToolCalls}, nil
	}

	callCloud := func(modelName string) (genOutcome, error) {
		if p.cloudClient == nil {
			return genOutcome{}, fmt.Errorf("cloud client not initialized")
		}
		res, err := p.cloudClient.GenerateWithTools(ctx, modelName, p.fullPrompt, p.turnImages, p.availableTools, p.sw)
		if err != nil {
			return genOutcome{}, err
		}
		if res == nil {
			return genOutcome{}, fmt.Errorf("cloud returned nil result")
		}
		return genOutcome{text: strings.TrimSpace(res.Text), calls: res.ToolCalls}, nil
	}

	// OpenAI-compatible local backend never calls Cloud APIs.
	if p.explicitProvider == "openai" || strings.TrimSpace(p.openAIBaseURL) != "" {
		targetModel := selectOpenAIModel(
			p.openAIModel,
			p.activeModel,
			p.localModelName,
			p.localFastModel,
			p.localHeavyModel,
			p.routingMode,
			p.routeDecision,
		)
		return callOpenAI(targetModel)
	}

	localTarget := p.routeDecision.Model
	if localTarget == "" {
		localTarget = p.localModelName
	}

	// Strict local-only mode never touches Cloud APIs.
	if p.routingMode == router.ModeLocalOnly {
		return callOllama(localTarget)
	}

	switch p.explicitProvider {
	case "ollama":
		// Explicit ollama provider never falls back to Cloud APIs.
		if p.localFastModel != "" || p.localHeavyModel != "" {
			return callOllama(localTarget)
		}
		return callOllama(p.localModelName)

	case "cloud":
		out, err := callCloud(p.activeModel)
		if err == nil {
			return out, nil
		}
		if ctx.Err() != nil {
			return genOutcome{}, ctx.Err()
		}
		if p.routingMode == router.ModeCloudOnly {
			return genOutcome{}, err
		}
		p.sw.StopSpinner()
		outLocal, errLocal := callOllama(p.localModelName)
		if errLocal == nil {
			return outLocal, nil
		}
		return genOutcome{}, fmt.Errorf("cloud failed (%w); ollama fallback failed (%w)", err, errLocal)
	}

	if p.routingMode == router.ModeCloudOnly {
		return callCloud(p.activeModel)
	}

	// Automatic Hybrid Routing
	var ollamaErr error
	ollamaTried := false
	if p.complexity == router.ComplexitySimple {
		out, err := callOllama(localTarget)
		if err == nil {
			return out, nil
		}
		if ctx.Err() != nil {
			return genOutcome{}, ctx.Err()
		}
		ollamaErr = err
		ollamaTried = true
	}

	outCloud, errCloud := callCloud(p.activeModel)
	if errCloud == nil {
		return outCloud, nil
	}
	if ctx.Err() != nil {
		return genOutcome{}, ctx.Err()
	}

	if ollamaTried {
		return genOutcome{}, fmt.Errorf("ollama failed (%w); cloud failed (%w)", ollamaErr, errCloud)
	}

	p.sw.StopSpinner()
	outLocal, errLocal := callOllama(localTarget)
	if errLocal == nil {
		return outLocal, nil
	}
	return genOutcome{}, fmt.Errorf("cloud failed (%w); ollama fallback failed (%w)", errCloud, errLocal)
}

// RunPrompt executes a single turn or non-interactive prompt through the full engine pipeline.
func (s *Session) RunPrompt(ctx context.Context, prompt string, images []string, writer io.Writer) (string, error) {
	return s.ExecuteTurn(ctx, prompt, images, writer)
}

// ExecuteTurn processes a user turn, handles tool calling loops, and returns the final agent response.
func (s *Session) ExecuteTurn(ctx context.Context, userInput string, turnImages []string, writer io.Writer) (string, error) {
	if s == nil {
		return "", fmt.Errorf("session is nil")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}

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

	s.mu.RLock()
	activeModel := s.Config.ActiveModel
	localModelName := s.Config.LocalModelName
	localFastModel := s.Config.LocalFastModel
	localHeavyModel := s.Config.LocalHeavyModel
	cloudModelName := s.Config.CloudModelName
	explicitProvider := s.Config.ExplicitProvider
	routingModeRaw := s.Config.RoutingMode
	openAIBaseURL := s.Config.OpenAIBaseURL
	openAIModel := s.Config.OpenAIModel
	cwd := s.Config.Cwd
	maxTurns := s.Config.MaxTurns
	exec := s.Config.Executor
	ollamaClient := s.OllamaClient
	openAIClient := s.OpenAIClient
	cloudClient := s.CloudClient
	routerInst := s.Router
	verifierInst := s.Verifier
	trajLogger := s.TrajLogger
	asyncCh := s.AsyncEvents
	s.mu.RUnlock()

	if maxTurns <= 0 {
		maxTurns = defaultMaxTurns
	}
	if localModelName == "" {
		localModelName = "gemma:2b"
	}
	if cloudModelName == "" {
		cloudModelName = "gemini-3.6-flash"
	}
	if activeModel == "" {
		activeModel = cloudModelName
	}

	var routingMode router.RoutingMode
	if strings.TrimSpace(routingModeRaw) != "" {
		routingMode = router.NormalizeRoutingMode(routingModeRaw)
	} else if envMode, ok := router.RoutingModeFromEnv(); ok {
		routingMode = envMode
	} else {
		routingMode = router.ModeHybrid
	}

	effectiveLocalFast := strings.TrimSpace(localFastModel)
	if effectiveLocalFast == "" {
		effectiveLocalFast = localModelName
	}
	if routerInst == nil {
		routerInst = router.NewRouter()
	}

	s.mu.Lock()
	s.Conversation.WriteString("\nUser: " + cleanInput + injectedContext + "\nAgent: ")
	s.Messages = append(s.Messages, NewUserMessage(cleanInput+injectedContext, turnImages...))
	s.mu.Unlock()

	if trajLogger != nil {
		_ = trajLogger.LogStep("USER_EXPLICIT", "USER_INPUT", "DONE", cleanInput+injectedContext, "", nil)
	}

	autoToolExecuted := false
	var finalResponse string
	turnCounter := 0
	healingAttempts := 0
	maxHealingAttempts := 3

	// Inner Agentic Loop: loops while tools are being executed
	for {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		turnCounter++
		if turnCounter > maxTurns {
			return "", fmt.Errorf("turn loop exceeded max turns (%d)", maxTurns)
		}

		s.mu.RLock()
		convoStr := s.Conversation.String()
		s.mu.RUnlock()

		routeDecision := routerInst.SelectRoute(cleanInput, routingMode, effectiveLocalFast, localHeavyModel, activeModel)
		complexity := routeDecision.Complexity

		budgetModel := activeModel
		if explicitProvider == "openai" || strings.TrimSpace(openAIBaseURL) != "" {
			budgetModel = selectOpenAIModel(openAIModel, activeModel, localModelName, localFastModel, localHeavyModel, routingMode, routeDecision)
		} else if routingMode == router.ModeLocalOnly || explicitProvider == "ollama" || routeDecision.Provider == "ollama" {
			if routeDecision.Model != "" {
				budgetModel = routeDecision.Model
			} else {
				budgetModel = localModelName
			}
		}

		maxModelTokens := pruner.MaxTokensForModel(budgetModel)
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

		ctxCancel, cancel := context.WithCancel(ctx)
		llmCh := make(chan genOutcome, 1)
		errCh := make(chan error, 1)
		doneCh := make(chan struct{})

		go func() {
			defer close(doneCh)
			defer cancel()
			out, genErr := generateWithFailover(ctxCancel, turnDispatchParams{
				explicitProvider: explicitProvider,
				routingMode:      routingMode,
				activeModel:      activeModel,
				localModelName:   localModelName,
				localFastModel:   localFastModel,
				localHeavyModel:  localHeavyModel,
				cloudModelName:   cloudModelName,
				openAIBaseURL:    openAIBaseURL,
				openAIModel:      openAIModel,
				routeDecision:    routeDecision,
				complexity:       complexity,
				fullPrompt:       fullPrompt,
				turnImages:       turnImages,
				availableTools:   availableTools,
				ollamaClient:     ollamaClient,
				openAIClient:     openAIClient,
				cloudClient:      cloudClient,
				sw:               sw,
			})
			if genErr != nil {
				errCh <- genErr
				return
			}
			llmCh <- out
		}()

		var responseText string
		var nativeToolCalls []cmdparse.ToolCall
		var genErr error
		preemptedByAsync := false

	waitSelect:
		for {
			select {
			case err := <-errCh:
				<-doneCh
				sw.StopSpinner()
				genErr = err
				break waitSelect
			case ev, ok := <-asyncCh:
				if !ok {
					asyncCh = nil
					continue
				}
				sw.Disable()
				cancel()
				<-doneCh
				if err := ctx.Err(); err != nil {
					return "", err
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
				preemptedByAsync = true
				break waitSelect
			case outcome := <-llmCh:
				<-doneCh
				sw.StopSpinner()
				responseText = outcome.text
				nativeToolCalls = outcome.calls
				if writer == nil {
					fmt.Println()
				}
				break waitSelect
			case <-ctx.Done():
				sw.Disable()
				cancel()
				<-doneCh
				return "", ctx.Err()
			}
		}

		if preemptedByAsync {
			continue
		}
		if err := ctx.Err(); err != nil {
			return "", err
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
		var activePlan *plan.Plan
		if parsedPlan, err := plan.ParsePlan(responseText); err == nil && len(parsedPlan.Tasks) > 0 {
			s.mu.Lock()
			s.ActivePlan = parsedPlan
			activePlan = parsedPlan
			s.mu.Unlock()
			if writer == nil {
				fmt.Println(activePlan.RenderTUI())
			}
		} else {
			s.mu.RLock()
			activePlan = s.ActivePlan
			s.mu.RUnlock()
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

		if trajLogger != nil {
			_ = trajLogger.LogStep("MODEL", "PLANNER_RESPONSE", "DONE", responseText, "", loggedTools)
		}

		if hasToolCalls && parseErr == nil && len(reqs) > 0 {
			if exec == nil {
				return "", fmt.Errorf("tool executor is not initialized")
			}
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

			toolOutput, _ := exec.ExecuteToolCalls(ctx, reqs, cwd, activePlan)
			if err := ctx.Err(); err != nil {
				return "", err
			}

			s.mu.Lock()
			s.Conversation.WriteString(fmt.Sprintf("\nTool Output:\n%s\n", toolOutput))
			s.Messages = append(s.Messages, NewToolMessage("tool", toolOutput))
			s.mu.Unlock()
			_ = s.SaveHistory()

			// 3. In-Turn Doubt-Driven Verification & Self-Correction
			if isModifyingFiles && verifierInst != nil {
				s.SetState(StateVerifying)
				verResult, verErr := verifierInst.Verify(ctx, cwd)
				if err := ctx.Err(); err != nil {
					return "", err
				}
				if verErr != nil && verResult == nil {
					verResult = &verifier.VerificationResult{
						Passed: false,
						Output: verErr.Error(),
					}
				}
				if verResult != nil && !verResult.Passed {
					if healingAttempts < maxHealingAttempts {
						healingAttempts++
						if trajLogger != nil {
							_ = trajLogger.LogStep("SYSTEM", "VERIFICATION_FAILURE", "ERROR", verResult.Output, "", nil)
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
		if !toolExecuted && !autoToolExecuted && exec != nil {
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

				toolOutput, ok := exec.HandleToolCall(ctx, autoTool, autoArgs, cwd, activePlan)
				if err := ctx.Err(); err != nil {
					return "", err
				}
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

var _ = (*executor.Executor)(nil)

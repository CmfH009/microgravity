package cmd

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/CmfH009/microgravity/internal/agents"
	"github.com/CmfH009/microgravity/internal/config"
	"github.com/CmfH009/microgravity/internal/history"
	"github.com/CmfH009/microgravity/internal/router"
	"github.com/CmfH009/microgravity/internal/ui"
	"github.com/spf13/cobra"
)

var (
	rootAddDirs           []string
	rootAgent             string
	rootContinue          bool
	rootConversation      string
	rootDangerouslySkip   bool
	rootDisableSlash      bool
	rootEffort            string
	rootPromptInteractive string
	rootInputFormat       string
	rootOutputFormat      string
	rootJSONSchema        string
	rootLogFile           string
	rootMode              string
	rootModel             string
	rootNewProject        bool
	rootPrint             string
	rootPrintTimeout      time.Duration
	rootProject           string
	rootSandbox           bool
	rootDryRun            bool
	rootTheme             string
	rootLocal             bool
	rootRoutingMode       string
	rootBaseURL           string
	rootLocalFastModel    string
	rootLocalHeavyModel   string
)

func resolveRoutingMode(cfg *config.Config, localFlag bool, routingModeFlag string) string {
	if localFlag {
		return string(router.ModeLocalOnly)
	}
	if strings.TrimSpace(routingModeFlag) != "" {
		return string(router.NormalizeRoutingMode(routingModeFlag))
	}
	if envMode, ok := router.RoutingModeFromEnv(); ok {
		return string(envMode)
	}
	if cfg != nil && strings.TrimSpace(cfg.RoutingMode) != "" {
		return string(router.NormalizeRoutingMode(cfg.RoutingMode))
	}
	return string(router.ModeHybrid)
}

func resolveOpenAIBaseURL(cfg *config.Config, baseURLFlag string) string {
	if strings.TrimSpace(baseURLFlag) != "" {
		return strings.TrimSpace(baseURLFlag)
	}
	if envURL := strings.TrimSpace(os.Getenv("MG_OPENAI_BASE_URL")); envURL != "" {
		return envURL
	}
	if cfg != nil && strings.TrimSpace(cfg.OpenAIBaseURL) != "" {
		return strings.TrimSpace(cfg.OpenAIBaseURL)
	}
	return ""
}

func resolveOpenAIAPIKey(cfg *config.Config) string {
	if envKey := strings.TrimSpace(os.Getenv("MG_OPENAI_API_KEY")); envKey != "" {
		return envKey
	}
	if cfg != nil && strings.TrimSpace(cfg.OpenAIAPIKey) != "" {
		return strings.TrimSpace(cfg.OpenAIAPIKey)
	}
	return ""
}

func resolveLocalTierModels(cfg *config.Config, fastFlag, heavyFlag, fallbackLocal string) (string, string) {
	fast := strings.TrimSpace(fastFlag)
	if fast == "" {
		fast = strings.TrimSpace(os.Getenv("MG_LOCAL_FAST_MODEL"))
	}
	if fast == "" && cfg != nil {
		fast = strings.TrimSpace(cfg.LocalFastModel)
	}
	if fast == "" {
		fast = strings.TrimSpace(fallbackLocal)
	}

	heavy := strings.TrimSpace(heavyFlag)
	if heavy == "" {
		heavy = strings.TrimSpace(os.Getenv("MG_LOCAL_HEAVY_MODEL"))
	}
	if heavy == "" && cfg != nil {
		heavy = strings.TrimSpace(cfg.LocalHeavyModel)
	}
	return fast, heavy
}

func shouldSkipCloudAuth(cfg *config.Config, localFlag bool, routingModeFlag, baseURLFlag string) bool {
	if localFlag {
		return true
	}
	if resolveRoutingMode(cfg, localFlag, routingModeFlag) == string(router.ModeLocalOnly) {
		return true
	}
	if resolveOpenAIBaseURL(cfg, baseURLFlag) != "" {
		return true
	}
	if rootModel != "" && !isCloudModelID(rootModel) {
		return true
	}
	if rootModel == "" && cfg != nil && strings.TrimSpace(cfg.ActiveModel) != "" && !isCloudModelID(cfg.ActiveModel) {
		return true
	}
	return false
}

func isCloudModelID(modelID string) bool {
	lower := strings.ToLower(strings.TrimSpace(modelID))
	return strings.HasPrefix(lower, "gemini-") || strings.HasPrefix(lower, "gemma-2-") || lower == "flash" || lower == "pro"
}

var rootCmd = &cobra.Command{
	Use:   "microgravity [flags]",
	Short: "Microgravity is an AI agent CLI driven by the Gemma and Gemini model families",
	Long: `Microgravity (mg) is an autonomous AI agent CLI and execution environment
driven by local (Ollama/Gemma) and cloud-routed (Gemini/Gemma) LLM model architectures.`,
	Run: func(cmd *cobra.Command, args []string) {
		if rootLogFile != "" {
			if f, err := os.OpenFile(rootLogFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644); err == nil {
				defer f.Close()
			}
		}

		// Apply theme early so all subsequent UI rendering uses it
		if rootTheme != "" {
			ui.SetTheme(rootTheme)
		} else if savedTheme := config.GetTheme(); savedTheme != "" {
			ui.SetTheme(savedTheme)
		}

		if rootAgent != "" {
			if a, err := agents.GetAgentByName(rootAgent); err == nil {
				ui.PrintStepInfo(fmt.Sprintf("Running with Agent role: %s (%s)", a.Name, a.Role))
			} else {
				fmt.Printf("%s\n", ui.PrintError(fmt.Errorf("Unknown agent %q. Run 'mg agent' to list available agents.", rootAgent)))
				os.Exit(1)
			}
		}

		if rootModel != "" {
			_ = config.SetActiveModel(rootModel)
		}

		if rootConversation != "" {
			sessionName = rootConversation
			history.CurrentSession = rootConversation
		} else if rootContinue {
			// Continue latest conversation session
			sessList, _ := history.ListSessions()
			if len(sessList) > 0 {
				sessionName = sessList[0]
				history.CurrentSession = sessList[0]
			}
		}

		if rootNewProject {
			ui.PrintStepSuccess("Initialized new project session context.")
		}

		// Non-interactive print / prompt mode
		targetPrompt := rootPrint
		if targetPrompt == "" && len(args) > 0 {
			targetPrompt = strings.Join(args, " ")
		}

		if targetPrompt != "" || rootInputFormat == "stream-json" {
			executePrintMode(targetPrompt)
			return
		}

		// Interactive chat session
		if rootPromptInteractive != "" {
			initialPrompt = rootPromptInteractive
		}

		cfg, err := config.LoadConfig()
		if !shouldSkipCloudAuth(cfg, rootLocal, rootRoutingMode, rootBaseURL) {
			if err != nil || cfg.Token == nil || cfg.Token.AccessToken == "" || cfg.AutoAPIKey == "" {
				authCmd.Run(cmd, args)
			}
		}

		chatCmd.Run(cmd, args)
	},
}

func executePrintMode(prompt string) {
	ctx := context.Background()
	if rootPrintTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, rootPrintTimeout)
		defer cancel()
	}

	cfg, _ := config.LoadConfig()
	activeID := config.GetActiveModel()
	if rootModel != "" {
		activeID = rootModel
	}

	routingMode := resolveRoutingMode(cfg, rootLocal, rootRoutingMode)
	openAIBaseURL := resolveOpenAIBaseURL(cfg, rootBaseURL)
	openAIAPIKey := resolveOpenAIAPIKey(cfg)
	localFast, localHeavy := resolveLocalTierModels(cfg, rootLocalFastModel, rootLocalHeavyModel, "")
	openAIModel := ""
	if cfg != nil {
		openAIModel = strings.TrimSpace(cfg.OpenAIModel)
	}

	opts := SessionOptions{
		SessionName:     sessionName,
		ActiveModel:     activeID,
		Mode:            rootMode,
		Effort:          rootEffort,
		AutoApprove:     rootDangerouslySkip || true,
		DisableSlash:    rootDisableSlash,
		AddDirs:         rootAddDirs,
		Sandbox:         rootSandbox,
		DryRun:          rootDryRun,
		RoutingMode:     routingMode,
		LocalFastModel:  localFast,
		LocalHeavyModel: localHeavy,
		OpenAIBaseURL:   openAIBaseURL,
		OpenAIAPIKey:    openAIAPIKey,
		OpenAIModel:     openAIModel,
	}

	if openAIBaseURL != "" {
		opts.ExplicitProvider = "openai"
		if rootModel != "" {
			opts.OpenAIModel = rootModel
		}
	} else if routingMode == string(router.ModeLocalOnly) {
		if rootModel != "" && !isCloudModelID(rootModel) {
			opts.LocalModelName = rootModel
			if opts.LocalFastModel == "" {
				opts.LocalFastModel = rootModel
			}
		}
	} else if activeID != "" && !isCloudModelID(activeID) {
		opts.LocalModelName = activeID
		opts.ExplicitProvider = "ollama"
	} else if isCloudModelID(activeID) {
		opts.CloudModelName = activeID
		opts.ExplicitProvider = "cloud"
	}

	sess, cleanup, err := BuildEngineSession(ctx, opts)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s\n", ui.PrintError(fmt.Errorf("Engine session initialization failed: %w", err)))
		return
	}
	defer cleanup()

	isNonTextOutput := strings.ToLower(rootOutputFormat) == "json" || strings.ToLower(rootOutputFormat) == "stream-json"

	if rootInputFormat == "stream-json" {
		scanner := bufio.NewScanner(os.Stdin)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" {
				continue
			}

			var payload struct {
				Prompt string `json:"prompt"`
			}
			if err := json.Unmarshal([]byte(line), &payload); err == nil && payload.Prompt != "" {
				prompt = payload.Prompt
			} else {
				prompt = line
			}

			var outWriter io.Writer
			if isNonTextOutput {
				outWriter = io.Discard
			}

			res, _ := sess.RunPrompt(ctx, prompt, nil, outWriter)
			emitOutput(prompt, res)
		}
		return
	}

	var outWriter io.Writer
	if isNonTextOutput {
		outWriter = io.Discard
	}

	res, err := sess.RunPrompt(ctx, prompt, nil, outWriter)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s\n", ui.PrintError(err))
	}
	emitOutput(prompt, res)
}

func emitOutput(prompt, response string) {
	if rootJSONSchema != "" {
		// Enforce / validate JSON schema
		if !strings.HasPrefix(strings.TrimSpace(response), "{") && !strings.HasPrefix(strings.TrimSpace(response), "[") {
			// Wrap structured response
			wrapper := map[string]string{
				"prompt":   prompt,
				"response": response,
			}
			data, _ := json.MarshalIndent(wrapper, "", "  ")
			response = string(data)
		}
	}

	switch strings.ToLower(rootOutputFormat) {
	case "json":
		type jsonResp struct {
			Prompt    string `json:"prompt"`
			Response  string `json:"response"`
			Timestamp string `json:"timestamp"`
		}
		out := jsonResp{
			Prompt:    prompt,
			Response:  response,
			Timestamp: time.Now().Format(time.RFC3339),
		}
		data, _ := json.MarshalIndent(out, "", "  ")
		fmt.Println(string(data))
	case "stream-json":
		type ndjsonResp struct {
			Type      string `json:"type"`
			Content   string `json:"content"`
			Timestamp string `json:"timestamp"`
		}
		out := ndjsonResp{
			Type:      "response",
			Content:   response,
			Timestamp: time.Now().Format(time.RFC3339),
		}
		data, _ := json.Marshal(out)
		fmt.Println(string(data))
	default:
		fmt.Println(response)
	}
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func init() {
	rootCmd.PersistentFlags().StringSliceVar(&rootAddDirs, "add-dir", []string{}, "Add a directory to the workspace (repeatable)")
	rootCmd.Flags().StringVar(&rootAgent, "agent", "", "Agent for the current CLI session")
	rootCmd.Flags().BoolVarP(&rootContinue, "continue", "c", false, "Continue the most recent conversation")
	rootCmd.Flags().StringVar(&rootConversation, "conversation", "", "Resume a previous conversation by ID")
	rootCmd.Flags().BoolVar(&rootDangerouslySkip, "dangerously-skip-permissions", false, "Auto-approve all tool permission requests without prompting")
	rootCmd.Flags().BoolVar(&rootDisableSlash, "disable-slash-commands", false, "Disable slash command and skill expansion in print mode")
	rootCmd.Flags().StringVar(&rootEffort, "effort", "medium", "Reasoning effort for the current CLI session (low|medium|high)")
	rootCmd.Flags().StringVarP(&rootPromptInteractive, "prompt-interactive", "i", "", "Run an initial prompt interactively and continue the session")
	rootCmd.Flags().StringVar(&rootInputFormat, "input-format", "text", "Input format for print mode (text, stream-json)")
	rootCmd.Flags().StringVar(&rootOutputFormat, "output-format", "text", "Output format for print mode (text, json, stream-json)")
	rootCmd.Flags().StringVar(&rootJSONSchema, "json-schema", "", "Optional JSON schema string or path to a schema file to enforce structured output")
	rootCmd.Flags().StringVar(&rootLogFile, "log-file", "", "Override CLI log file path")
	rootCmd.Flags().StringVar(&rootMode, "mode", "default", "Set the agent execution mode for this session (accept-edits, plan)")
	rootCmd.Flags().StringVar(&rootModel, "model", "", "Model for the current CLI session")
	rootCmd.Flags().BoolVar(&rootNewProject, "new-project", false, "Create a new project for this session")
	rootCmd.Flags().StringVarP(&rootPrint, "print", "p", "", "Run a single prompt non-interactively and print the response")
	rootCmd.Flags().StringVar(&rootPrint, "prompt", "", "Alias for --print")
	rootCmd.Flags().DurationVar(&rootPrintTimeout, "print-timeout", 5*time.Minute, "Timeout for print mode wait")
	rootCmd.Flags().StringVar(&rootProject, "project", "", "Project ID or project name for the current CLI session")
	rootCmd.PersistentFlags().BoolVar(&rootSandbox, "sandbox", false, "Run in a sandbox with terminal restrictions enabled")
	rootCmd.Flags().BoolVar(&rootDryRun, "dry-run", false, "Preview planned tool calls without executing them")
	rootCmd.Flags().StringVar(&rootTheme, "theme", "", "UI theme: 'dark' (default), 'light', 'auto', 'tokyo-night', 'catppuccin', or 'high-contrast'")
	rootCmd.PersistentFlags().BoolVar(&rootLocal, "local", false, "Run strictly offline/local-only without cloud API calls (shortcut for --routing-mode local-only)")
	rootCmd.PersistentFlags().StringVar(&rootRoutingMode, "routing-mode", "", "Routing policy: hybrid|local-only|cloud-only")
	rootCmd.PersistentFlags().StringVar(&rootBaseURL, "base-url", "", "OpenAI-compatible local endpoint base URL (also reads MG_OPENAI_BASE_URL)")
	rootCmd.PersistentFlags().StringVar(&rootLocalFastModel, "local-fast-model", "", "Local fast-tier model for simple prompts")
	rootCmd.PersistentFlags().StringVar(&rootLocalHeavyModel, "local-heavy-model", "", "Local heavy-tier model for complex prompts")
}

package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/c-bata/go-prompt"
	"github.com/CmfH009/microgravity/internal/config"
	"github.com/CmfH009/microgravity/internal/engine"
	"github.com/CmfH009/microgravity/internal/history"
	"github.com/CmfH009/microgravity/internal/lock"
	"github.com/CmfH009/microgravity/internal/models"
	"github.com/CmfH009/microgravity/internal/ollama"
	"github.com/CmfH009/microgravity/internal/router"
	"github.com/CmfH009/microgravity/internal/sessions"
	"github.com/CmfH009/microgravity/internal/skills"
	"github.com/CmfH009/microgravity/internal/slash"
	"github.com/CmfH009/microgravity/internal/ui"
	"github.com/CmfH009/microgravity/internal/updater"
	"github.com/spf13/cobra"
)

var (
	sessionName   string
	initialPrompt string
)

func completer(d prompt.Document) []prompt.Suggest {
	word := d.GetWordBeforeCursor()
	if !strings.HasPrefix(word, "/") {
		return []prompt.Suggest{}
	}

	catalog := slash.Catalog()
	s := make([]prompt.Suggest, 0, len(catalog))
	for _, spec := range catalog {
		s = append(s, prompt.Suggest{
			Text:        spec.Name,
			Description: spec.Description,
		})
	}
	return prompt.FilterHasPrefix(s, word, true)
}

func formatSlashResultForCLI(res slash.Result) string {
	if res.Err != nil {
		return ui.PrintError(res.Err)
	}
	return ui.PrintSystemInfo(res.Output)
}

func handleLocalSlashCommand(sess *engine.Session, raw string) string {
	h := newSlashHandler(sess, nil, false)
	cmd := "/local"
	if trimmed := strings.TrimSpace(raw); trimmed != "" {
		cmd += " " + trimmed
	}
	return formatSlashResultForCLI(h.Execute(context.Background(), cmd))
}

func handleThemeSlashCommand(raw string) string {
	h := newSlashHandler(nil, nil, false)
	cmd := "/theme"
	if trimmed := strings.TrimSpace(raw); trimmed != "" {
		cmd += " " + trimmed
	}
	return formatSlashResultForCLI(h.Execute(context.Background(), cmd))
}

var chatCmd = &cobra.Command{
	Use:   "chat",
	Short: "Start an interactive chat session with Microgravity",
	Run: func(cmd *cobra.Command, args []string) {
		ctx := context.Background()
		history.CurrentSession = sessionName

		// Prevent multiple CLI instances blocking Ollama
		sessionLock, err := lock.Acquire("session")
		if err == nil {
			defer sessionLock.Release()
		}

		// ── Boot Sequence ──────────────────────────────────────
		ui.PrintBootBanner()

		notifyChan := make(chan string, 10)
		go func() {
			for msg := range notifyChan {
				fmt.Printf("\n%s %s\n> ", ui.PrintSystemInfo("[Scheduler Notification]"), msg)
			}
		}()

		cfgDisk, _ := config.LoadConfig()
		resolved := router.ResolveConfig(router.ConfigInput{
			Config:          cfgDisk,
			LocalFlag:       rootLocal,
			RoutingMode:     rootRoutingMode,
			ModelFlag:       rootModel,
			LocalModel:      modelName,
			CloudModel:      cloudModel,
			LocalFastModel:  rootLocalFastModel,
			LocalHeavyModel: rootLocalHeavyModel,
			OpenAIBaseURL:   rootBaseURL,
		})
		modelName = resolved.LocalModel
		cloudModel = resolved.CloudModel

		opts := SessionOptions{
			SessionName:      sessionName,
			ActiveModel:      resolved.ActiveModel,
			LocalModelName:   resolved.LocalModel,
			CloudModelName:   resolved.CloudModel,
			OllamaBaseURL:    baseURL,
			ExplicitProvider: resolved.ExplicitProvider,
			RoutingMode:      string(resolved.RoutingMode),
			LocalFastModel:   resolved.LocalFastModel,
			LocalHeavyModel:  resolved.LocalHeavyModel,
			OpenAIBaseURL:    resolved.OpenAIBaseURL,
			OpenAIAPIKey:     resolved.OpenAIAPIKey,
			OpenAIModel:      resolved.OpenAIModel,
			MCPServers:       mcpServers,
			NotifyChan:       notifyChan,
			AddDirs:          rootAddDirs,
			Sandbox:          rootSandbox,
			DryRun:           rootDryRun,
		}

		sess, cleanup, err := BuildEngineSession(ctx, opts)
		if err != nil {
			fmt.Println(ui.PrintError(err))
			return
		}
		defer cleanup()

		_ = sess.LoadSavedHistory()

		ollamaClient := ollama.NewClient(baseURL)
		localModelStatus := ""
		if opts.OpenAIBaseURL != "" {
			if modelsList, lErr := sess.OpenAIClient.ListModels(ctx); lErr == nil && len(modelsList) > 0 {
				localModelStatus = modelsList[0]
			} else if opts.OpenAIModel != "" {
				localModelStatus = opts.OpenAIModel
			} else {
				localModelStatus = opts.OpenAIBaseURL
			}
		} else {
			ollama.EnsureOllama()
			hasModel, _ := ollamaClient.HasModel(ctx, modelName)
			daemon := updater.NewDaemon(ollamaClient, modelName)
			daemon.Start()
			defer daemon.Stop()
			if hasModel {
				localModelStatus = modelName
			}
		}

		mcpConnected := len(sess.Config.MCPClients)
		loadedSkills := sess.Config.LoadedSkills

		ui.PrintStatusSummary(resolved.DisplayModel(), mcpConnected, len(loadedSkills), localModelStatus)

		if initialPrompt != "" {
			firstPrompt := initialPrompt
			initialPrompt = ""
			_, err = sess.ExecuteTurn(ctx, firstPrompt, nil, nil)
			if err != nil {
				fmt.Fprintf(os.Stderr, "%s\n", ui.PrintError(err))
			}
		}

		slashHandler := newSlashHandler(sess, notifyChan, false)

		for {
			input := prompt.Input("You: ", completer, prompt.OptionPrefixTextColor(prompt.Green))
			input = strings.TrimSpace(input)
			if input == "" {
				continue
			}

			if input == "quit" || input == "exit" {
				ui.PrintGoodbye()
				break
			}

			if res := slashHandler.Execute(ctx, input); res.Handled {
				switch res.Action {
				case slash.ActionExit:
					ui.PrintGoodbye()
					return
				case slash.ActionPaste:
					pasted, pErr := ReadMultiLineInput()
					if pErr != nil || strings.TrimSpace(pasted) == "" {
						continue
					}
					input = pasted
				case slash.ActionPickModel:
					selected, ok := models.RunTUI(ctx)
					if ok {
						applied := slashHandler.ApplyModel(ctx, slash.ModelOption{
							ID:       selected.ID,
							Name:     selected.Name,
							Provider: selected.Provider,
							Badge:    selected.RenderBadge(),
						})
						if selected.Provider == "cloud" {
							cloudModel = selected.ID
						} else {
							modelName = selected.ID
						}
						if applied.Err != nil {
							fmt.Println(ui.PrintError(applied.Err))
						} else {
							ui.PrintStepSuccess(applied.Output)
						}
					}
					continue
				case slash.ActionPickSkill:
					selected, ok := skills.RunTUI(loadedSkills)
					if ok && selected != nil {
						ui.PrintStepSuccess(fmt.Sprintf("Active Skill: %s (%s)", selected.Name, selected.Description))
						if selected.Body != "" {
							fmt.Println(ui.PrintHeader(" Skill Instructions "))
							fmt.Println(selected.Body)
							fmt.Println(ui.PrintSeparator())
						}
					}
					continue
				case slash.ActionPickSession:
					selected := sessions.RunTUI()
					if selected != "" {
						sessionName = selected
						switched := slashHandler.Execute(ctx, "/session "+selected)
						if switched.Err != nil {
							fmt.Println(ui.PrintError(switched.Err))
						} else {
							ui.PrintStepSuccess(switched.Output)
						}
					}
					continue
				default:
					if res.Action == slash.ActionReloadSession && res.ActionArg != "" {
						sessionName = res.ActionArg
					}
					if res.Err != nil {
						fmt.Println(ui.PrintError(res.Err))
						continue
					}
					if res.Prompt != "" {
						input = res.Prompt
					} else {
						if res.Title == "Help" || res.Title == "Audit" || res.Title == "Changelog" {
							fmt.Println(res.Output)
						} else if strings.Contains(res.Output, "\n") && res.Title != "" {
							fmt.Println(ui.PrintHeader(" " + res.Title + " "))
							fmt.Println(res.Output)
							fmt.Println(ui.PrintSeparator())
						} else if res.Output != "" {
							fmt.Println(ui.PrintSystemInfo(res.Output))
						}
						continue
					}
				}
			}

			recs := skills.AutoRecommend(input, loadedSkills)
			skills.PrintRecommendations(recs)

			_, err = sess.ExecuteTurn(ctx, input, nil, nil)
			if err != nil {
				fmt.Fprintf(os.Stderr, "%s\n", ui.PrintError(err))
			}
		}
	},
}

func init() {
	chatCmd.Flags().StringVarP(&modelName, "model", "m", "gemma:2b", "Ollama model name to use")
	chatCmd.Flags().StringVarP(&baseURL, "url", "u", "http://localhost:11434", "Ollama API base URL")
	chatCmd.Flags().StringVarP(&cloudModel, "cloud-model", "c", "gemini-3.6-flash", "Cloud Gemini model to use")
	chatCmd.Flags().StringArrayVarP(&mcpServers, "mcp-server", "s", []string{}, "Command to start an MCP stdio server (can be used multiple times)")
	chatCmd.Flags().StringVar(&sessionName, "session", "default", "Chat session name to resume")
	rootCmd.AddCommand(chatCmd)
}

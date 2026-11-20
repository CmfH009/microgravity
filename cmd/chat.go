package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/c-bata/go-prompt"
	"github.com/CmfH009/microgravity/internal/audit"
	"github.com/CmfH009/microgravity/internal/changelog"
	"github.com/CmfH009/microgravity/internal/config"
	"github.com/CmfH009/microgravity/internal/git"
	"github.com/CmfH009/microgravity/internal/history"
	"github.com/CmfH009/microgravity/internal/lock"
	"github.com/CmfH009/microgravity/internal/engine"
	"github.com/CmfH009/microgravity/internal/models"
	"github.com/CmfH009/microgravity/internal/ollama"
	"github.com/CmfH009/microgravity/internal/router"
	"github.com/CmfH009/microgravity/internal/rules"
	"github.com/CmfH009/microgravity/internal/scheduler"
	"github.com/CmfH009/microgravity/internal/sessions"
	"github.com/CmfH009/microgravity/internal/skills"
	"github.com/CmfH009/microgravity/internal/subagent"
	"github.com/CmfH009/microgravity/internal/ui"
	"github.com/CmfH009/microgravity/internal/updater"
	"github.com/CmfH009/microgravity/internal/vision"
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

	s := []prompt.Suggest{
		{Text: "/exit", Description: "Exit the chat session"},
		{Text: "/help", Description: "Show available commands"},
		{Text: "/clear", Description: "Clear context history"},
		{Text: "/skills", Description: "List loaded skills"},
		{Text: "/subagents", Description: "List, spawn, or kill subagents (/subagents list|spawn|kill-all)"},
		{Text: "/schedule", Description: "Schedule timers or cron jobs (/schedule timer 10s 'task')"},
		{Text: "/audit", Description: "Run project setup & directive health audit"},
		{Text: "/plan", Description: "Generate structured execution plan before proceeding"},
		{Text: "/goal", Description: "Run autonomous goal mode until fully complete"},
		{Text: "/learn", Description: "Persist custom directive to workspace rules"},
		{Text: "/paste", Description: "Open multi-line paste buffer mode"},
		{Text: "/grill-me", Description: "Interactive interview mode to clarify requirements"},
		{Text: "/model", Description: "Inspect catalog or switch active model (e.g. /model gemini-3.1-pro)"},
		{Text: "/diff", Description: "View git diff or working tree changes"},
		{Text: "/rewind", Description: "Instant rollback of last turn's file modifications"},
		{Text: "/undo", Description: "Instant rollback of last turn's file modifications"},
		{Text: "/usage", Description: "View token usage, budget, and model quotas"},
		{Text: "/credits", Description: "Inspect remaining G1 credits & tier status"},
		{Text: "/changelog", Description: "View release notes & changelog in TUI"},
		{Text: "/caveman", Description: "Toggle caveman mode (/caveman lite|full|ultra|off)"},
		{Text: "/dashboard", Description: "Launch Web & Terminal Telemetry Control Center"},
		{Text: "/fix", Description: "Run autonomous self-healing test repair & doubt loop"},
		{Text: "/ci", Description: "Generate production CI/CD GitHub Actions workflows"},
		{Text: "/mcp", Description: "Inspect and manage MCP servers (/mcp list|add|remove)"},
		{Text: "/rules", Description: "Inspect active project and global rules"},
		{Text: "/local", Description: "Toggle or set routing mode (/local [on|off|hybrid|local-only|cloud-only|status])"},
		{Text: "/theme", Description: "Switch UI theme (/theme [dark|light|auto|tokyo-night|catppuccin|high-contrast])"},
		{Text: "/teamwork-preview", Description: "Multi-agent swarm collaboration preview"},
		{Text: "/teamwork", Description: "Multi-agent swarm collaboration preview"},
	}

	return prompt.FilterHasPrefix(s, word, true)
}

func isSupportedTheme(name string) bool {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "dark", "light", "auto", "tokyo-night", "catppuccin", "high-contrast":
		return true
	default:
		return false
	}
}

func handleLocalSlashCommand(sess *engine.Session, raw string) string {
	arg := strings.ToLower(strings.TrimSpace(raw))
	switch arg {
	case "", "status":
		mode := config.GetRoutingMode()
		fast := modelName
		heavy := ""
		base := ""
		if sess != nil {
			mode = string(sess.GetRoutingMode())
			if sess.Config.LocalFastModel != "" {
				fast = sess.Config.LocalFastModel
			} else if sess.Config.LocalModelName != "" {
				fast = sess.Config.LocalModelName
			}
			heavy = sess.Config.LocalHeavyModel
			base = sess.Config.OpenAIBaseURL
		}
		if heavy == "" {
			heavy = fast
		}
		msg := fmt.Sprintf("Routing mode: %s (fast=%s, heavy=%s)", mode, fast, heavy)
		if base != "" {
			msg += fmt.Sprintf(" [openai base-url=%s]", base)
		}
		return ui.PrintSystemInfo(msg)

	case "on", "local", "local-only", "offline":
		if sess != nil {
			sess.SetRoutingMode(string(router.ModeLocalOnly))
		}
		if err := config.SetRoutingMode(string(router.ModeLocalOnly)); err != nil {
			return ui.PrintError(err)
		}
		return ui.PrintSystemInfo("Routing mode switched to local-only (cloud calls disabled).")

	case "off", "hybrid", "auto":
		if sess != nil {
			sess.SetRoutingMode(string(router.ModeHybrid))
		}
		if err := config.SetRoutingMode(string(router.ModeHybrid)); err != nil {
			return ui.PrintError(err)
		}
		return ui.PrintSystemInfo("Routing mode switched to hybrid.")

	case "cloud", "cloud-only":
		if sess != nil {
			sess.SetRoutingMode(string(router.ModeCloudOnly))
		}
		if err := config.SetRoutingMode(string(router.ModeCloudOnly)); err != nil {
			return ui.PrintError(err)
		}
		return ui.PrintSystemInfo("Routing mode switched to cloud-only.")

	default:
		return ui.PrintSystemInfo("Usage: /local [on|off|hybrid|local-only|cloud-only|status]")
	}
}

func handleThemeSlashCommand(raw string) string {
	arg := strings.ToLower(strings.TrimSpace(raw))
	if arg == "" || arg == "status" {
		return ui.PrintSystemInfo(fmt.Sprintf("Active theme: %s (available: dark, light, auto, tokyo-night, catppuccin, high-contrast)", ui.CurrentTheme.Name))
	}
	if !isSupportedTheme(arg) {
		return ui.PrintError(fmt.Errorf("unknown theme %q (supported: dark, light, auto, tokyo-night, catppuccin, high-contrast)", arg))
	}
	ui.SetTheme(arg)
	if err := config.SetTheme(arg); err != nil {
		return ui.PrintError(err)
	}
	return ui.PrintSystemInfo(fmt.Sprintf("Theme switched to %s.", arg))
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
		activeModelID := strings.TrimSpace(config.GetActiveModel())
		if rootModel != "" {
			activeModelID = strings.TrimSpace(rootModel)
		}
		if activeModelID != "" && isCloudModelID(activeModelID) {
			cloudModel = activeModelID
		} else if activeModelID != "" {
			modelName = activeModelID
		}

		routingMode := resolveRoutingMode(cfgDisk, rootLocal, rootRoutingMode)
		openAIBaseURL := resolveOpenAIBaseURL(cfgDisk, rootBaseURL)
		openAIAPIKey := resolveOpenAIAPIKey(cfgDisk)
		localFast, localHeavy := resolveLocalTierModels(cfgDisk, rootLocalFastModel, rootLocalHeavyModel, modelName)
		if localFast != "" && modelName == "gemma:2b" {
			modelName = localFast
		}
		openAIModel := ""
		if cfgDisk != nil {
			openAIModel = strings.TrimSpace(cfgDisk.OpenAIModel)
		}

		opts := SessionOptions{
			SessionName:     sessionName,
			ActiveModel:     activeModelID,
			LocalModelName:  modelName,
			CloudModelName:  cloudModel,
			OllamaBaseURL:   baseURL,
			RoutingMode:     routingMode,
			LocalFastModel:  localFast,
			LocalHeavyModel: localHeavy,
			OpenAIBaseURL:   openAIBaseURL,
			OpenAIAPIKey:    openAIAPIKey,
			OpenAIModel:     openAIModel,
			MCPServers:      mcpServers,
			NotifyChan:      notifyChan,
			AddDirs:         rootAddDirs,
			Sandbox:         rootSandbox,
			DryRun:          rootDryRun,
		}
		if opts.ActiveModel == "" {
			opts.ActiveModel = cloudModel
		}
		if openAIBaseURL != "" {
			opts.ExplicitProvider = "openai"
			if rootModel != "" {
				opts.OpenAIModel = rootModel
			}
		} else if activeModelID != "" && !isCloudModelID(activeModelID) {
			opts.ExplicitProvider = "ollama"
			opts.LocalModelName = activeModelID
			if opts.LocalFastModel == "" {
				opts.LocalFastModel = activeModelID
			}
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

		cwd, _ := os.Getwd()
		mcpConnected := len(sess.Config.MCPClients)
		loadedSkills := sess.Config.LoadedSkills

		displayModel := cloudModel
		if opts.OpenAIBaseURL != "" && opts.OpenAIModel != "" {
			displayModel = opts.OpenAIModel
		} else if opts.ExplicitProvider == "ollama" && opts.LocalModelName != "" {
			displayModel = opts.LocalModelName
		} else if opts.RoutingMode == string(router.ModeLocalOnly) && opts.LocalFastModel != "" {
			displayModel = opts.LocalFastModel
		}
		ui.PrintStatusSummary(displayModel, mcpConnected, len(loadedSkills), localModelStatus)

		if initialPrompt != "" {
			firstPrompt := initialPrompt
			initialPrompt = ""
			_, err = sess.ExecuteTurn(ctx, firstPrompt, nil, nil)
			if err != nil {
				fmt.Fprintf(os.Stderr, "%s\n", ui.PrintError(err))
			}
		}

		for {
			input := prompt.Input("You: ", completer, prompt.OptionPrefixTextColor(prompt.Green))
			input = strings.TrimSpace(input)

			if input == "/exit" || input == "quit" || input == "exit" {
				ui.PrintGoodbye()
				break
			}
			if input == "/clear" {
				sess.ClearHistory()
				fmt.Println(ui.PrintSystemInfo("🧹 Chat history cleared."))
				continue
			}
			if input == "/help" {
				fmt.Println("Available commands:\n  /exit       - Leave chat\n  /clear      - Clear history\n  /local      - Toggle local-only routing (/local [on|off|hybrid|local-only|cloud-only|status])\n  /theme      - Switch UI theme (/theme [dark|light|auto|tokyo-night|catppuccin|high-contrast])\n  /subagents  - Inspect/spawn/kill subagents (/subagents list|spawn|kill-all)\n  /schedule   - Set timers or cron jobs (/schedule timer 10s 'msg')\n  /audit      - Run project health & directive audit\n  /paste      - Multi-line paste buffer mode\n  /model      - Interactive model selector / switch model\n  /skills     - List loaded skills\n  /plan       - Request structured execution plan\n  /goal       - Execute autonomous goal loop\n  /learn      - Persist custom directive to rules.md\n  /grill-me   - Interactive requirements interview\n  /help       - Show this message")
				continue
			}
			if input == "/audit" {
				pa, err := audit.AuditWorkspace(cwd)
				if err != nil {
					fmt.Println(ui.PrintError(err))
				} else {
					fmt.Println(pa.RenderScorecard())
				}
				continue
			}
			if strings.HasPrefix(input, "/subagents") {
				raw := strings.TrimSpace(strings.TrimPrefix(input, "/subagents"))
				m := subagent.GetManager()
				if raw == "" || raw == "list" {
					list := m.List()
					if len(list) == 0 {
						fmt.Println(ui.PrintSystemInfo("No subagents currently active."))
					} else {
						fmt.Println(ui.PrintHeader(" Active Subagents "))
						for _, sa := range list {
							fmt.Printf(" • [%s] %s (%s) - %s [Workspace: %s]\n", sa.Status, sa.ID, sa.Role, sa.Prompt, sa.Workspace)
						}
						fmt.Println(ui.PrintSeparator())
					}
				} else if raw == "kill-all" {
					count := m.KillAll()
					ui.PrintStepSuccess(fmt.Sprintf("Terminated %d active subagent(s).", count))
				} else if strings.HasPrefix(raw, "spawn ") {
					promptTask := strings.TrimPrefix(raw, "spawn ")
					id := fmt.Sprintf("sub-%d", time.Now().UnixNano()%1000000)
					info, err := m.Spawn(ctx, id, "Subagent Worker", subagent.ProfileGeneral, promptTask, "", subagent.WorkspaceInherit, nil)
					if err != nil {
						fmt.Println(ui.PrintError(err))
					} else {
						ui.PrintStepSuccess(fmt.Sprintf("Spawned Subagent [%s] for task: %s", info.ID, promptTask))
					}
				} else {
					fmt.Println(ui.PrintSystemInfo("Usage: /subagents [list|kill-all|spawn <task>]"))
				}
				continue
			}
			if strings.HasPrefix(input, "/schedule") {
				raw := strings.TrimSpace(strings.TrimPrefix(input, "/schedule"))
				parts := strings.Fields(raw)
				if len(parts) >= 3 && parts[0] == "timer" {
					dur, err := time.ParseDuration(parts[1])
					if err != nil {
						fmt.Println(ui.PrintError(fmt.Errorf("Invalid duration: %v", err)))
					} else {
						msg := strings.Join(parts[2:], " ")
						id := sess.Config.Scheduler.ScheduleTimer(dur, msg, scheduler.ConditionNever, "", notifyChan)
						ui.PrintStepSuccess(fmt.Sprintf("Scheduled timer #%d for %s: %s", id, dur, msg))
					}
				} else {
					fmt.Println(ui.PrintSystemInfo("Usage: /schedule timer <duration e.g. 10s|5m> <message>"))
				}
				continue
			}
			if input == "/paste" {
				pasted, err := ReadMultiLineInput()
				if err != nil || pasted == "" {
					continue
				}
				input = pasted
			}
			if strings.HasPrefix(input, "/skills") {
				query := strings.TrimSpace(strings.TrimPrefix(input, "/skills"))
				if len(loadedSkills) == 0 {
					fmt.Println(ui.PrintSystemInfo("No skills loaded. Run 'microgravity skills add <name>' to create one."))
					continue
				}

				if query == "" || query == "pick" {
					selected, ok := skills.RunTUI(loadedSkills)
					if ok && selected != nil {
						ui.PrintStepSuccess(fmt.Sprintf("Active Skill: %s (%s)", selected.Name, selected.Description))
						if selected.Body != "" {
							fmt.Println(ui.PrintHeader(" Skill Instructions "))
							fmt.Println(selected.Body)
							fmt.Println(ui.PrintSeparator())
						}
					}
				} else {
					q := strings.ToLower(query)
					var matches []*skills.Skill
					for _, s := range loadedSkills {
						if strings.Contains(strings.ToLower(s.Name), q) ||
							strings.Contains(strings.ToLower(s.Description), q) ||
							strings.Contains(strings.ToLower(s.Type), q) {
							matches = append(matches, s)
						}
					}
					if len(matches) == 0 {
						fmt.Println(ui.PrintSystemInfo(fmt.Sprintf("No skills matching %q", query)))
					} else {
						fmt.Println(ui.PrintHeader(fmt.Sprintf(" Skill Search: %q (%d) ", query, len(matches))))
						for _, s := range matches {
							fmt.Println(skills.FormatSkillSummary(s))
						}
						fmt.Println(ui.PrintSeparator())
					}
				}
				continue
			}

			if strings.HasPrefix(input, "/sessions") {
				selected := sessions.RunTUI()
				if selected != "" {
					sessionName = selected
					history.CurrentSession = selected
					sess.Config.SessionName = selected
					_ = sess.LoadSavedHistory()
					ui.PrintStepSuccess(fmt.Sprintf("Switched to session %q", selected))
				}
				continue
			}

			if strings.HasPrefix(input, "/memory") {
				query := strings.TrimSpace(strings.TrimPrefix(input, "/memory"))
				if query == "" {
					fmt.Println(ui.PrintSystemInfo("Usage: /memory <search terms>"))
					continue
				}
				savedConvo := sess.Conversation.String()
				lines := strings.Split(savedConvo, "\n")
				var matches []string
				qLower := strings.ToLower(query)
				for _, line := range lines {
					if strings.Contains(strings.ToLower(line), qLower) {
						matches = append(matches, line)
					}
				}
				if len(matches) == 0 {
					fmt.Println(ui.PrintSystemInfo(fmt.Sprintf("No persistent memory results for %q", query)))
				} else {
					fmt.Println(ui.PrintHeader(fmt.Sprintf(" Memory Context Search (%d) ", len(matches))))
					for i, m := range matches {
						if i >= 10 {
							break
						}
						fmt.Printf(" • %s\n", m)
					}
					fmt.Println(ui.PrintSeparator())
				}
				continue
			}

			if strings.HasPrefix(input, "/learn") {
				ruleText := strings.TrimSpace(strings.TrimPrefix(input, "/learn"))
				if ruleText == "" {
					fmt.Println(ui.PrintSystemInfo("Usage: /learn <directive or rule text>"))
					continue
				}
				rulesDir := filepath.Join(cwd, ".microgravity")
				os.MkdirAll(rulesDir, 0755)
				rulesPath := filepath.Join(rulesDir, "rules.md")
				f, err := os.OpenFile(rulesPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
				if err != nil {
					fmt.Printf("%s\n", ui.PrintError(err))
				} else {
					f.WriteString("\n- " + ruleText + "\n")
					f.Close()
					ui.PrintStepComplete("Directive persisted to " + rulesPath)
				}
				continue
			}
			if strings.HasPrefix(input, "/model") {
				target := strings.TrimSpace(strings.TrimPrefix(input, "/model"))
				scanner := models.NewScanner()
				catalog, _ := scanner.Scan(ctx)

				if target == "" {
					selected, ok := models.RunTUI(ctx)
					if ok {
						sess.Config.ExplicitProvider = selected.Provider
						if selected.Provider == "cloud" {
							cloudModel = selected.ID
							sess.Config.CloudModelName = selected.ID
						} else {
							modelName = selected.ID
							sess.Config.LocalModelName = selected.ID
						}
						sess.Config.ActiveModel = selected.ID
						config.SetActiveModel(selected.ID)
						ui.PrintStepSuccess(fmt.Sprintf("Active model switched to: %s [%s] (%s)", selected.ID, selected.RenderBadge(), selected.Name))
					}
					continue
				}

				found := false
				for _, m := range catalog {
					if m.ID == target {
						found = true
						sess.Config.ExplicitProvider = m.Provider
						sess.Config.ActiveModel = m.ID
						config.SetActiveModel(m.ID)
						if m.Provider == "cloud" {
							cloudModel = target
							sess.Config.CloudModelName = target
						} else {
							modelName = target
							sess.Config.LocalModelName = target
							ollama.EnsureOllama()
							hasModel, _ := ollamaClient.HasModel(ctx, target)
							if !hasModel {
								ui.PrintStepInfo("Model " + target + " downloading in background — prompt anytime!")
								go func(mn string) {
									_ = ollamaClient.Pull(context.Background(), mn, nil)
								}(target)
							}
						}
						ui.PrintStepSuccess(fmt.Sprintf("Active model switched to: %s [%s] (%s)", m.ID, m.RenderBadge(), m.Name))
						break
					}
				}
				if !found {
					fmt.Println(ui.PrintError(fmt.Errorf("Unknown model '%s'. Launch selector with '/model' or list with 'mg models list'.", target)))
				}
				continue
			}
			if strings.HasPrefix(input, "/plan") {
				raw := strings.TrimSpace(strings.TrimPrefix(input, "/plan"))
				if raw == "" {
					fmt.Println(ui.PrintSystemInfo("Usage: /plan <task description>"))
					continue
				}
				sess.Config.Mode = "plan"
				input = "[PLAN MODE ACTIVATED] Formulate a clear, step-by-step Markdown execution plan with task checkboxes before taking implementation action: " + raw
			} else if strings.HasPrefix(input, "/grill-me") {
				raw := strings.TrimSpace(strings.TrimPrefix(input, "/grill-me"))
				if raw == "" {
					fmt.Println(ui.PrintSystemInfo("Usage: /grill-me <project or feature idea>"))
					continue
				}
				input = "[INTERVIEW MODE ACTIVATED] Conduct a 1-question-at-a-time interactive interview to clarify requirements and constraints: " + raw
			} else if strings.HasPrefix(input, "/goal") {
				raw := strings.TrimSpace(strings.TrimPrefix(input, "/goal"))
				if raw == "" {
					fmt.Println(ui.PrintSystemInfo("Usage: /goal <long-running goal>"))
					continue
				}
				input = "[GOAL MODE ACTIVATED] Relentlessly pursue this objective, self-correcting and executing until verified complete: " + raw
			} else if strings.HasPrefix(input, "/diff") {
				diff, err := git.GitDiff(cwd, false)
				if err != nil || strings.TrimSpace(diff) == "" {
					fmt.Println(ui.PrintSystemInfo("No git changes or working tree is clean."))
				} else {
					fmt.Println(ui.PrintHeader(" Working Copy Git Diff "))
					fmt.Println(diff)
					fmt.Println(ui.PrintSeparator())
				}
				continue
			} else if input == "/rewind" || input == "/undo" {
				if sess == nil {
					fmt.Println(ui.PrintSystemInfo("No active engine session to rewind."))
					continue
				}
				rewound, err := sess.Rewind()
				if err != nil {
					fmt.Printf("%s\n", ui.PrintError(fmt.Errorf("Rewind failed: %v", err)))
				} else {
					ui.PrintStepSuccess(fmt.Sprintf("Rewound working tree to checkpoint %s (Turn %d, Commit: %s)", rewound.ID, rewound.Turn, rewound.CommitSHA[:8]))
				}
				continue
			} else if strings.HasPrefix(input, "/usage") || strings.HasPrefix(input, "/quota") || strings.HasPrefix(input, "/credits") {
				activeModel := config.GetActiveModel()
				fmt.Println(ui.PrintHeader(" Model Quota & Resource Usage "))
				fmt.Printf(" • Active Model Tier: %s\n", activeModel)
				fmt.Printf(" • Remaining G1 Credits: Unlimited Developer Tier\n")
				fmt.Printf(" • Current Session Context: ~%d characters\n", sess.Conversation.Len())
				fmt.Println(ui.PrintSeparator())
				continue
			} else if strings.HasPrefix(input, "/changelog") {
				fmt.Print(changelog.Render())
				continue
			} else if strings.HasPrefix(input, "/statusline") {
				statusLine := ui.RenderStatusLine(config.GetActiveModel(), len(loadedSkills), mcpConnected, cwd)
				fmt.Println(ui.PrintSystemInfo("Statusline: " + statusLine))
				continue
			} else if strings.HasPrefix(input, "/caveman") {
				raw := strings.TrimSpace(strings.TrimPrefix(input, "/caveman"))
				if raw == "off" || raw == "disable" || raw == "stop" {
					ui.PrintStepSuccess("Caveman mode disabled. Normal communication restored.")
					sess.Config.CavemanLevel = "off"
				} else {
					level := "full"
					if raw == "lite" || raw == "ultra" || raw == "max" {
						level = raw
					}
					ui.PrintStepSuccess(fmt.Sprintf("Caveman mode active (level: %s). Ultra-compressed responses.", level))
					sess.Config.CavemanLevel = level
				}
				continue
			} else if strings.HasPrefix(input, "/rules") {
				loadedRules, _ := rules.LoadDefaultRules(cwd)
				fmt.Println(ui.PrintHeader(" Active Antigravity Rules "))
				fmt.Println(loadedRules.FormatForSystemPrompt())
				fmt.Println(ui.PrintSeparator())
				continue
			} else if strings.HasPrefix(input, "/mcp") {
				servers := config.GetMCPServers()
				fmt.Println(ui.PrintHeader(" MCP Servers & Extensions "))
				if len(servers) == 0 {
					fmt.Println(" • No external MCP servers configured. (Use 'mg mcp add' or config.json)")
				} else {
					for _, s := range servers {
						fmt.Printf(" • %s\n", s)
					}
				}
				fmt.Println(ui.PrintSeparator())
				continue
			} else if input == "/local" || strings.HasPrefix(input, "/local ") {
				fmt.Println(handleLocalSlashCommand(sess, strings.TrimPrefix(input, "/local")))
				continue
			} else if input == "/theme" || strings.HasPrefix(input, "/theme ") {
				fmt.Println(handleThemeSlashCommand(strings.TrimPrefix(input, "/theme")))
				continue
			} else if strings.HasPrefix(input, "/dashboard") {
				fmt.Println(ui.PrintSystemInfo("Launching Telemetry Control Center (mg dashboard)... Run in separate terminal or use 'mg dashboard -w'."))
				continue
			} else if strings.HasPrefix(input, "/fix") {
				fmt.Println(ui.PrintSystemInfo("Running autonomous test healing verifier (mg fix)..."))
				continue
			} else if strings.HasPrefix(input, "/ci") {
				fmt.Println(ui.PrintSystemInfo("Generate CI workflow with: mg ci -o .github/workflows"))
				continue
			} else if strings.HasPrefix(input, "/teamwork-preview") || strings.HasPrefix(input, "/teamwork") {
				fmt.Println(ui.PrintHeader(" Autonomous Agent Teamwork & Swarm Mode "))
				fmt.Println(" • Swarm Workers: Researcher, Code Reviewer, Security Auditor, Test Engineer")
				fmt.Println(" • Workspaces: Branch / Share / Inherit")
				fmt.Println(" • Launch parallel tasks with: mg swarm <task 1> <task 2>")
				fmt.Println(ui.PrintSeparator())
				continue
			}

			if input == "" {
				continue
			}

			var responseText string

			recs := skills.AutoRecommend(input, loadedSkills)
			skills.PrintRecommendations(recs)

			// NOTE: Now images are persisted in the conversation history across multiple turns.
			cleanInput, turnImages, err := vision.ExtractImages(input)
			if err != nil {
				fmt.Printf("%s\n", ui.PrintError(fmt.Errorf("Error processing images: %v", err)))
				continue
			}

			historyDir, _ := history.GetHistoryDir()
			visionHistory := history.NewHistoryWithDir(historyDir)

			if len(turnImages) > 0 {
				_ = visionHistory.SaveEntry(history.HistoryEntry{
					UserText: cleanInput,
					Images:   turnImages,
				})
			}

			// Load all past images for this turn
			var allImages []string
			entries, _ := visionHistory.LoadEntries()
			for _, e := range entries {
				allImages = append(allImages, e.Images...)
			}
			// Append current turn images
			allImages = append(allImages, turnImages...)
			images := allImages

			// Retrieve relevant context from cross-session memory
			if sess.Config.MemoryStore != nil {
				ctxResults, memErr := sess.Config.MemoryStore.RetrieveRelevant(cleanInput, 3)
				if memErr == nil && len(ctxResults) > 0 {
					var ctx strings.Builder
					ctx.WriteString("[Relevant context from past sessions]\n")
					for _, r := range ctxResults {
						ctx.WriteString(fmt.Sprintf("  - %s\n", r.Text))
					}
					ctx.WriteString("\n")
					cleanInput = ctx.String() + cleanInput
				}
			}

			responseText, err = sess.ExecuteTurn(ctx, cleanInput, images, nil)
			if err != nil {
				fmt.Fprintf(os.Stderr, "%s\n", ui.PrintError(err))
			}

			// Store turn in cross-session memory
			if sess.Config.MemoryStore != nil {
				// Get the assistant's last response from conversation
				lastResp := ""
				msgs := sess.GetMessages()
				if len(msgs) > 0 {
					lastMsg := msgs[len(msgs)-1]
					if lastMsg.Role == engine.RoleAssistant {
						lastResp = lastMsg.Content
					}
				}
				if lastResp == "" {
					lastResp = responseText // fallback from ExecuteTurn return
				}
				_ = sess.Config.MemoryStore.StoreTurn(sessionName, cleanInput, lastResp)
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

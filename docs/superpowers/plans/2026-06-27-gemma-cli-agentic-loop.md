# Agentic ReAct Loop Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Upgrade the `chat` command to act as an autonomous agent. It will inject MCP tools into the system prompt, parse the model's responses for tool call JSON, execute the tools using the MCP client, and loop back to the model with the tool output.

**Architecture:**
- **System Prompt:** If an MCP server is attached, fetch its tools and build a system prompt explaining how to use them.
- **Execution Loop:** Wrap the `Generate` call in an inner loop. If the model outputs a specific JSON block requesting a tool, we intercept it, run `mcpClient.CallTool`, append the result to the context, and automatically trigger another `Generate` without asking the user.

**Tech Stack:** Go, standard `regexp`, `encoding/json`.

---

### Task 1: Update Chat Command for Agentic Loop

**Files:**
- Modify: `C:\AgyHut\microgravity\cmd\chat.go`

- [ ] **Step 1: Update chat.go to support MCP and ReAct**
Modify `cmd/chat.go` to add the MCP initialization (similar to `run.go`) and the ReAct inner loop.

```go
package cmd

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/spf13/cobra"
	"github.com/purav/microgravity/internal/cloud"
	"github.com/purav/microgravity/internal/mcp"
	"github.com/purav/microgravity/internal/ollama"
	"github.com/purav/microgravity/internal/router"
	"github.com/purav/microgravity/internal/updater"
)

// Regex to find our JSON tool call block
var toolCallRegex = regexp.MustCompile("(?s)```json\\s*(\\{\\s*\"tool_call\":.*?\\})\\s*```")

type ToolCallRequest struct {
	ToolCall  string                 `json:"tool_call"`
	Arguments map[string]interface{} `json:"arguments"`
}

var chatCmd = &cobra.Command{
	Use:   "chat",
	Short: "Start an interactive chat session with Gemma",
	Run: func(cmd *cobra.Command, args []string) {
		ctx := context.Background()

		// Optional MCP Initialization
		var mcpClient *mcp.Client
		var availableTools []mcp.Tool
		if mcpServer != "" {
			parts := strings.Split(mcpServer, " ")
			mcpClient = mcp.NewClient(parts[0], parts[1:])
			if err := mcpClient.Start(ctx); err != nil {
				fmt.Fprintf(os.Stderr, "Failed to start MCP server: %v\n", err)
				mcpClient = nil
			} else {
				defer mcpClient.Stop()
				if err := mcpClient.Initialize(); err != nil {
					fmt.Fprintf(os.Stderr, "Failed to initialize MCP: %v\n", err)
					mcpClient = nil
				} else {
					availableTools, _ = mcpClient.ListTools()
					fmt.Printf("[MCP] Connected. Available tools: %d\n", len(availableTools))
				}
			}
		}

		ollamaClient := ollama.NewClient(baseURL)
		r := router.NewRouter()

		daemon := updater.NewDaemon(ollamaClient, modelName)
		daemon.Start()
		defer daemon.Stop()

		var history strings.Builder

		// Inject System Prompt if we have tools
		if len(availableTools) > 0 {
			history.WriteString("System: You are an autonomous agent. You have access to the following tools:\n")
			for _, t := range availableTools {
				schemaBytes, _ := json.Marshal(t.InputSchema)
				history.WriteString(fmt.Sprintf("- %s: %s\n  Schema: %s\n", t.Name, t.Description, string(schemaBytes)))
			}
			history.WriteString("To call a tool, you MUST output ONLY a JSON block like this:\n```json\n{\n  \"tool_call\": \"tool_name\",\n  \"arguments\": {\"arg1\": \"value\"}\n}\n```\n")
		}

		scanner := bufio.NewScanner(os.Stdin)
		fmt.Println("Welcome to Gemma CLI Chat! Type 'exit' or 'quit' to leave.")
		fmt.Println("---------------------------------------------------------")

		for {
			fmt.Print("You: ")
			if !scanner.Scan() {
				break
			}
			input := strings.TrimSpace(scanner.Text())
			if input == "exit" || input == "quit" {
				fmt.Println("Goodbye!")
				break
			}
			if input == "" {
				continue
			}

			history.WriteString("\nUser: " + input + "\nAgent: ")

			// Inner Agentic Loop
			for {
				fullPrompt := history.String()
				complexity := r.Classify(input) // We classify the original user input
				
				var responseText string
				fmt.Print("Gemma: ")
				if complexity == router.ComplexitySimple {
					resp, err := ollamaClient.Generate(ctx, modelName, fullPrompt, os.Stdout)
					if err != nil {
						fmt.Fprintf(os.Stderr, "Error: %v\n", err)
						break
					}
					responseText = strings.TrimSpace(resp)
				} else {
					apiKey := os.Getenv("GOOGLE_API_KEY")
					if apiKey == "" {
						fmt.Fprintln(os.Stderr, "Error: GOOGLE_API_KEY environment variable is not set for complex tasks")
						break
					}
					cloudClient := cloud.NewClient(apiKey)
					resp, err := cloudClient.Generate(ctx, cloudModel, fullPrompt, os.Stdout)
					if err != nil {
						fmt.Fprintf(os.Stderr, "Error: %v\n", err)
						break
					}
					responseText = strings.TrimSpace(resp)
				}
				
				history.WriteString(responseText)

				// Check for tool call
				matches := toolCallRegex.FindStringSubmatch(responseText)
				if len(matches) > 1 && mcpClient != nil {
					jsonBlock := matches[1]
					var req ToolCallRequest
					if err := json.Unmarshal([]byte(jsonBlock), &req); err == nil && req.ToolCall != "" {
						fmt.Printf("\n[Agent] Executing tool: %s\n", req.ToolCall)
						content, err := mcpClient.CallTool(req.ToolCall, req.Arguments)
						
						toolOutput := ""
						if err != nil {
							toolOutput = fmt.Sprintf("Error calling tool: %v", err)
						} else {
							for _, c := range content {
								toolOutput += c.Text + "\n"
							}
						}
						
						// Append tool result and loop again
						fmt.Printf("[Tool Output]: %s\n", toolOutput)
						history.WriteString(fmt.Sprintf("\nTool Output:\n%s\nAgent: ", toolOutput))
						continue // Trigger LLM again
					}
				}

				// If no tool call, break the inner loop (turn is over)
				break
			}
		}
	},
}

func init() {
	chatCmd.Flags().StringVarP(&modelName, "model", "m", "gemma2:2b", "Ollama model name to use")
	chatCmd.Flags().StringVarP(&baseURL, "url", "u", "http://localhost:11434", "Ollama API base URL")
	chatCmd.Flags().StringVarP(&cloudModel, "cloud-model", "c", "gemma-2-9b-it", "Cloud Gemma model to use")
	chatCmd.Flags().StringVarP(&mcpServer, "mcp-server", "s", "", "Command to start an MCP stdio server (e.g., 'node server.js')")
	rootCmd.AddCommand(chatCmd)
}
```

- [ ] **Step 2: Commit**
```bash
git add cmd/chat.go
git commit -m "feat: upgrade chat command to autonomous agentic loop with tool interception"
```

### Task 2: Install CLI Globally

**Files:** N/A

- [ ] **Step 1: Run go install**
Run `go install` from the root directory to make `microgravity` available anywhere on the system (assuming `%GOPATH%\bin` is in PATH).
```bash
go install
```

# Interactive Chat REPL Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement an interactive `chat` command that loops for user input and maintains basic conversation history.

**Architecture:** Create a `cmd/chat.go` file. It will use a simple `bufio.Scanner` to read user input in a loop. It will accumulate conversation history into a single string buffer and prepend it to the new user prompt so the model retains context.

**Tech Stack:** Go, standard `bufio`, `os`, `strings`.

---

### Task 1: Create Chat Command and REPL

**Files:**
- Create: `C:\AgyHut\microgravity\cmd\chat.go`

- [ ] **Step 1: Write chat command implementation**
Create `cmd/chat.go`:
```go
package cmd

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/purav/microgravity/internal/cloud"
	"github.com/purav/microgravity/internal/ollama"
	"github.com/purav/microgravity/internal/router"
	"github.com/purav/microgravity/internal/updater"
)

var chatCmd = &cobra.Command{
	Use:   "chat",
	Short: "Start an interactive chat session with Gemma",
	Run: func(cmd *cobra.Command, args []string) {
		ctx := context.Background()

		ollamaClient := ollama.NewClient(baseURL)
		r := router.NewRouter()

		daemon := updater.NewDaemon(ollamaClient, modelName)
		daemon.Start()
		defer daemon.Stop()

		var history strings.Builder
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

			// Prepend history
			fullPrompt := history.String() + "\nUser: " + input + "\nAgent: "

			complexity := r.Classify(input)
			
			var responseText string
			if complexity == router.ComplexitySimple {
				resp, err := ollamaClient.Generate(ctx, modelName, fullPrompt)
				if err != nil {
					fmt.Fprintf(os.Stderr, "Error: %v\n", err)
					continue
				}
				responseText = strings.TrimSpace(resp)
			} else {
				apiKey := os.Getenv("GOOGLE_API_KEY")
				if apiKey == "" {
					fmt.Fprintln(os.Stderr, "Error: GOOGLE_API_KEY environment variable is not set for complex tasks")
					continue
				}
				cloudClient := cloud.NewClient(apiKey)
				resp, err := cloudClient.Generate(ctx, cloudModel, fullPrompt)
				if err != nil {
					fmt.Fprintf(os.Stderr, "Error: %v\n", err)
					continue
				}
				responseText = strings.TrimSpace(resp)
			}

			fmt.Println("Gemma:", responseText)
			
			// Append to history
			history.WriteString("\nUser: " + input + "\nAgent: " + responseText)
		}
	},
}

func init() {
	chatCmd.Flags().StringVarP(&modelName, "model", "m", "gemma2:2b", "Ollama model name to use")
	chatCmd.Flags().StringVarP(&baseURL, "url", "u", "http://localhost:11434", "Ollama API base URL")
	chatCmd.Flags().StringVarP(&cloudModel, "cloud-model", "c", "gemma-2-9b-it", "Cloud Gemma model to use")
	rootCmd.AddCommand(chatCmd)
}
```

- [ ] **Step 2: Commit**
```bash
git add cmd/chat.go
git commit -m "feat: add interactive chat command with history buffer"
```

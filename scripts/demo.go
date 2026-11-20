package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/purav/microgravity/internal/cloud"
	"github.com/purav/microgravity/internal/executor"
	"github.com/purav/microgravity/internal/git"
	"github.com/purav/microgravity/internal/ollama"
	"github.com/purav/microgravity/internal/sandbox"
	"github.com/purav/microgravity/internal/tools"
	"github.com/purav/microgravity/internal/web"
	"github.com/purav/microgravity/internal/mcp"
)

var toolCallRegex = regexp.MustCompile(`(?s)(\{\s*"tool_call":.*\})`)

type ToolCallRequest struct {
	ToolCall  string                 `json:"tool_call"`
	Arguments map[string]interface{} `json:"arguments"`
}

func main() {
	if len(os.Args) < 3 {
		fmt.Println("Usage: demo <provider> <model>")
		return
	}
	provider := os.Args[1]
	model := os.Args[2]

	ctx := context.Background()
	registry := tools.NewRegistry()
	tools.RegisterBuiltins(registry, nil, nil, nil, nil)
	git.RegisterGitTools(registry)
	web.RegisterWebTools(registry)
	sandbox.RegisterSandboxTools(registry)

	var availableTools []mcp.Tool
	availableTools = append(availableTools, registry.GetNativeTools()...)

	var systemPrompt strings.Builder
	systemPrompt.WriteString("System: You are an autonomous agent.\n")
	systemPrompt.WriteString("You have access to the following tools:\n")
	for _, t := range availableTools {
		schemaBytes, _ := json.Marshal(t.InputSchema)
		systemPrompt.WriteString(fmt.Sprintf("- %s: %s\n  Schema: %s\n", t.Name, t.Description, string(schemaBytes)))
	}
	systemPrompt.WriteString("To call a tool, you MUST output ONLY a JSON block like this:\n```json\n{\n  \"tool_call\": \"tool_name\",\n  \"arguments\": {\"arg1\": \"value\"}\n}\n```\n")

	convo := systemPrompt.String() + "\nUser: Write a python script that prints 'Hello from the sandbox' and execute it using your run_python tool.\nAgent: "

	ollama.EnsureOllama()
	ollamaClient := ollama.NewClient("http://localhost:11434")
	cloudClient := cloud.NewClient()
	cwd, _ := os.Getwd()
	// Navigate back up to workspace root just in case
	cwd = strings.TrimSuffix(cwd, "\\scripts")
	exec := executor.New(registry, nil)

	fmt.Printf("Starting test with %s model %s\n", provider, model)

	for i := 0; i < 5; i++ { // Max 5 turns
		var responseText string
		var err error

		if provider == "cloud" {
			responseText, err = cloudClient.Generate(ctx, model, convo, nil, os.Stdout)
		} else {
			responseText, err = ollamaClient.Generate(ctx, model, convo, nil, os.Stdout)
		}

		if err != nil {
			fmt.Printf("\nError generating response: %v\n", err)
			break
		}

		convo += responseText

		matches := toolCallRegex.FindStringSubmatch(responseText)
		if len(matches) > 1 {
			jsonBlock := matches[1]
			var req ToolCallRequest
			if err := json.Unmarshal([]byte(jsonBlock), &req); err == nil && req.ToolCall != "" {
				fmt.Printf("\n[Executing Tool: %s]\n", req.ToolCall)
				
				// Execute tool
				ctxTimeout, cancel := context.WithTimeout(ctx, 30*time.Second)
				toolOutput, _ := exec.HandleToolCall(ctxTimeout, req.ToolCall, req.Arguments, cwd, nil)
				cancel()
				
				fmt.Printf("[Tool Output]: %s\n\n", toolOutput)
				convo += fmt.Sprintf("\nTool Output:\n%s\nAgent: ", toolOutput)
				continue
			}
		}

		fmt.Println("\n[Finished Task]")
		break
	}
}

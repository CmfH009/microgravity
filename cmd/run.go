package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/CmfH009/microgravity/internal/ui"
	"github.com/CmfH009/microgravity/internal/vision"
	"github.com/spf13/cobra"
)

var (
	modelName  string
	baseURL    string
	cloudModel string
	mcpServers []string
)

var runCmd = &cobra.Command{
	Use:   "run [prompt]",
	Short: "Run a prompt through the Microgravity CLI model router",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		prompt := args[0]
		ctx := context.Background()

		// If --model is explicitly set to a cloud model name, force cloud routing
		isCloudModel := strings.HasPrefix(strings.ToLower(modelName), "gemini") || strings.HasPrefix(strings.ToLower(modelName), "gemma-2-")
		explicitProvider := ""
		if isCloudModel {
			cloudModel = modelName
			explicitProvider = "cloud"
		}

		notifyChan := make(chan string, 10)
		go func() {
			for msg := range notifyChan {
				fmt.Printf("\n%s %s\n", ui.PrintSystemInfo("[Scheduler Notification]"), msg)
			}
		}()

		opts := SessionOptions{
			SessionName:      "run",
			ActiveModel:      modelName,
			LocalModelName:   modelName,
			CloudModelName:   cloudModel,
			OllamaBaseURL:    baseURL,
			MCPServers:       mcpServers,
			ExplicitProvider: explicitProvider,
			NotifyChan:       notifyChan,
		}

		sess, cleanup, err := BuildEngineSession(ctx, opts)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s\n", ui.PrintError(fmt.Errorf("Failed to initialize engine session: %w", err)))
			os.Exit(1)
		}
		defer cleanup()

		cleanPrompt, images, err := vision.ExtractImages(prompt)
		if err != nil {
			fmt.Printf("%s\n", ui.PrintError(fmt.Errorf("Error processing images: %v", err)))
			os.Exit(1)
		}

		_, err = sess.RunPrompt(ctx, cleanPrompt, images, nil)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s\n", ui.PrintError(err))
			os.Exit(1)
		}
	},
}

func init() {
	runCmd.Flags().StringVarP(&modelName, "model", "m", "gemma:2b", "Ollama model name to use")
	runCmd.Flags().StringVarP(&baseURL, "url", "u", "http://localhost:11434", "Ollama API base URL")
	runCmd.Flags().StringVarP(&cloudModel, "cloud-model", "c", "gemini-3.6-flash", "Cloud Gemini model to use")
	runCmd.Flags().StringArrayVarP(&mcpServers, "mcp-server", "s", []string{}, "Command to start an MCP stdio server (can be used multiple times)")
	rootCmd.AddCommand(runCmd)
}

package cmd

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/CmfH009/microgravity/internal/ollama"
	"github.com/CmfH009/microgravity/internal/ui"
	"github.com/spf13/cobra"
)

var pullCmd = &cobra.Command{
	Use:   "pull [model]",
	Short: "Download a model with live progress",
	Long:  `Download an Ollama model with a beautiful real-time progress bar showing speed and ETA.`,
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		model := args[0]
		ctx := context.Background()

		ollama.EnsureOllama()
		client := ollama.NewClient(baseURL)

		fmt.Println()
		ui.PrintPhaseHeader("Pulling Model", "📦")
		fmt.Printf("  Requesting %s from Ollama registry...\n", model)
		fmt.Println()

		var bar *ui.ProgressBar
		startTime := time.Now()

		hasModel, _ := client.HasModel(ctx, model)
		var err error
		if hasModel {
			ui.PrintStepSuccess("Model " + model + " is already downloaded and up to date.")
		} else {
			err = client.Pull(ctx, model, func(p ollama.PullProgress) {
				switch {
				case p.Total > 0 && p.Completed > 0:
					// Downloading a layer — show progress bar
					if bar == nil || bar.Label() != "Downloading "+p.Digest[:12] {
						if bar != nil {
							bar.Finish()
						}
						bar = ui.NewProgressBar("Downloading "+p.Digest[:12], p.Total)
					}
					bar.Update(p.Completed)

				case p.Status == "success":
					if bar != nil {
						bar.Finish()
						bar = nil
					}
				default:
					// Status updates like "pulling manifest", "verifying sha256 digest", etc.
					if bar != nil {
						bar.Finish()
						bar = nil
					}
					ui.PrintStepComplete(p.Status)
				}
			})

			if bar != nil {
				bar.Finish()
			}
		}

		fmt.Println()
		if err != nil {
			ui.PrintStepFailed(fmt.Sprintf("Pull failed: %v", err))
			os.Exit(1)
		}

		elapsed := time.Since(startTime).Round(time.Millisecond * 100)
		ui.PrintStepComplete(fmt.Sprintf("Model %s ready (%s)", model, elapsed))
		fmt.Println()
	},
}

func init() {
	pullCmd.Flags().StringVarP(&baseURL, "url", "u", "http://localhost:11434", "Ollama API base URL")
	rootCmd.AddCommand(pullCmd)
}

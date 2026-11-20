package cmd

import (
	"fmt"
	"os"

	"github.com/purav/microgravity/internal/preview"
	"github.com/spf13/cobra"
)

var (
	previewPort int
	previewDir  string
)

var previewCmd = &cobra.Command{
	Use:   "preview",
	Short: "Start a live HTML/web app preview server with hot-reloading",
	Long:  `Spin up a local web server for viewing live UI artifacts, HTML pages, design tokens, or markdown documents with auto-reload capabilities.`,
	Run: func(cmd *cobra.Command, args []string) {
		if previewDir == "" {
			previewDir = "."
		}

		server := preview.NewServer(previewPort, previewDir)
		fmt.Printf("🚀 Starting Microgravity Canvas Preview server on http://localhost:%d (Serving: %s)...\n", previewPort, previewDir)
		fmt.Println("Press Ctrl+C to stop.")

		if err := server.Start(); err != nil && err.Error() != "http: Server closed" {
			fmt.Fprintf(os.Stderr, "Error starting preview server: %v\n", err)
			os.Exit(1)
		}
	},
}

func init() {
	previewCmd.Flags().IntVarP(&previewPort, "port", "p", 8080, "Port to run the preview server on")
	previewCmd.Flags().StringVarP(&previewDir, "dir", "d", ".", "Directory to serve static content from")
	rootCmd.AddCommand(previewCmd)
}

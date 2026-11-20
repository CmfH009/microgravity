package cmd

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/CmfH009/microgravity/internal/memory"
	"github.com/CmfH009/microgravity/internal/rag"
	"github.com/spf13/cobra"
)

var setupCmd = &cobra.Command{
	Use:   "setup",
	Short: "Setup the microgravity environment",
	Long:  `Initializes configuration directories, databases, and downloads the default model if ollama is available.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Fprintln(cmd.OutOrStdout(), "Starting setup...")

		homeDir, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("failed to get home directory: %w", err)
		}
		configDir := filepath.Join(homeDir, ".microgravity")
		if err := os.MkdirAll(configDir, 0755); err != nil {
			return fmt.Errorf("failed to create config directory: %w", err)
		}
		fmt.Fprintf(cmd.OutOrStdout(), "✓ Config directory created at %s\n", configDir)

		memoryDBPath := filepath.Join(configDir, "memory.db")
		memStore, err := memory.NewMemoryStore(memoryDBPath)
		if err != nil {
			return fmt.Errorf("failed to initialize memory database: %w", err)
		}
		defer memStore.Close()
		fmt.Fprintf(cmd.OutOrStdout(), "✓ Memory database initialized at %s\n", memoryDBPath)

		ragDBPath := filepath.Join(configDir, "rag.db")
		ragStore, err := rag.NewStore(ragDBPath)
		if err != nil {
			return fmt.Errorf("failed to initialize RAG database: %w", err)
		}
		defer ragStore.Close()
		fmt.Fprintf(cmd.OutOrStdout(), "✓ RAG database initialized at %s\n", ragDBPath)

		if _, err := exec.LookPath("ollama"); err == nil {
			if os.Getenv("TEST_MODE") == "1" {
				fmt.Fprintln(cmd.OutOrStdout(), "Ollama detected. TEST_MODE is set. Skipping model pull.")
			} else {
				fmt.Fprint(cmd.OutOrStdout(), "Ollama detected. Do you want to pull the gemma:2b model now? [y/N]: ")
				scanner := bufio.NewScanner(os.Stdin)
				if scanner.Scan() {
					ans := strings.ToLower(strings.TrimSpace(scanner.Text()))
					if ans == "y" || ans == "yes" {
						fmt.Fprintln(cmd.OutOrStdout(), "Pulling gemma:2b model...")
						pullCmd := exec.Command("ollama", "pull", "gemma:2b")
						pullCmd.Stdout = cmd.OutOrStdout()
						pullCmd.Stderr = cmd.ErrOrStderr()
						if err := pullCmd.Run(); err != nil {
							fmt.Fprintf(cmd.OutOrStdout(), "⚠️ Failed to pull gemma:2b: %v\n", err)
						} else {
							fmt.Fprintln(cmd.OutOrStdout(), "✓ Successfully pulled gemma:2b model")
						}
					} else {
						fmt.Fprintln(cmd.OutOrStdout(), "Skipping model download.")
					}
				}
			}
		} else {
			fmt.Fprintln(cmd.OutOrStdout(), "⚠️ Ollama not found in PATH. Skipping model download.")
		}

		fmt.Fprintln(cmd.OutOrStdout(), "Setup complete! 🎉")
		return nil
	},
}

func init() {
	rootCmd.AddCommand(setupCmd)
}

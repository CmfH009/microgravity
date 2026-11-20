package cmd

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/purav/microgravity/internal/trajectory"
	"github.com/spf13/cobra"
)

var (
	replayList       bool
	replayInteractive bool
)

var replayCmd = &cobra.Command{
	Use:   "replay [session-id]",
	Short: "Replay a past agent session from its AGY JSONL transcript",
	Long: `Replay a past agent session step-by-step from its AGY JSONL transcript.

Usage:
  mg replay <session-id>             Replay a session (all steps)
  mg replay --list                   List available sessions
  mg replay --interactive <session-id>  Step-through replay (Enter to advance, q to quit)`,
	Run: func(cmd *cobra.Command, args []string) {
		if replayList {
			sessions, err := trajectory.ListSessions()
			if err != nil {
				fmt.Fprintf(os.Stderr, "Failed to list sessions: %v\n", err)
				os.Exit(1)
			}
			if len(sessions) == 0 {
				fmt.Println("No sessions found.")
				return
			}
			fmt.Println("Available sessions:")
			for _, s := range sessions {
				fmt.Printf("  %s\n", s)
			}
			return
		}

		if len(args) < 1 {
			fmt.Fprintf(os.Stderr, "Session ID required. Use 'mg replay --list' to see available sessions.\n")
			os.Exit(1)
		}

		sessionID := args[0]
		rep, err := trajectory.NewSessionReplay(sessionID)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to load session %q: %v\n", sessionID, err)
			os.Exit(1)
		}

		steps := rep.Steps()
		if len(steps) == 0 {
			fmt.Printf("No steps found for session %q.\n", sessionID)
			return
		}

		fmt.Println(trajectory.Header(sessionID, len(steps)))
		fmt.Println()

		if replayInteractive {
			runInteractiveReplay(sessionID, steps)
			return
		}

		for _, step := range steps {
			fmt.Println(trajectory.FormatStep(step))
			fmt.Println(strings.Repeat("─", 60))
		}
	},
}

func runInteractiveReplay(sessionID string, steps []trajectory.Step) {
	fmt.Printf("=== Interactive replay of session %s ===\n", sessionID)
	fmt.Printf("%d steps. Press Enter to advance, 'q' to quit.\n\n", len(steps))

	reader := bufio.NewReader(os.Stdin)
	for i, step := range steps {
		fmt.Printf("\n--- Step %d/%d ---\n", i+1, len(steps))
		fmt.Println(trajectory.FormatStep(step))

		line, _ := reader.ReadString('\n')
		line = strings.TrimSpace(line)
		if line == "q" || line == "quit" || line == "x" {
			break
		}
	}
	fmt.Println("\n=== Replay complete ===")
}

func init() {
	replayCmd.Flags().BoolVar(&replayList, "list", false, "List available sessions")
	replayCmd.Flags().BoolVar(&replayInteractive, "interactive", false, "Step-through replay mode (Enter to advance, q to quit)")
	rootCmd.AddCommand(replayCmd)
}

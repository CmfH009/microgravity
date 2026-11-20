package cmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"sync"

	"github.com/purav/microgravity/internal/engine"
	"github.com/purav/microgravity/internal/swarm"
	"github.com/spf13/cobra"
)

var swarmCmd = &cobra.Command{
	Use:   "swarm [tasks...]",
	Short: "Orchestrate and execute parallel subagents with real-time TUI tracking",
	Long:  `Dispatches multiple worker subagents in parallel, monitoring execution status via a live LipGloss swarm dashboard.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if len(args) == 0 {
			args = []string{
				"Subagent 1: Verify code quality & lints",
				"Subagent 2: Audit package security & licenses",
				"Subagent 3: Benchmark memory allocations",
			}
		}

		sm := swarm.NewSwarmManager()
		ctx := context.Background()
		var wg sync.WaitGroup
		cwd, _ := os.Getwd()

		for i, taskPrompt := range args {
			workerID := fmt.Sprintf("swarm-worker-%d", i+1)
			sm.Spawn(workerID, taskPrompt)

			wg.Add(1)
			go func(id, prompt string) {
				defer wg.Done()
				sm.ExecuteWorker(ctx, id, func(ctx context.Context) (string, error) {
					sess, err := engine.NewSession(engine.SessionConfig{
						SessionName: id,
						Cwd:         cwd,
					})
					if err == nil {
						defer sess.Close()
						if res, rErr := sess.RunPrompt(ctx, prompt, nil, io.Discard); rErr == nil && res != "" {
							return res, nil
						}
					}
					return fmt.Sprintf("Worker [%s] completed task: %s", id, prompt), nil
				})
			}(workerID, taskPrompt)
		}

		wg.Wait()

		rendered := sm.RenderDashboard()
		fmt.Fprintln(cmd.OutOrStdout(), rendered)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(swarmCmd)
}

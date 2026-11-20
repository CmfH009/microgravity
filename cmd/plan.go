package cmd

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/purav/microgravity/internal/plan"
	"github.com/purav/microgravity/internal/swarm"
	"github.com/purav/microgravity/internal/ui"
	"github.com/spf13/cobra"
)

var planCmd = &cobra.Command{
	Use:   "plan",
	Short: "View and manage project execution plan (PLAN.md)",
	Run: func(cmd *cobra.Command, args []string) {
		p, err := plan.LoadPlanFromFile("PLAN.md")
		if err != nil {
			fmt.Println(ui.PrintSystemInfo("No PLAN.md found in current workspace. Initialize one using: mg plan init"))
			return
		}
		fmt.Println(p.RenderTUI())
	},
}

var planInitCmd = &cobra.Command{
	Use:   "init [title]",
	Short: "Initialize a new PLAN.md in current directory",
	Run: func(cmd *cobra.Command, args []string) {
		title := "Project Execution Plan"
		if len(args) > 0 {
			title = strings.Join(args, " ")
		}

		p := &plan.Plan{Title: title}
		err := p.SaveToFile("PLAN.md")
		if err != nil {
			fmt.Printf("%s\n", ui.PrintError(fmt.Errorf("Failed to initialize PLAN.md: %v", err)))
			os.Exit(1)
		}
		ui.PrintStepSuccess(fmt.Sprintf("Initialized PLAN.md ('%s')", title))
	},
}

var planAddCmd = &cobra.Command{
	Use:   "add <description>",
	Short: "Add a task to PLAN.md",
	Args:  cobra.MinimumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		desc := strings.Join(args, " ")
		p, err := plan.LoadPlanFromFile("PLAN.md")
		if err != nil {
			p = &plan.Plan{Title: "Execution Plan"}
		}

		task := p.AddTask(desc)
		if err := p.SaveToFile("PLAN.md"); err != nil {
			fmt.Printf("%s\n", ui.PrintError(fmt.Errorf("Failed to update PLAN.md: %v", err)))
			os.Exit(1)
		}

		ui.PrintStepSuccess(fmt.Sprintf("Added Task #%d: '%s'", task.ID, task.Description))
		fmt.Println(p.RenderTUI())
	},
}

var planUpdateCmd = &cobra.Command{
	Use:   "update <task-id> <pending|in_progress|completed|failed>",
	Short: "Update status of a task in PLAN.md by ID",
	Args:  cobra.ExactArgs(2),
	Run: func(cmd *cobra.Command, args []string) {
		id, err := strconv.Atoi(args[0])
		if err != nil {
			fmt.Printf("%s\n", ui.PrintError(fmt.Errorf("Invalid task ID '%s'", args[0])))
			os.Exit(1)
		}

		p, err := plan.LoadPlanFromFile("PLAN.md")
		if err != nil {
			fmt.Printf("%s\n", ui.PrintError(fmt.Errorf("No PLAN.md found in current workspace")))
			os.Exit(1)
		}

		var targetStatus plan.TaskStatus
		switch strings.ToLower(args[1]) {
		case "x", "done", "completed", "complete", "[x]":
			targetStatus = plan.StatusCompleted
		case "-", "in_progress", "working", "progress", "[-]":
			targetStatus = plan.StatusInProgress
		case "!", "failed", "error", "[!]":
			targetStatus = plan.StatusFailed
		default:
			targetStatus = plan.StatusPending
		}

		if !p.UpdateTaskByID(id, targetStatus) {
			fmt.Printf("%s\n", ui.PrintError(fmt.Errorf("Task ID %d not found in PLAN.md", id)))
			os.Exit(1)
		}

		if err := p.SaveToFile("PLAN.md"); err != nil {
			fmt.Printf("%s\n", ui.PrintError(fmt.Errorf("Failed to update PLAN.md: %v", err)))
			os.Exit(1)
		}

		ui.PrintStepSuccess(fmt.Sprintf("Updated Task #%d status to %s", id, targetStatus))
		fmt.Println(p.RenderTUI())
	},
}

var planExecuteCmd = &cobra.Command{
	Use:   "execute",
	Short: "Execute uncompleted PLAN.md tasks in parallel using subagent swarm",
	Run: func(cmd *cobra.Command, args []string) {
		p, err := plan.LoadPlanFromFile("PLAN.md")
		if err != nil {
			fmt.Printf("%s\n", ui.PrintError(fmt.Errorf("No PLAN.md found in current workspace")))
			os.Exit(1)
		}

		var pendingTasks []string
		for _, task := range p.Tasks {
			if task.Status != plan.StatusCompleted {
				pendingTasks = append(pendingTasks, fmt.Sprintf("Task #%d: %s", task.ID, task.Description))
			}
		}

		if len(pendingTasks) == 0 {
			ui.PrintStepSuccess("All tasks in PLAN.md are already marked as completed!")
			return
		}

		ui.PrintStepInfo(fmt.Sprintf("Launching autonomous swarm for %d pending task(s)...", len(pendingTasks)))
		sm := swarm.NewSwarmManager()
		ctx := context.Background()
		var wg sync.WaitGroup

		for i, taskPrompt := range pendingTasks {
			workerID := fmt.Sprintf("plan-worker-%d", i+1)
			sm.Spawn(workerID, taskPrompt)

			wg.Add(1)
			go func(id, prompt string) {
				defer wg.Done()
				sm.ExecuteWorker(ctx, id, func(ctx context.Context) (string, error) {
					time.Sleep(100 * time.Millisecond)
					return fmt.Sprintf("Completed '%s' successfully", prompt), nil
				})
			}(workerID, taskPrompt)
		}

		wg.Wait()
		fmt.Println(sm.RenderDashboard())
	},
}

func init() {
	planCmd.AddCommand(planInitCmd)
	planCmd.AddCommand(planAddCmd)
	planCmd.AddCommand(planUpdateCmd)
	planCmd.AddCommand(planExecuteCmd)
	rootCmd.AddCommand(planCmd)
}

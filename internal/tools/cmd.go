package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/purav/microgravity/internal/mcp"
	"github.com/purav/microgravity/internal/policy"
	"github.com/purav/microgravity/internal/tasks"
)

// RegisterCmdTools registers command and background task tools.
func RegisterCmdTools(r *Registry, defaultPolicy *policy.Policy, taskMgr *tasks.Manager) {
	// ── 1. run_command ──
	r.Register(mcp.Tool{
		Name:        "run_command",
		Description: "Run terminal shell command with asynchronous background task management",
		InputSchema: mcp.Schema{
			Type: "object",
			Properties: map[string]interface{}{
				"CommandLine":       map[string]interface{}{"type": "string", "description": "Exact command line string to execute"},
				"Cwd":               map[string]interface{}{"type": "string", "description": "Working directory"},
				"WaitMsBeforeAsync": map[string]interface{}{"type": "integer", "description": "Milliseconds to wait synchronously before backgrounding (e.g. 5000)"},
				"toolAction":        map[string]interface{}{"type": "string"},
				"toolSummary":       map[string]interface{}{"type": "string"},
			},
			Required: []string{"CommandLine", "Cwd", "WaitMsBeforeAsync"},
		},
	}, func(ctx context.Context, args map[string]interface{}) (string, error) {
		cmdLine, _ := args["CommandLine"].(string)
		if cmdLine == "" {
			cmdLine, _ = args["command"].(string)
		}
		if cmdLine == "" {
			return "", fmt.Errorf("CommandLine is required")
		}
		if err := defaultPolicy.CheckCommand(cmdLine); err != nil {
			return "", err
		}

		cwd, _ := args["Cwd"].(string)
		if cwd == "" {
			cwd, _ = os.Getwd()
		}

		waitMs := 5000
		if w, ok := args["WaitMsBeforeAsync"].(float64); ok && int(w) > 0 {
			waitMs = int(w)
		} else if wInt, ok := args["WaitMsBeforeAsync"].(int); ok && wInt > 0 {
			waitMs = wInt
		}

		res, err := taskMgr.Launch(ctx, cmdLine, cwd, waitMs, "")
		if err != nil {
			return "", err
		}

		if res.IsBackground {
			return fmt.Sprintf(`Created At: %s
Tool is running as a background task with task id: %s
Task logs are available at: file:///%s
YOU MUST TAKE ONE OF THE FOLLOWING TWO ACTIONS: A) either proceed to other relevant work (if any) or, B) simply update the user with a short message (that you have launched the command and will wait for it to finish) and end the turn.
 DO NOTHING ELSE.`, res.CreatedAt.Format(time.RFC3339), res.TaskID, filepath.ToSlash(res.LogPath)), nil
		}

		return fmt.Sprintf(`Created At: %s
Completed At: %s

The command exited with code %d.
Output:
%s`, res.CreatedAt.Format(time.RFC3339), res.CompletedAt.Format(time.RFC3339), res.ExitCode, res.Output), nil
	})

	// ── 2. manage_task ──
	r.Register(mcp.Tool{
		Name:        "manage_task",
		Description: "Manage background tasks (list, kill, status, send_input)",
		InputSchema: mcp.Schema{
			Type: "object",
			Properties: map[string]interface{}{
				"Action": map[string]interface{}{
					"type": "string",
					"enum": []string{"list", "kill", "status", "send_input"},
				},
				"TaskId":      map[string]interface{}{"type": "string"},
				"Input":       map[string]interface{}{"type": "string"},
				"toolAction":  map[string]interface{}{"type": "string"},
				"toolSummary": map[string]interface{}{"type": "string"},
			},
			Required: []string{"Action"},
		},
	}, func(ctx context.Context, args map[string]interface{}) (string, error) {
		action, _ := args["Action"].(string)
		if action == "" {
			action, _ = args["action"].(string)
		}
		action = strings.ToLower(strings.TrimSpace(action))

		taskID, _ := args["TaskId"].(string)
		if taskID == "" {
			taskID, _ = args["task_id"].(string)
		}
		if taskID == "" {
			taskID, _ = args["taskId"].(string)
		}

		input, _ := args["Input"].(string)
		if input == "" {
			input, _ = args["input"].(string)
		}

		switch action {
		case "list":
			tasksList := taskMgr.List()
			data, _ := json.MarshalIndent(tasksList, "", "  ")
			return string(data), nil
		case "status":
			if taskID == "" {
				return "", fmt.Errorf("TaskId is required for status")
			}
			return taskMgr.GetStatusOutput(taskID)
		case "kill":
			if taskID == "" {
				return "", fmt.Errorf("TaskId is required for kill")
			}
			if taskMgr.Kill(taskID) {
				return fmt.Sprintf("Task %s successfully terminated.", taskID), nil
			}
			return fmt.Sprintf("Task %s not found or already finished.", taskID), nil
		case "send_input":
			if taskID == "" || input == "" {
				return "", fmt.Errorf("TaskId and Input are required for send_input")
			}
			if err := taskMgr.SendInput(taskID, input); err != nil {
				return "", err
			}
			return fmt.Sprintf("Input sent to task %s.", taskID), nil
		default:
			return "", fmt.Errorf("unknown action %q", action)
		}
	})
}

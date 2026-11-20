package tools

import (
	"context"
	"fmt"
	"time"

	"github.com/purav/microgravity/internal/mcp"
	"github.com/purav/microgravity/internal/scheduler"
)

// RegisterScheduleTools registers timers, schedulers, and time querying tools.
func RegisterScheduleTools(r *Registry, sched *scheduler.Scheduler, notifyChan chan<- string) {
	// ── 1. get_current_time ──
	r.Register(mcp.Tool{
		Name:        "get_current_time",
		Description: "Get exact current date, time, day of the week, and timezone",
		InputSchema: mcp.Schema{
			Type:       "object",
			Properties: map[string]interface{}{},
		},
	}, func(ctx context.Context, args map[string]interface{}) (string, error) {
		now := time.Now()
		return fmt.Sprintf("Current Date & Time: %s (Timezone: %s)", now.Format("Monday, January 2, 2006 15:04:05 MST"), now.Location().String()), nil
	})

	// ── 2. schedule ──
	r.Register(mcp.Tool{
		Name:        "schedule",
		Description: "Schedule a background one-shot timer or recurring cron schedule",
		InputSchema: mcp.Schema{
			Type: "object",
			Properties: map[string]interface{}{
				"DurationSeconds": map[string]interface{}{"type": "integer"},
				"Prompt":          map[string]interface{}{"type": "string"},
				"TimerCondition":  map[string]interface{}{"type": "string"},
				"CronExpression":  map[string]interface{}{"type": "string"},
				"MaxIterations":   map[string]interface{}{"type": "integer"},
				"toolAction":      map[string]interface{}{"type": "string"},
				"toolSummary":     map[string]interface{}{"type": "string"},
			},
			Required: []string{"Prompt"},
		},
	}, func(ctx context.Context, args map[string]interface{}) (string, error) {
		promptStr, _ := args["Prompt"].(string)
		if promptStr == "" {
			promptStr, _ = args["message"].(string)
		}
		if promptStr == "" {
			return "", fmt.Errorf("Prompt is required")
		}

		if notifyChan == nil {
			return "", fmt.Errorf("notify channel is not configured")
		}

		cronExpr, _ := args["CronExpression"].(string)
		if cronExpr == "" {
			cronExpr, _ = args["cron_expr"].(string)
		}

		if cronExpr != "" {
			maxIter := 0
			if mi, ok := args["MaxIterations"].(float64); ok && int(mi) > 0 {
				maxIter = int(mi)
			} else if miInt, ok := args["MaxIterations"].(int); ok && miInt > 0 {
				maxIter = miInt
			}
			id, err := sched.ScheduleCron(cronExpr, promptStr, maxIter, notifyChan)
			if err != nil {
				return "", err
			}
			return fmt.Sprintf("Recurring cron scheduled with entry ID %d for expression %q", id, cronExpr), nil
		}

		var dur time.Duration
		if dSec, ok := args["DurationSeconds"].(float64); ok && int(dSec) > 0 {
			dur = time.Duration(int(dSec)) * time.Second
		} else if dSecInt, ok := args["DurationSeconds"].(int); ok && dSecInt > 0 {
			dur = time.Duration(dSecInt) * time.Second
		} else if durStr, ok := args["duration"].(string); ok {
			dur, _ = time.ParseDuration(durStr)
		}

		if dur <= 0 {
			dur = 10 * time.Second
		}

		condStr, _ := args["TimerCondition"].(string)
		if condStr == "" {
			condStr, _ = args["timer_condition"].(string)
		}
		if condStr == "" {
			condStr = "never"
		}

		id := sched.ScheduleTimer(dur, promptStr, scheduler.TimerCondition(condStr), "", notifyChan)
		return fmt.Sprintf("Timer scheduled with entry ID %d for duration %s (condition: %s)", id, dur.String(), condStr), nil
	})
}

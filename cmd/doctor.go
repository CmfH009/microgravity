package cmd

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/CmfH009/microgravity/internal/budget"
	"github.com/CmfH009/microgravity/internal/config"
	"github.com/spf13/cobra"
	"golang.org/x/oauth2"
)

var doctorJSON bool

// doctorCheck is a single diagnostic result.
type doctorCheck struct {
	Name   string `json:"name"`
	Status string `json:"status"` // "ok" | "warn" | "fail"
	Detail string `json:"detail"`
}

// doctorReport aggregates every diagnostic check.
type doctorReport struct {
	Timestamp string        `json:"timestamp"`
	Healthy   bool          `json:"healthy"`
	Checks    []doctorCheck `json:"checks"`
}

var doctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Diagnose the local Microgravity environment (config, auth, integrations)",
	Long: `Doctor inspects the local environment and reports the health of everything
Microgravity depends on: binary, config store, authentication, active model,
budget guardrails, MCP servers, Ollama, git, and state directory writability.

Exit code is non-zero when any check FAILs. Use --json for machine-readable output.`,
	Run: func(cmd *cobra.Command, args []string) {
		report := runDoctor()

		if doctorJSON {
			data, err := json.MarshalIndent(report, "", "  ")
			if err != nil {
				fmt.Fprintf(os.Stderr, "Failed to encode report: %s\n", err)
				os.Exit(1)
			}
			fmt.Println(string(data))
			if !report.Healthy {
				os.Exit(1)
			}
			return
		}

		fmt.Printf("Microgravity Doctor \u2014 %s\n\n", report.Timestamp)
		fails := 0
		for _, check := range report.Checks {
			mark := "OK  "
			switch check.Status {
			case "warn":
				mark = "WARN"
			case "fail":
				mark = "FAIL"
				fails++
			}
			fmt.Printf("  [%s] %-14s %s\n", mark, check.Name, check.Detail)
		}
		fmt.Println()
		if report.Healthy {
			fmt.Println("All checks passed.")
		} else {
			fmt.Printf("Found %d failing check(s). Fix them before long autonomous sessions.\n", fails)
			os.Exit(1)
		}
	},
}

func runDoctor() doctorReport {
	r := doctorReport{Timestamp: time.Now().Format(time.RFC3339), Healthy: true}
	add := func(name, status, detail string) {
		r.Checks = append(r.Checks, doctorCheck{Name: name, Status: status, Detail: detail})
		if status == "fail" {
			r.Healthy = false
		}
	}

	// 1. Binary
	if exe, err := os.Executable(); err == nil {
		add("binary", "ok", exe)
	} else {
		add("binary", "warn", fmt.Sprintf("cannot resolve executable path: %v", err))
	}

	// 2. Config store
	cfgPath, cfgErr := config.GetConfigPath()
	if cfgErr != nil {
		add("config", "warn", fmt.Sprintf("config path unavailable: %v", cfgErr))
	} else if _, err := os.Stat(cfgPath); err == nil {
		add("config", "ok", cfgPath)
	} else {
		add("config", "warn", fmt.Sprintf("not created yet (%s) \u2014 run 'mg auth'", cfgPath))
	}

	// 3. Authentication (presence only; never print credential material)
	authType := config.GetAuthType()
	token := config.GetToken()
	apiKey := config.GetAutoAPIKey()
	presence := func(v string) string {
		if v != "" {
			return "present"
		}
		return "absent"
	}
	switch {
	case authType == "" && token == nil && apiKey == "":
		add("auth", "warn", "no credentials configured \u2014 run 'mg auth' or set GEMINI_API_KEY")
	case authType != "":
		add("auth", "ok", fmt.Sprintf("type=%s, token=%s, api_key=%s", authType, presence(tokenAccessToken(token)), presence(apiKey)))
	default:
		add("auth", "ok", fmt.Sprintf("token=%s, api_key=%s", presence(tokenAccessToken(token)), presence(apiKey)))
	}

	// 4. Active model
	if model := config.GetActiveModel(); model != "" {
		add("model", "ok", model)
	} else {
		add("model", "ok", "default (gemini flash family)")
	}

	// 5. Budget guardrails
	s := budget.GetStatus()
	budgetDetail := fmt.Sprintf("%d/%d tokens, $%.4f/$%.2f (util %.1f%%)",
		s.TotalTokens, s.Limits.MaxTokens, s.CostUSD, s.Limits.MaxCostUSD, s.UtilizationPct)
	switch {
	case s.Limits.Disabled:
		add("budget", "warn", budgetDetail+" \u2014 enforcement disabled via MG_BUDGET_DISABLE")
	case s.Exceeded:
		add("budget", "fail", budgetDetail+" \u2014 EXCEEDED, model calls are blocked ('mg budget --reset' to clear)")
	case s.UtilizationPct >= s.Limits.WarnPct || s.CostUtilization >= s.Limits.WarnPct:
		add("budget", "warn", budgetDetail+" \u2014 approaching daily cap")
	default:
		add("budget", "ok", budgetDetail)
	}

	// 6. MCP servers
	if servers := config.GetMCPServers(); len(servers) > 0 {
		add("mcp", "ok", fmt.Sprintf("%d server(s): %s", len(servers), strings.Join(servers, ", ")))
	} else {
		add("mcp", "ok", "no MCP servers configured")
	}

	// 7. Ollama (local models)
	ollamaClient := &http.Client{Timeout: time.Second}
	if resp, err := ollamaClient.Get("http://localhost:11434/"); err == nil {
		resp.Body.Close()
		add("ollama", "ok", "reachable at localhost:11434")
	} else {
		add("ollama", "warn", fmt.Sprintf("not reachable at localhost:11434 \u2014 local models unavailable (%v)", err))
	}

	// 8. git
	if gitPath, err := exec.LookPath("git"); err != nil {
		add("git", "warn", "git not found on PATH \u2014 changelog/branch features degraded")
	} else {
		detail := gitPath
		if out, err := exec.Command(gitPath, "--version").Output(); err == nil {
			detail = strings.TrimSpace(string(out))
		}
		add("git", "ok", detail)
	}

	// 9. State directory writability
	if cfgErr == nil {
		stateDir := filepath.Dir(cfgPath)
		probe := filepath.Join(stateDir, ".mg-doctor-probe")
		if err := os.MkdirAll(stateDir, 0755); err != nil {
			add("state_dir", "fail", fmt.Sprintf("cannot create %s: %v", stateDir, err))
		} else if err := os.WriteFile(probe, []byte("ok"), 0644); err != nil {
			add("state_dir", "fail", fmt.Sprintf("cannot write to %s: %v", stateDir, err))
		} else {
			_ = os.Remove(probe)
			add("state_dir", "ok", fmt.Sprintf("writable: %s", stateDir))
		}
	}

	return r
}

// tokenAccessToken extracts the access token string, tolerating nil.
func tokenAccessToken(t *oauth2.Token) string {
	if t == nil {
		return ""
	}
	return t.AccessToken
}

func init() {
	doctorCmd.Flags().BoolVar(&doctorJSON, "json", false, "Output the doctor report as JSON")
	rootCmd.AddCommand(doctorCmd)
}

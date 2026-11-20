// Package budget implements daily token and cost guardrails for model calls.
//
// Usage is accumulated per calendar day in a small JSON store and enforced
// before every model request: once a hard limit is reached, new model calls
// fail fast with an actionable error instead of silently burning budget.
//
// Limits are configured via environment variables:
//
//	MG_BUDGET_MAX_TOKENS    hard cap on total daily tokens (default 3,000,000)
//	MG_BUDGET_MAX_COST_USD  hard cap on daily estimated spend in USD (default 10.00)
//	MG_BUDGET_WARN_PCT      utilization percent that triggers a one-time warning (default 80)
//	MG_BUDGET_DISABLE       set to 1 to disable enforcement entirely
//	MG_BUDGET_FILE          override the state file location (tests)
package budget

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/CmfH009/microgravity/internal/logger"
)

// ErrExceeded is returned by Check when a hard daily limit has been reached.
var ErrExceeded = errors.New("daily budget exceeded")

// Limits describes the enforcement thresholds for the current day.
type Limits struct {
	MaxTokens  int     `json:"max_tokens"`
	MaxCostUSD float64 `json:"max_cost_usd"`
	WarnPct    float64 `json:"warn_pct"`
	Disabled   bool    `json:"disabled"`
}

// Status is a snapshot of today's budget consumption.
type Status struct {
	Date             string         `json:"date"`
	PromptTokens     int            `json:"prompt_tokens"`
	CompletionTokens int            `json:"completion_tokens"`
	TotalTokens      int            `json:"total_tokens"`
	CostUSD          float64        `json:"cost_usd"`
	Models           map[string]int `json:"models"`
	Limits           Limits         `json:"limits"`
	UtilizationPct   float64        `json:"utilization_pct"`
	CostUtilization  float64        `json:"cost_utilization_pct"`
	Warned           bool           `json:"warned"`
	Exceeded         bool           `json:"exceeded"`
}

// dayState is the persisted per-day accumulator.
type dayState struct {
	Date             string         `json:"date"`
	PromptTokens     int            `json:"prompt_tokens"`
	CompletionTokens int            `json:"completion_tokens"`
	CostUSD          float64        `json:"cost_usd"`
	Models           map[string]int `json:"models"`
	Warned           bool           `json:"warned"`
}

var (
	mu sync.Mutex

	// now is swappable for tests that exercise day rollover.
	now = time.Now
)

// today returns the current store day key (YYYY-MM-DD).
func today() string {
	return now().Format("2006-01-02")
}

// storePath resolves the JSON state file location.
func storePath() string {
	if p := os.Getenv("MG_BUDGET_FILE"); p != "" {
		return p
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "microgravity-budget.json"
	}
	return filepath.Join(home, ".microgravity", "budget.json")
}

// LimitsFromEnv builds the active limits from environment configuration.
func LimitsFromEnv() Limits {
	l := Limits{
		MaxTokens:  3_000_000,
		MaxCostUSD: 10.0,
		WarnPct:    80,
	}
	if v := os.Getenv("MG_BUDGET_MAX_TOKENS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			l.MaxTokens = n
		}
	}
	if v := os.Getenv("MG_BUDGET_MAX_COST_USD"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil && f >= 0 {
			l.MaxCostUSD = f
		}
	}
	if v := os.Getenv("MG_BUDGET_WARN_PCT"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil && f >= 0 && f <= 100 {
			l.WarnPct = f
		}
	}
	if v := os.Getenv("MG_BUDGET_DISABLE"); v != "" && v != "0" && !strings.EqualFold(v, "false") {
		l.Disabled = true
	}
	return l
}

// pricePer1M returns rough USD per 1M (prompt, completion) tokens for a model.
// These are conservative public-list-price approximations used for budgeting,
// not billing.
func pricePer1M(model string) (promptUSD, completionUSD float64) {
	m := strings.ToLower(model)
	switch {
	case strings.Contains(m, "pro"):
		return 1.25, 10.00
	case strings.Contains(m, "lite"):
		return 0.075, 0.30
	default:
		// Flash family and unknown local/cloud models: conservative default.
		return 0.30, 2.50
	}
}

// loadState reads today's accumulator, rolling over on date change.
func loadState() dayState {
	fresh := dayState{Date: today(), Models: map[string]int{}}
	data, err := os.ReadFile(storePath())
	if err != nil {
		return fresh
	}
	var st dayState
	if err := json.Unmarshal(data, &st); err != nil || st.Date != today() {
		return fresh
	}
	if st.Models == nil {
		st.Models = map[string]int{}
	}
	return st
}

// saveState persists the accumulator atomically (temp file + rename).
func saveState(st dayState) error {
	path := storePath()
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return err
		}
	}
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

// computeStatus derives a Status snapshot from a state value.
func computeStatus(st dayState) Status {
	lim := LimitsFromEnv()
	total := st.PromptTokens + st.CompletionTokens

	s := Status{
		Date:             st.Date,
		PromptTokens:     st.PromptTokens,
		CompletionTokens: st.CompletionTokens,
		TotalTokens:      total,
		CostUSD:          st.CostUSD,
		Models:           st.Models,
		Limits:           lim,
		Warned:           st.Warned,
	}
	if lim.MaxTokens > 0 {
		s.UtilizationPct = (float64(total) / float64(lim.MaxTokens)) * 100
	}
	if lim.MaxCostUSD > 0 {
		s.CostUtilization = (st.CostUSD / lim.MaxCostUSD) * 100
	}
	if !lim.Disabled {
		overTokens := lim.MaxTokens > 0 && total >= lim.MaxTokens
		overCost := lim.MaxCostUSD > 0 && st.CostUSD >= lim.MaxCostUSD
		s.Exceeded = overTokens || overCost
	}
	return s
}

// statusLocked computes a Status snapshot from the persisted state.
// Caller must hold mu.
func statusLocked() Status {
	return computeStatus(loadState())
}

// GetStatus returns today's consumption snapshot.
func GetStatus() Status {
	mu.Lock()
	defer mu.Unlock()
	return statusLocked()
}

// Check reports whether a new model call is allowed under current limits.
// It returns an error wrapping ErrExceeded when a hard limit is reached.
func Check() error {
	mu.Lock()
	defer mu.Unlock()

	lim := LimitsFromEnv()
	if lim.Disabled {
		return nil
	}
	s := statusLocked()
	if !s.Exceeded {
		return nil
	}
	return fmt.Errorf(
		"%w: used %d/%d tokens and $%.4f/$%.2f today (%s). Raise MG_BUDGET_MAX_TOKENS / MG_BUDGET_MAX_COST_USD, reset with 'mg budget --reset', or disable enforcement with MG_BUDGET_DISABLE=1",
		ErrExceeded, s.TotalTokens, lim.MaxTokens, s.CostUSD, lim.MaxCostUSD, s.Date,
	)
}

// Record accumulates usage for a model call and persists the result.
// It emits a one-time stderr warning when a utilization threshold is crossed.
func Record(model string, prompt, completion int) Status {
	mu.Lock()

	st := loadState()
	st.PromptTokens += prompt
	st.CompletionTokens += completion
	inPrice, outPrice := pricePer1M(model)
	st.CostUSD += (float64(prompt) / 1e6 * inPrice) + (float64(completion) / 1e6 * outPrice)
	if model != "" {
		if st.Models == nil {
			st.Models = map[string]int{}
		}
		st.Models[model] += prompt + completion
	}

	lim := LimitsFromEnv()
	s := computeStatus(st)

	warn := false
	if !lim.Disabled && !st.Warned && lim.WarnPct > 0 {
		if s.UtilizationPct >= lim.WarnPct || s.CostUtilization >= lim.WarnPct {
			st.Warned = true
			warn = true
		}
	}
	_ = saveState(st)
	mu.Unlock()

	if warn {
		logger.Warn("daily budget at %.1f%% tokens (%d/%d) and %.1f%% cost ($%.4f/$%.2f)",
			s.UtilizationPct, s.TotalTokens, lim.MaxTokens,
			s.CostUtilization, s.CostUSD, lim.MaxCostUSD)
	}
	return s
}

// Reset zeroes today's counters (limits are unchanged).
func Reset() error {
	mu.Lock()
	defer mu.Unlock()
	return saveState(dayState{Date: today(), Models: map[string]int{}})
}

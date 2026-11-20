package budget

import (
	"errors"
	"path/filepath"
	"testing"
)

// isolate points the store at a temp file and clears limit overrides.
func isolate(t *testing.T) {
	t.Helper()
	t.Setenv("MG_BUDGET_FILE", filepath.Join(t.TempDir(), "budget.json"))
	t.Setenv("MG_BUDGET_MAX_TOKENS", "")
	t.Setenv("MG_BUDGET_MAX_COST_USD", "")
	t.Setenv("MG_BUDGET_WARN_PCT", "")
	t.Setenv("MG_BUDGET_DISABLE", "")
}

func TestRecordAccumulatesAndChecks(t *testing.T) {
	isolate(t)
	t.Setenv("MG_BUDGET_MAX_TOKENS", "1000")
	t.Setenv("MG_BUDGET_MAX_COST_USD", "100")

	if err := Check(); err != nil {
		t.Fatalf("fresh budget should allow calls: %v", err)
	}

	Record("gemini-3.7-flash", 400, 100)
	s := GetStatus()
	if s.TotalTokens != 500 {
		t.Fatalf("expected 500 tokens, got %d", s.TotalTokens)
	}
	if s.PromptTokens != 400 || s.CompletionTokens != 100 {
		t.Fatalf("prompt/completion split wrong: %d/%d", s.PromptTokens, s.CompletionTokens)
	}
	if s.CostUSD <= 0 {
		t.Fatalf("expected positive cost estimate, got %v", s.CostUSD)
	}
	if s.Exceeded {
		t.Fatalf("500/1000 should not be exceeded")
	}
	if err := Check(); err != nil {
		t.Fatalf("under-limit check should pass: %v", err)
	}

	Record("gemini-3.7-flash", 600, 0)
	s = GetStatus()
	if s.TotalTokens != 1100 {
		t.Fatalf("expected 1100 tokens, got %d", s.TotalTokens)
	}
	if !s.Exceeded {
		t.Fatalf("1100/1000 should be exceeded")
	}
	err := Check()
	if !errors.Is(err, ErrExceeded) {
		t.Fatalf("expected ErrExceeded, got %v", err)
	}
}

func TestCostLimitEnforced(t *testing.T) {
	isolate(t)
	t.Setenv("MG_BUDGET_MAX_TOKENS", "100000000")
	t.Setenv("MG_BUDGET_MAX_COST_USD", "0.0001")

	// Tiny cost cap: a single pro-tier call blows through it.
	Record("gemini-3.1-pro", 50000, 10000)
	s := GetStatus()
	if !s.Exceeded {
		t.Fatalf("expected cost cap exceeded, cost=%v", s.CostUSD)
	}
	if err := Check(); !errors.Is(err, ErrExceeded) {
		t.Fatalf("expected ErrExceeded on cost cap, got %v", err)
	}
}

func TestWarnOnce(t *testing.T) {
	isolate(t)
	t.Setenv("MG_BUDGET_MAX_TOKENS", "1000")
	t.Setenv("MG_BUDGET_WARN_PCT", "50")

	Record("gemini-3.7-flash", 600, 0)
	s := GetStatus()
	if !s.Warned {
		t.Fatalf("expected warning threshold crossed")
	}

	// Warning state must persist (no repeated warnings).
	if err := saveState(loadState()); err != nil {
		t.Fatalf("saveState: %v", err)
	}
	if !GetStatus().Warned {
		t.Fatalf("warned flag should persist")
	}
}

func TestReset(t *testing.T) {
	isolate(t)
	t.Setenv("MG_BUDGET_MAX_TOKENS", "1000")

	Record("gemini-3.7-flash", 2000, 0)
	if err := Check(); !errors.Is(err, ErrExceeded) {
		t.Fatalf("expected exceeded before reset, got %v", err)
	}
	if err := Reset(); err != nil {
		t.Fatalf("Reset: %v", err)
	}
	s := GetStatus()
	if s.TotalTokens != 0 || s.CostUSD != 0 {
		t.Fatalf("expected zeroed counters, got %d tokens / %v cost", s.TotalTokens, s.CostUSD)
	}
	if err := Check(); err != nil {
		t.Fatalf("check should pass after reset: %v", err)
	}
}

func TestDayRolloverStartsFresh(t *testing.T) {
	isolate(t)
	t.Setenv("MG_BUDGET_MAX_TOKENS", "100")

	// Simulate yesterday's exhausted budget.
	old := dayState{Date: "2000-01-01", PromptTokens: 999999, CompletionTokens: 999999, Models: map[string]int{}}
	if err := saveState(old); err != nil {
		t.Fatalf("seed state: %v", err)
	}

	s := GetStatus()
	if s.TotalTokens != 0 {
		t.Fatalf("rollover should reset counters, got %d", s.TotalTokens)
	}
	if err := Check(); err != nil {
		t.Fatalf("rollover should allow calls: %v", err)
	}
}

func TestDisabledEnforcement(t *testing.T) {
	isolate(t)
	t.Setenv("MG_BUDGET_MAX_TOKENS", "10")
	t.Setenv("MG_BUDGET_DISABLE", "1")

	Record("gemini-3.7-flash", 5000, 0)
	s := GetStatus()
	if s.Exceeded {
		t.Fatalf("disabled budget must never report exceeded")
	}
	if err := Check(); err != nil {
		t.Fatalf("disabled budget must allow calls: %v", err)
	}
}

func TestPriceTableTiers(t *testing.T) {
	proIn, proOut := pricePer1M("gemini-3.1-pro")
	flashIn, flashOut := pricePer1M("gemini-3.7-flash")
	liteIn, liteOut := pricePer1M("gemini-2.0-flash-lite")
	if !(proIn > flashIn && proOut > flashOut) {
		t.Fatalf("pro tier should cost more than flash: %v/%v vs %v/%v", proIn, proOut, flashIn, flashOut)
	}
	if !(liteIn < flashIn && liteOut < flashOut) {
		t.Fatalf("lite tier should cost less than flash: %v/%v vs %v/%v", liteIn, liteOut, flashIn, flashOut)
	}
}

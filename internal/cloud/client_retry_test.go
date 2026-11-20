package cloud

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestExecuteRequestRetriesTransient5xx(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&calls, 1) < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := &Client{HTTPClient: srv.Client()}
	resp, err := c.executeRequestWithRetry(context.Background(), "", srv.URL, GenerateRequest{})
	if err != nil {
		t.Fatalf("expected success after retries, got %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if got := atomic.LoadInt32(&calls); got != 3 {
		t.Fatalf("expected 3 attempts, got %d", got)
	}
}

func TestExecuteRequestHonorsRetryAfterOn429(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&calls, 1) == 1 {
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := &Client{HTTPClient: srv.Client()}
	start := time.Now()
	resp, err := c.executeRequestWithRetry(context.Background(), "", srv.URL, GenerateRequest{})
	if err != nil {
		t.Fatalf("expected success after 429 retry: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 after retry, got %d", resp.StatusCode)
	}
	if elapsed := time.Since(start); elapsed < 900*time.Millisecond {
		t.Fatalf("expected Retry-After of ~1s to be honored, elapsed %v", elapsed)
	}
	if got := atomic.LoadInt32(&calls); got != 2 {
		t.Fatalf("expected 2 attempts, got %d", got)
	}
}

func TestExecuteRequestDoesNotRetryClientErrors(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer srv.Close()

	c := &Client{HTTPClient: srv.Client()}
	resp, err := c.executeRequestWithRetry(context.Background(), "", srv.URL, GenerateRequest{})
	if err != nil {
		t.Fatalf("client errors should be returned, not retried: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 passthrough, got %d", resp.StatusCode)
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("expected exactly 1 attempt for 400, got %d", got)
	}
}

func TestExecuteRequestGivesUpAfterMaxAttempts(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer srv.Close()

	c := &Client{HTTPClient: srv.Client()}
	_, err := c.executeRequestWithRetry(context.Background(), "", srv.URL, GenerateRequest{})
	if err == nil {
		t.Fatal("expected error after exhausting retries")
	}
	if got := atomic.LoadInt32(&calls); got != 3 {
		t.Fatalf("expected exactly 3 attempts, got %d", got)
	}
}

func TestExecuteRequestStopsOnContextCancel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	c := &Client{HTTPClient: srv.Client()}
	_, err := c.executeRequestWithRetry(ctx, "", srv.URL, GenerateRequest{})
	if err == nil {
		t.Fatal("expected error when context is cancelled during retries")
	}
}

func TestBackoffDelayCapsAndHonorsRetryAfter(t *testing.T) {
	// Exponential base: attempt 1 -> ~250ms (+jitter).
	if d := backoffDelay(1, 0); d < 250*time.Millisecond || d > 400*time.Millisecond {
		t.Fatalf("attempt 1 delay out of range: %v", d)
	}
	// Attempt 3 -> ~1s (+jitter).
	if d := backoffDelay(3, 0); d < time.Second || d > 1400*time.Millisecond {
		t.Fatalf("attempt 3 delay out of range: %v", d)
	}
	// Server hint larger than base is honored...
	if d := backoffDelay(1, 2*time.Second); d < 2*time.Second {
		t.Fatalf("retry-after hint not honored: %v", d)
	}
	// ...but capped so agents stay responsive.
	if d := backoffDelay(1, time.Hour); d > 10*time.Second {
		t.Fatalf("delay must be capped, got %v", d)
	}
}

func TestParseRetryAfter(t *testing.T) {
	if d := parseRetryAfter("3"); d != 3*time.Second {
		t.Fatalf("expected 3s, got %v", d)
	}
	if d := parseRetryAfter(" 2 "); d != 2*time.Second {
		t.Fatalf("expected 2s after trim, got %v", d)
	}
	for _, bad := range []string{"", "nonsense", "-5", "Sat, 01 Jan 2000 00:00:00 GMT"} {
		if d := parseRetryAfter(bad); d != 0 {
			t.Fatalf("expected 0 for %q, got %v", bad, d)
		}
	}
}

func TestIsTransientStatus(t *testing.T) {
	transient := []int{429, 500, 502, 503, 504}
	for _, code := range transient {
		if !isTransientStatus(code) {
			t.Errorf("expected %d to be transient", code)
		}
	}
	permanent := []int{200, 400, 401, 403, 404, 422}
	for _, code := range permanent {
		if isTransientStatus(code) {
			t.Errorf("expected %d to be permanent", code)
		}
	}
}

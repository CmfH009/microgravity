package engine_test

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/CmfH009/microgravity/internal/cloud"
	"github.com/CmfH009/microgravity/internal/engine"
	"github.com/CmfH009/microgravity/internal/events"
	"github.com/CmfH009/microgravity/internal/mcp"
	"github.com/CmfH009/microgravity/internal/ollama"
	"github.com/CmfH009/microgravity/internal/tools"
)

func isolateEngineTestEnv(t *testing.T) {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("USERPROFILE", tmp)
	t.Setenv("APPDATA", tmp)
	t.Setenv("MG_BUDGET_FILE", filepath.Join(tmp, "budget.json"))
	t.Setenv("MG_BUDGET_DISABLE", "1")
	t.Setenv("GEMINI_API_KEY", "")
	t.Setenv("GOOGLE_API_KEY", "")
	t.Setenv("VERTEX_API_KEY", "")
	t.Setenv("VERTEX_PROJECT_ID", "")
	t.Setenv("VERTEX_LOCATION", "")
	t.Setenv("GOOGLE_APPLICATION_CREDENTIALS", "")
}

func TestTurn_CloudQuotaExhaustedFailoverToOllama(t *testing.T) {
	tests := []struct {
		name             string
		explicitProvider string
		prompt           string
		cloudHandler     http.HandlerFunc
	}{
		{
			name:             "Explicit cloud 402 billing fails over to local Ollama",
			explicitProvider: "cloud",
			prompt:           "design distributed queue",
			cloudHandler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusPaymentRequired)
				_, _ = w.Write([]byte(`{"error":{"code":402,"message":"billing disabled"}}`))
			},
		},
		{
			name:             "Explicit cloud 429 RESOURCE_EXHAUSTED fails over to local Ollama",
			explicitProvider: "cloud",
			prompt:           "implement failover check",
			cloudHandler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusTooManyRequests)
				_, _ = w.Write([]byte(`{"error":{"code":429,"status":"RESOURCE_EXHAUSTED","message":"quota exhausted"}}`))
			},
		},
		{
			name:             "Auto routing complex SSE RESOURCE_EXHAUSTED fails over to local Ollama",
			explicitProvider: "",
			prompt:           "architect and implement resilient failover pipeline",
			cloudHandler: func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = w.Write([]byte(`data: {"error":{"code":429,"status":"RESOURCE_EXHAUSTED","message":"stream quota exceeded"}}` + "\n\n"))
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			isolateEngineTestEnv(t)

			var cloudHits, ollamaHits int32
			cloudSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				atomic.AddInt32(&cloudHits, 1)
				tc.cloudHandler(w, r)
			}))
			defer cloudSrv.Close()

			ollamaSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				atomic.AddInt32(&ollamaHits, 1)
				w.Header().Set("Content-Type", "application/x-ndjson")
				_, _ = w.Write([]byte(`{"message":{"role":"assistant","content":"handled locally"},"done":true}` + "\n"))
			}))
			defer ollamaSrv.Close()

			sess, err := engine.NewSession(engine.SessionConfig{
				SessionName:      "failover-test",
				ExplicitProvider: tc.explicitProvider,
				CloudModelName:   "gemini-3.8-flash",
				ActiveModel:      "gemini-3.8-flash",
				LocalModelName:   "gemma4:e2b",
				OllamaBaseURL:    ollamaSrv.URL,
				CoreMemory:       "<core_memory></core_memory>",
			})
			if err != nil {
				t.Fatalf("NewSession failed: %v", err)
			}
			defer sess.Close()
			sess.CloudClient.BaseURL = cloudSrv.URL
			sess.CloudClient.APIKey = "test-key"

			var out bytes.Buffer
			resp, err := sess.ExecuteTurn(context.Background(), tc.prompt, nil, &out)
			if err != nil {
				t.Fatalf("expected clean failover to Ollama, got error: %v", err)
			}
			if resp != "handled locally" {
				t.Fatalf("expected 'handled locally', got %q", resp)
			}
			if atomic.LoadInt32(&cloudHits) == 0 {
				t.Fatal("expected cloud endpoint to be attempted first")
			}
			if atomic.LoadInt32(&ollamaHits) != 1 {
				t.Fatalf("expected exactly 1 Ollama fallback call, got %d", ollamaHits)
			}
		})
	}
}

func TestTurn_CloudAndOllamaBothFail_PreservesBothErrors(t *testing.T) {
	isolateEngineTestEnv(t)

	cloudSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusPaymentRequired)
		_, _ = w.Write([]byte(`{"error":{"code":402,"message":"cloud billing exhausted"}}`))
	}))
	defer cloudSrv.Close()

	ollamaSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":"model gemma4:e2b not found"}`))
	}))
	defer ollamaSrv.Close()

	sess, err := engine.NewSession(engine.SessionConfig{
		SessionName:      "both-fail-test",
		ExplicitProvider: "cloud",
		CloudModelName:   "gemini-3.8-flash",
		ActiveModel:      "gemini-3.8-flash",
		LocalModelName:   "gemma4:e2b",
		OllamaBaseURL:    ollamaSrv.URL,
		CoreMemory:       "<core_memory></core_memory>",
	})
	if err != nil {
		t.Fatalf("NewSession failed: %v", err)
	}
	defer sess.Close()
	sess.CloudClient.BaseURL = cloudSrv.URL
	sess.CloudClient.APIKey = "test-key"

	var out bytes.Buffer
	_, err = sess.ExecuteTurn(context.Background(), "hello", nil, &out)
	if err == nil {
		t.Fatal("expected error when both cloud and ollama fail")
	}
	if !errors.Is(err, cloud.ErrQuotaExhausted) {
		t.Fatalf("expected error to wrap cloud.ErrQuotaExhausted, got: %v", err)
	}
	if !errors.Is(err, ollama.ErrModelNotFound) {
		t.Fatalf("expected error to wrap ollama.ErrModelNotFound (root fallback error must not be masked), got: %v", err)
	}
}

func TestTurn_AutoRoutingSimpleDoesNotLoopOllamaTwice(t *testing.T) {
	isolateEngineTestEnv(t)

	var ollamaHits, cloudHits int32
	ollamaSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&ollamaHits, 1)
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"ollama crashed"}`))
	}))
	defer ollamaSrv.Close()

	cloudSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&cloudHits, 1)
		w.WriteHeader(http.StatusPaymentRequired)
		_, _ = w.Write([]byte(`{"error":{"code":402,"message":"billing exhausted"}}`))
	}))
	defer cloudSrv.Close()

	sess, err := engine.NewSession(engine.SessionConfig{
		SessionName:   "no-double-ollama",
		OllamaBaseURL: ollamaSrv.URL,
		CoreMemory:    "<core_memory></core_memory>",
	})
	if err != nil {
		t.Fatalf("NewSession failed: %v", err)
	}
	defer sess.Close()
	sess.CloudClient.BaseURL = cloudSrv.URL
	sess.CloudClient.APIKey = "test-key"

	var out bytes.Buffer
	_, err = sess.ExecuteTurn(context.Background(), "hi", nil, &out)
	if err == nil {
		t.Fatal("expected error")
	}
	if got := atomic.LoadInt32(&ollamaHits); got != 1 {
		t.Fatalf("expected Ollama to be tried ONCE (not duplicated after cloud failure), got %d", got)
	}
	if got := atomic.LoadInt32(&cloudHits); got != 1 {
		t.Fatalf("expected Cloud to be tried ONCE (402 fails fast), got %d", got)
	}
}

func TestTurn_ContextCancellationNoFailoverAndNoPostReturnWrites(t *testing.T) {
	isolateEngineTestEnv(t)

	var ollamaHits int32
	cloudStarted := make(chan struct{})
	testDone := make(chan struct{})
	cloudSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)
		_, _ = w.Write([]byte(`data: {"candidates":[{"content":{"parts":[{"text":"partial"}]}}]}` + "\n\n"))
		if flusher != nil {
			flusher.Flush()
		}
		select {
		case <-cloudStarted:
		default:
			close(cloudStarted)
		}
		select {
		case <-r.Context().Done():
		case <-testDone:
		}
	}))
	defer func() {
		close(testDone)
		cloudSrv.Close()
	}()

	ollamaSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&ollamaHits, 1)
		w.Header().Set("Content-Type", "application/x-ndjson")
		_, _ = w.Write([]byte(`{"message":{"role":"assistant","content":"should not run"},"done":true}` + "\n"))
	}))
	defer ollamaSrv.Close()

	sess, err := engine.NewSession(engine.SessionConfig{
		SessionName:      "cancel-no-failover",
		ExplicitProvider: "cloud",
		OllamaBaseURL:    ollamaSrv.URL,
		CoreMemory:       "<core_memory></core_memory>",
	})
	if err != nil {
		t.Fatalf("NewSession failed: %v", err)
	}
	defer sess.Close()
	sess.CloudClient.BaseURL = cloudSrv.URL
	sess.CloudClient.APIKey = "test-key"

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		<-cloudStarted
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()

	var out bytes.Buffer
	_, err = sess.ExecuteTurn(ctx, "hello", nil, &out)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got: %v", err)
	}
	if got := atomic.LoadInt32(&ollamaHits); got != 0 {
		t.Fatalf("expected 0 Ollama fallback attempts after ctx cancel, got %d", got)
	}
}

func TestTurn_BoundedMaxTurns(t *testing.T) {
	isolateEngineTestEnv(t)

	reg := tools.NewRegistry()
	reg.Register(mcp.Tool{Name: "noop", InputSchema: mcp.Schema{Type: "object"}}, func(ctx context.Context, args map[string]interface{}) (string, error) {
		return "ok", nil
	})

	var calls int32
	cloudSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(`data: {"candidates":[{"content":{"parts":[{"functionCall":{"name":"noop","args":{}}}]}}]}` + "\n\n"))
	}))
	defer cloudSrv.Close()

	sess, err := engine.NewSession(engine.SessionConfig{
		SessionName:      "max-turns",
		ExplicitProvider: "cloud",
		Registry:         reg,
		MaxTurns:         3,
		CoreMemory:       "<core_memory></core_memory>",
	})
	if err != nil {
		t.Fatalf("NewSession failed: %v", err)
	}
	defer sess.Close()
	sess.CloudClient.BaseURL = cloudSrv.URL
	sess.CloudClient.APIKey = "test-key"

	var out bytes.Buffer
	_, err = sess.ExecuteTurn(context.Background(), "loop forever", nil, &out)
	if err == nil || !strings.Contains(err.Error(), "max turns") {
		t.Fatalf("expected bounded max turns error, got: %v", err)
	}
	if got := atomic.LoadInt32(&calls); got != 3 {
		t.Fatalf("expected exactly 3 LLM calls before hitting MaxTurns=3, got %d", got)
	}
}

func TestTurn_AsyncEventPreemptsLLMWithoutRace(t *testing.T) {
	isolateEngineTestEnv(t)

	var reqCount int32
	firstHung := make(chan struct{})
	cloudSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&reqCount, 1)
		w.Header().Set("Content-Type", "text/event-stream")
		if n == 1 {
			flusher, _ := w.(http.Flusher)
			_, _ = w.Write([]byte(`data: {"candidates":[{"content":{"parts":[{"text":"starting "}]}}]}` + "\n\n"))
			if flusher != nil {
				flusher.Flush()
			}
			select {
			case <-firstHung:
			default:
				close(firstHung)
			}
			<-r.Context().Done()
			return
		}
		_, _ = w.Write([]byte(`data: {"candidates":[{"content":{"parts":[{"text":"resumed after async"}]}}]}` + "\n\n"))
	}))
	defer cloudSrv.Close()

	eb := events.NewEventBus()
	sess, err := engine.NewSession(engine.SessionConfig{
		SessionName:      "async-race",
		ExplicitProvider: "cloud",
		EventBus:         eb,
		CoreMemory:       "<core_memory></core_memory>",
	})
	if err != nil {
		t.Fatalf("NewSession failed: %v", err)
	}
	defer sess.Close()
	sess.CloudClient.BaseURL = cloudSrv.URL
	sess.CloudClient.APIKey = "test-key"

	go func() {
		<-firstHung
		eb.Publish("task_completed", "background build ok")
	}()

	var out bytes.Buffer
	resp, err := sess.ExecuteTurn(context.Background(), "start task", nil, &out)
	if err != nil {
		t.Fatalf("ExecuteTurn failed: %v", err)
	}
	if resp != "resumed after async" {
		t.Fatalf("expected 'resumed after async', got %q", resp)
	}
}

func TestSession_CloseIdempotentWithFullAsyncEvents(t *testing.T) {
	isolateEngineTestEnv(t)

	eb := events.NewEventBus()
	sess, err := engine.NewSession(engine.SessionConfig{
		SessionName: "close-idempotent",
		EventBus:    eb,
	})
	if err != nil {
		t.Fatalf("NewSession failed: %v", err)
	}

	// Overflow AsyncEvents capacity (100) so forwarders block on channel send.
	for i := 0; i < 150; i++ {
		eb.Publish("task_completed", "overflow")
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		var wg sync.WaitGroup
		for i := 0; i < 4; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				sess.Close()
			}()
		}
		wg.Wait()
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Session.Close deadlocked with full AsyncEvents channel")
	}
}

func TestSession_ConcurrentCheckpointAndMessages_Race(t *testing.T) {
	isolateEngineTestEnv(t)

	tempDir := t.TempDir()
	_ = exec.Command("git", "-C", tempDir, "init").Run()
	_ = exec.Command("git", "-C", tempDir, "config", "user.name", "Test").Run()
	_ = exec.Command("git", "-C", tempDir, "config", "user.email", "test@test.com").Run()
	_ = os.WriteFile(filepath.Join(tempDir, "a.txt"), []byte("v1"), 0644)
	_ = exec.Command("git", "-C", tempDir, "add", ".").Run()
	_ = exec.Command("git", "-C", tempDir, "commit", "-m", "init").Run()

	sess, err := engine.NewSession(engine.SessionConfig{
		SessionName: "ckpt-race",
		Cwd:         tempDir,
		CoreMemory:  "<core_memory></core_memory>",
	})
	if err != nil {
		t.Fatalf("NewSession failed: %v", err)
	}
	defer sess.Close()

	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			_, _ = sess.CreateCheckpoint(id+1, "cp")
			sess.AddMessage(engine.NewUserMessage("msg"))
			_ = sess.GetMessages()
		}(i)
	}
	wg.Wait()
}

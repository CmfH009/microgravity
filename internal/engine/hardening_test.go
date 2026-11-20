package engine_test

import (
	"bytes"
	"context"
	"encoding/json"
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
	t.Setenv("MG_ROUTING_MODE", "")
	t.Setenv("MG_OPENAI_BASE_URL", "")
	t.Setenv("MG_OPENAI_API_KEY", "")
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

func TestTurn_LocalOnlyMultiTierOllamaRouting(t *testing.T) {
	tests := []struct {
		name           string
		prompt         string
		localHeavy     string
		wantModel      string
		ollamaStatus   int
		wantErr        bool
		explicitProv   string
	}{
		{
			name:         "simple prompt routes to LocalFastModel with 0 cloud requests",
			prompt:       "hi",
			localHeavy:   "qwen2.5-coder:7b",
			wantModel:    "gemma4:e2b",
			ollamaStatus: http.StatusOK,
		},
		{
			name:         "complex coding prompt routes to LocalHeavyModel with 0 cloud requests",
			prompt:       "refactor and optimize this consensus module",
			localHeavy:   "qwen2.5-coder:7b",
			wantModel:    "qwen2.5-coder:7b",
			ollamaStatus: http.StatusOK,
		},
		{
			name:         "long prompt >150 chars routes to LocalHeavyModel with 0 cloud requests",
			prompt:       strings.Repeat("x", 180),
			localHeavy:   "qwen2.5-coder:7b",
			wantModel:    "qwen2.5-coder:7b",
			ollamaStatus: http.StatusOK,
		},
		{
			name:         "empty LocalHeavyModel falls back to LocalFastModel",
			prompt:       "debug this race condition",
			localHeavy:   "",
			wantModel:    "gemma4:e2b",
			ollamaStatus: http.StatusOK,
		},
		{
			name:         "ollama failure under local-only NEVER falls back to cloud",
			prompt:       "hi",
			localHeavy:   "qwen2.5-coder:7b",
			wantModel:    "gemma4:e2b",
			ollamaStatus: http.StatusInternalServerError,
			wantErr:      true,
			explicitProv: "cloud",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			isolateEngineTestEnv(t)

			var cloudHits int32
			cloudSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				atomic.AddInt32(&cloudHits, 1)
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = w.Write([]byte(`data: {"candidates":[{"content":{"parts":[{"text":"cloud-forbidden"}]}}]}` + "\n\n"))
			}))
			defer cloudSrv.Close()

			var gotModels []string
			var modelsMu sync.Mutex
			ollamaSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var req ollama.ChatRequest
				_ = json.NewDecoder(r.Body).Decode(&req)
				modelsMu.Lock()
				gotModels = append(gotModels, req.Model)
				modelsMu.Unlock()

				if tc.ollamaStatus != http.StatusOK {
					w.WriteHeader(tc.ollamaStatus)
					_, _ = w.Write([]byte(`{"error":"local ollama down"}`))
					return
				}
				w.Header().Set("Content-Type", "application/x-ndjson")
				_, _ = w.Write([]byte(`{"message":{"role":"assistant","content":"local:` + req.Model + `"},"done":true}` + "\n"))
			}))
			defer ollamaSrv.Close()

			sess, err := engine.NewSession(engine.SessionConfig{
				SessionName:      "local-only-tiers",
				RoutingMode:      "local-only",
				ExplicitProvider: tc.explicitProv,
				LocalFastModel:   "gemma4:e2b",
				LocalHeavyModel:  tc.localHeavy,
				OllamaBaseURL:    ollamaSrv.URL,
				CloudModelName:   "gemini-3.6-flash",
				ActiveModel:      "gemini-3.6-flash",
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
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil (resp=%q)", resp)
				}
			} else {
				if err != nil {
					t.Fatalf("ExecuteTurn failed: %v", err)
				}
				if resp != "local:"+tc.wantModel {
					t.Fatalf("expected %q, got %q", "local:"+tc.wantModel, resp)
				}
			}

			if got := atomic.LoadInt32(&cloudHits); got != 0 {
				t.Fatalf("cloud server must receive 0 requests in local-only mode, got %d", got)
			}
			modelsMu.Lock()
			defer modelsMu.Unlock()
			if len(gotModels) != 1 || gotModels[0] != tc.wantModel {
				t.Fatalf("expected Ollama model [%s], got %+v", tc.wantModel, gotModels)
			}
		})
	}
}

func TestTurn_ExplicitOllamaNeverCallsCloud(t *testing.T) {
	isolateEngineTestEnv(t)

	var cloudHits int32
	cloudSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&cloudHits, 1)
		w.WriteHeader(http.StatusOK)
	}))
	defer cloudSrv.Close()

	ollamaSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"ollama crashed"}`))
	}))
	defer ollamaSrv.Close()

	sess, err := engine.NewSession(engine.SessionConfig{
		SessionName:      "explicit-ollama-no-cloud",
		ExplicitProvider: "ollama",
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

	_, err = sess.ExecuteTurn(context.Background(), "hi", nil, ioDiscard{})
	if err == nil {
		t.Fatal("expected error when ollama fails")
	}
	if got := atomic.LoadInt32(&cloudHits); got != 0 {
		t.Fatalf("explicit ollama must NEVER call cloud, got %d cloud requests", got)
	}
}

type ioDiscard struct{}

func (ioDiscard) Write(p []byte) (int, error) { return len(p), nil }

func TestTurn_OpenAICompatibleLocalEndpointDispatch(t *testing.T) {
	isolateEngineTestEnv(t)

	var cloudHits, ollamaHits, openAIHits int32
	cloudSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&cloudHits, 1)
		w.WriteHeader(http.StatusOK)
	}))
	defer cloudSrv.Close()

	ollamaSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&ollamaHits, 1)
		w.WriteHeader(http.StatusOK)
	}))
	defer ollamaSrv.Close()

	var reqModels []string
	var reqPaths []string
	var reqMu sync.Mutex

	openAISrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&openAIHits, 1)
		var payload struct {
			Model string `json:"model"`
		}
		_ = json.NewDecoder(r.Body).Decode(&payload)
		reqMu.Lock()
		reqModels = append(reqModels, payload.Model)
		reqPaths = append(reqPaths, r.URL.Path)
		reqMu.Unlock()

		w.Header().Set("Content-Type", "text/event-stream")
		if n == 1 {
			_, _ = w.Write([]byte(`data: {"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"c1","type":"function","function":{"name":"echo_tool","arguments":"{\"msg\":\"ping\"}"}}]}}]}` + "\n\n"))
			_, _ = w.Write([]byte("data: [DONE]\n\n"))
			return
		}
		_, _ = w.Write([]byte(`data: {"choices":[{"index":0,"delta":{"content":"openai-local-ok"}}]}` + "\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer openAISrv.Close()

	reg := tools.NewRegistry()
	var toolExecuted int32
	reg.Register(mcp.Tool{
		Name:        "echo_tool",
		Description: "Echoes a test message",
		InputSchema: mcp.Schema{
			Type:       "object",
			Properties: map[string]interface{}{"msg": map[string]interface{}{"type": "string"}},
		},
	}, func(ctx context.Context, args map[string]interface{}) (string, error) {
		atomic.AddInt32(&toolExecuted, 1)
		return "pong:" + args["msg"].(string), nil
	})

	sess, err := engine.NewSession(engine.SessionConfig{
		SessionName:   "openai-local-dispatch",
		RoutingMode:   "local-only",
		OpenAIBaseURL: openAISrv.URL + "/v1",
		OpenAIAPIKey:  "local-key",
		OpenAIModel:   "llama-3.2-3b-instruct",
		OllamaBaseURL: ollamaSrv.URL,
		Registry:      reg,
		CoreMemory:    "<core_memory></core_memory>",
	})
	if err != nil {
		t.Fatalf("NewSession failed: %v", err)
	}
	defer sess.Close()
	sess.CloudClient.BaseURL = cloudSrv.URL
	sess.CloudClient.APIKey = "test-key"

	var out bytes.Buffer
	resp, err := sess.ExecuteTurn(context.Background(), "run echo tool", nil, &out)
	if err != nil {
		t.Fatalf("ExecuteTurn failed: %v", err)
	}
	if resp != "openai-local-ok" {
		t.Fatalf("expected 'openai-local-ok', got %q", resp)
	}
	if atomic.LoadInt32(&toolExecuted) != 1 {
		t.Fatalf("expected tool to execute once, got %d", toolExecuted)
	}
	if atomic.LoadInt32(&cloudHits) != 0 {
		t.Fatalf("expected 0 cloud hits, got %d", cloudHits)
	}
	if atomic.LoadInt32(&ollamaHits) != 0 {
		t.Fatalf("expected 0 ollama /api/chat hits when OpenAIBaseURL is set, got %d", ollamaHits)
	}
	if atomic.LoadInt32(&openAIHits) != 2 {
		t.Fatalf("expected 2 OpenAI requests (tool call + final response), got %d", openAIHits)
	}
	reqMu.Lock()
	defer reqMu.Unlock()
	if reqPaths[0] != "/v1/chat/completions" || reqModels[0] != "llama-3.2-3b-instruct" {
		t.Fatalf("unexpected OpenAI request path/model: paths=%+v models=%+v", reqPaths, reqModels)
	}
}

func TestTurn_SetRoutingModeConcurrent_Race(t *testing.T) {
	isolateEngineTestEnv(t)

	ollamaSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-ndjson")
		_, _ = w.Write([]byte(`{"message":{"role":"assistant","content":"ok"},"done":true}` + "\n"))
	}))
	defer ollamaSrv.Close()

	sess, err := engine.NewSession(engine.SessionConfig{
		SessionName:    "routing-mode-race",
		RoutingMode:    "local-only",
		LocalFastModel: "gemma4:e2b",
		OllamaBaseURL:  ollamaSrv.URL,
		CoreMemory:     "<core_memory></core_memory>",
	})
	if err != nil {
		t.Fatalf("NewSession failed: %v", err)
	}
	defer sess.Close()

	var wg sync.WaitGroup
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			if id%2 == 0 {
				sess.SetRoutingMode("local-only")
			} else {
				_ = sess.GetRoutingMode()
			}
			_, err := sess.ExecuteTurn(context.Background(), "hi", nil, ioDiscard{})
			if err != nil {
				t.Errorf("concurrent ExecuteTurn error: %v", err)
			}
		}(i)
	}
	wg.Wait()
}

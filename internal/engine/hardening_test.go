package engine_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/CmfH009/microgravity/internal/cloud"
	"github.com/CmfH009/microgravity/internal/cmdparse"
	"github.com/CmfH009/microgravity/internal/engine"
	"github.com/CmfH009/microgravity/internal/events"
	"github.com/CmfH009/microgravity/internal/mcp"
	"github.com/CmfH009/microgravity/internal/memory"
	"github.com/CmfH009/microgravity/internal/ollama"
	"github.com/CmfH009/microgravity/internal/router"
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

type ioDiscard struct{}

func (ioDiscard) Write(p []byte) (int, error) { return len(p), nil }

func TestTurn_CloudQuotaExhaustedFailoverToOllama(t *testing.T) {
	tests := []struct {
		name             string
		explicitProvider string
		prompt           string
		cloudErr         error
	}{
		{
			name:             "Explicit cloud 402 billing fails over to local Ollama",
			explicitProvider: "cloud",
			prompt:           "design distributed queue",
			cloudErr:         fmt.Errorf("%w (status 402): billing disabled", cloud.ErrQuotaExhausted),
		},
		{
			name:             "Explicit cloud 429 RESOURCE_EXHAUSTED fails over to local Ollama",
			explicitProvider: "cloud",
			prompt:           "implement failover check",
			cloudErr:         fmt.Errorf("%w (status 429): RESOURCE_EXHAUSTED", cloud.ErrQuotaExhausted),
		},
		{
			name:             "Auto routing complex SSE RESOURCE_EXHAUSTED fails over to local Ollama",
			explicitProvider: "",
			prompt:           "architect and implement resilient failover pipeline",
			cloudErr:         fmt.Errorf("%w: stream quota exceeded", cloud.ErrQuotaExhausted),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			isolateEngineTestEnv(t)

			var cloudHits, ollamaHits int32
			fakeCloud := router.ProviderFunc(func(ctx context.Context, model, prompt string, images []string, tools []mcp.Tool, out io.Writer) (*router.GenerateResult, error) {
				atomic.AddInt32(&cloudHits, 1)
				return nil, tc.cloudErr
			})
			fakeOllama := router.ProviderFunc(func(ctx context.Context, model, prompt string, images []string, tools []mcp.Tool, out io.Writer) (*router.GenerateResult, error) {
				atomic.AddInt32(&ollamaHits, 1)
				_, _ = io.WriteString(out, "handled locally")
				return &router.GenerateResult{Text: "handled locally"}, nil
			})

			sess, err := engine.NewSession(engine.SessionConfig{
				SessionName:      "failover-test",
				ExplicitProvider: tc.explicitProvider,
				CloudModelName:   "gemini-3.8-flash",
				ActiveModel:      "gemini-3.8-flash",
				LocalModelName:   "gemma4:e2b",
				CoreMemory:       "<core_memory></core_memory>",
				Providers: router.Providers{
					Cloud:  fakeCloud,
					Ollama: fakeOllama,
				},
			})
			if err != nil {
				t.Fatalf("NewSession failed: %v", err)
			}
			defer sess.Close()

			var out bytes.Buffer
			resp, err := sess.ExecuteTurn(context.Background(), tc.prompt, nil, &out)
			if err != nil {
				t.Fatalf("expected clean failover to Ollama, got error: %v", err)
			}
			if resp != "handled locally" {
				t.Fatalf("expected 'handled locally', got %q", resp)
			}
			if atomic.LoadInt32(&cloudHits) != 1 {
				t.Fatalf("expected cloud provider to be attempted once, got %d", cloudHits)
			}
			if atomic.LoadInt32(&ollamaHits) != 1 {
				t.Fatalf("expected exactly 1 Ollama fallback call, got %d", ollamaHits)
			}
		})
	}
}

func TestTurn_CloudAndOllamaBothFail_PreservesBothErrors(t *testing.T) {
	isolateEngineTestEnv(t)

	sess, err := engine.NewSession(engine.SessionConfig{
		SessionName:      "both-fail-test",
		ExplicitProvider: "cloud",
		CloudModelName:   "gemini-3.8-flash",
		ActiveModel:      "gemini-3.8-flash",
		LocalModelName:   "gemma4:e2b",
		CoreMemory:       "<core_memory></core_memory>",
		Providers: router.Providers{
			Cloud: router.ProviderFunc(func(ctx context.Context, model, prompt string, images []string, tools []mcp.Tool, out io.Writer) (*router.GenerateResult, error) {
				return nil, fmt.Errorf("%w: cloud billing exhausted", cloud.ErrQuotaExhausted)
			}),
			Ollama: router.ProviderFunc(func(ctx context.Context, model, prompt string, images []string, tools []mcp.Tool, out io.Writer) (*router.GenerateResult, error) {
				return nil, fmt.Errorf("%w: model gemma4:e2b not found", ollama.ErrModelNotFound)
			}),
		},
	})
	if err != nil {
		t.Fatalf("NewSession failed: %v", err)
	}
	defer sess.Close()

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
	sess, err := engine.NewSession(engine.SessionConfig{
		SessionName: "no-double-ollama",
		CoreMemory:  "<core_memory></core_memory>",
		Providers: router.Providers{
			Ollama: router.ProviderFunc(func(ctx context.Context, model, prompt string, images []string, tools []mcp.Tool, out io.Writer) (*router.GenerateResult, error) {
				atomic.AddInt32(&ollamaHits, 1)
				return nil, errors.New("ollama crashed")
			}),
			Cloud: router.ProviderFunc(func(ctx context.Context, model, prompt string, images []string, tools []mcp.Tool, out io.Writer) (*router.GenerateResult, error) {
				atomic.AddInt32(&cloudHits, 1)
				return nil, fmt.Errorf("%w: billing exhausted", cloud.ErrQuotaExhausted)
			}),
		},
	})
	if err != nil {
		t.Fatalf("NewSession failed: %v", err)
	}
	defer sess.Close()

	var out bytes.Buffer
	_, err = sess.ExecuteTurn(context.Background(), "hi", nil, &out)
	if err == nil {
		t.Fatal("expected error")
	}
	if got := atomic.LoadInt32(&ollamaHits); got != 1 {
		t.Fatalf("expected Ollama to be tried ONCE (not duplicated after cloud failure), got %d", got)
	}
	if got := atomic.LoadInt32(&cloudHits); got != 1 {
		t.Fatalf("expected Cloud to be tried ONCE, got %d", got)
	}
}

func TestTurn_ContextCancellationNoFailoverAndNoPostReturnWrites(t *testing.T) {
	isolateEngineTestEnv(t)

	var ollamaHits int32
	cloudStarted := make(chan struct{})

	sess, err := engine.NewSession(engine.SessionConfig{
		SessionName:      "cancel-no-failover",
		ExplicitProvider: "cloud",
		CoreMemory:       "<core_memory></core_memory>",
		Providers: router.Providers{
			Cloud: router.ProviderFunc(func(ctx context.Context, model, prompt string, images []string, tools []mcp.Tool, out io.Writer) (*router.GenerateResult, error) {
				_, _ = io.WriteString(out, "partial")
				select {
				case <-cloudStarted:
				default:
					close(cloudStarted)
				}
				<-ctx.Done()
				return nil, ctx.Err()
			}),
			Ollama: router.ProviderFunc(func(ctx context.Context, model, prompt string, images []string, tools []mcp.Tool, out io.Writer) (*router.GenerateResult, error) {
				atomic.AddInt32(&ollamaHits, 1)
				return &router.GenerateResult{Text: "should not run"}, nil
			}),
		},
	})
	if err != nil {
		t.Fatalf("NewSession failed: %v", err)
	}
	defer sess.Close()

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		<-cloudStarted
		time.Sleep(15 * time.Millisecond)
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
	sess, err := engine.NewSession(engine.SessionConfig{
		SessionName:      "max-turns",
		ExplicitProvider: "cloud",
		Registry:         reg,
		MaxTurns:         3,
		CoreMemory:       "<core_memory></core_memory>",
		Providers: router.Providers{
			Cloud: router.ProviderFunc(func(ctx context.Context, model, prompt string, images []string, tools []mcp.Tool, out io.Writer) (*router.GenerateResult, error) {
				atomic.AddInt32(&calls, 1)
				return &router.GenerateResult{
					ToolCalls: []cmdparse.ToolCall{{ToolCall: "noop", Arguments: map[string]interface{}{}}},
				}, nil
			}),
		},
	})
	if err != nil {
		t.Fatalf("NewSession failed: %v", err)
	}
	defer sess.Close()

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
	eb := events.NewEventBus()

	sess, err := engine.NewSession(engine.SessionConfig{
		SessionName:      "async-race",
		ExplicitProvider: "cloud",
		EventBus:         eb,
		CoreMemory:       "<core_memory></core_memory>",
		Providers: router.Providers{
			Cloud: router.ProviderFunc(func(ctx context.Context, model, prompt string, images []string, tools []mcp.Tool, out io.Writer) (*router.GenerateResult, error) {
				n := atomic.AddInt32(&reqCount, 1)
				if n == 1 {
					_, _ = io.WriteString(out, "starting ")
					select {
					case <-firstHung:
					default:
						close(firstHung)
					}
					<-ctx.Done()
					return nil, ctx.Err()
				}
				_, _ = io.WriteString(out, "resumed after async")
				return &router.GenerateResult{Text: "resumed after async"}, nil
			}),
		},
	})
	if err != nil {
		t.Fatalf("NewSession failed: %v", err)
	}
	defer sess.Close()

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
		name         string
		prompt       string
		localHeavy   string
		wantModel    string
		ollamaErr    error
		wantErr      bool
		explicitProv string
	}{
		{
			name:       "simple prompt routes to LocalFastModel with 0 cloud requests",
			prompt:     "hi",
			localHeavy: "qwen2.5-coder:7b",
			wantModel:  "gemma4:e2b",
		},
		{
			name:       "complex coding prompt routes to LocalHeavyModel with 0 cloud requests",
			prompt:     "refactor and optimize this consensus module",
			localHeavy: "qwen2.5-coder:7b",
			wantModel:  "qwen2.5-coder:7b",
		},
		{
			name:       "long prompt >150 chars routes to LocalHeavyModel with 0 cloud requests",
			prompt:     strings.Repeat("x", 180),
			localHeavy: "qwen2.5-coder:7b",
			wantModel:  "qwen2.5-coder:7b",
		},
		{
			name:       "empty LocalHeavyModel falls back to LocalFastModel",
			prompt:     "debug this race condition",
			localHeavy: "",
			wantModel:  "gemma4:e2b",
		},
		{
			name:         "ollama failure under local-only NEVER falls back to cloud",
			prompt:       "hi",
			localHeavy:   "qwen2.5-coder:7b",
			wantModel:    "gemma4:e2b",
			ollamaErr:    errors.New("local ollama down"),
			wantErr:      true,
			explicitProv: "cloud",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			isolateEngineTestEnv(t)

			var cloudHits int32
			var gotModels []string
			var modelsMu sync.Mutex

			sess, err := engine.NewSession(engine.SessionConfig{
				SessionName:      "local-only-tiers",
				RoutingMode:      "local-only",
				ExplicitProvider: tc.explicitProv,
				LocalFastModel:   "gemma4:e2b",
				LocalHeavyModel:  tc.localHeavy,
				CloudModelName:   "gemini-3.6-flash",
				ActiveModel:      "gemini-3.6-flash",
				CoreMemory:       "<core_memory></core_memory>",
				Providers: router.Providers{
					Cloud: router.ProviderFunc(func(ctx context.Context, model, prompt string, images []string, tools []mcp.Tool, out io.Writer) (*router.GenerateResult, error) {
						atomic.AddInt32(&cloudHits, 1)
						return &router.GenerateResult{Text: "cloud-forbidden"}, nil
					}),
					Ollama: router.ProviderFunc(func(ctx context.Context, model, prompt string, images []string, tools []mcp.Tool, out io.Writer) (*router.GenerateResult, error) {
						modelsMu.Lock()
						gotModels = append(gotModels, model)
						modelsMu.Unlock()
						if tc.ollamaErr != nil {
							return nil, tc.ollamaErr
						}
						return &router.GenerateResult{Text: "local:" + model}, nil
					}),
				},
			})
			if err != nil {
				t.Fatalf("NewSession failed: %v", err)
			}
			defer sess.Close()

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
				t.Fatalf("cloud provider must receive 0 requests in local-only mode, got %d", got)
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
	sess, err := engine.NewSession(engine.SessionConfig{
		SessionName:      "explicit-ollama-no-cloud",
		ExplicitProvider: "ollama",
		LocalModelName:   "gemma4:e2b",
		CoreMemory:       "<core_memory></core_memory>",
		Providers: router.Providers{
			Cloud: router.ProviderFunc(func(ctx context.Context, model, prompt string, images []string, tools []mcp.Tool, out io.Writer) (*router.GenerateResult, error) {
				atomic.AddInt32(&cloudHits, 1)
				return &router.GenerateResult{Text: "forbidden"}, nil
			}),
			Ollama: router.ProviderFunc(func(ctx context.Context, model, prompt string, images []string, tools []mcp.Tool, out io.Writer) (*router.GenerateResult, error) {
				return nil, errors.New("ollama crashed")
			}),
		},
	})
	if err != nil {
		t.Fatalf("NewSession failed: %v", err)
	}
	defer sess.Close()

	_, err = sess.ExecuteTurn(context.Background(), "hi", nil, ioDiscard{})
	if err == nil {
		t.Fatal("expected error when ollama fails")
	}
	if got := atomic.LoadInt32(&cloudHits); got != 0 {
		t.Fatalf("explicit ollama must NEVER call cloud, got %d cloud requests", got)
	}
}

func TestTurn_OpenAICompatibleLocalEndpointDispatch(t *testing.T) {
	isolateEngineTestEnv(t)

	var cloudHits, ollamaHits, openAIHits int32
	var reqModels []string
	var reqMu sync.Mutex

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
		OpenAIBaseURL: "http://127.0.0.1:1234/v1",
		OpenAIAPIKey:  "local-key",
		OpenAIModel:   "llama-3.2-3b-instruct",
		Registry:      reg,
		CoreMemory:    "<core_memory></core_memory>",
		Providers: router.Providers{
			Cloud: router.ProviderFunc(func(ctx context.Context, model, prompt string, images []string, tools []mcp.Tool, out io.Writer) (*router.GenerateResult, error) {
				atomic.AddInt32(&cloudHits, 1)
				return &router.GenerateResult{Text: "cloud-forbidden"}, nil
			}),
			Ollama: router.ProviderFunc(func(ctx context.Context, model, prompt string, images []string, tools []mcp.Tool, out io.Writer) (*router.GenerateResult, error) {
				atomic.AddInt32(&ollamaHits, 1)
				return &router.GenerateResult{Text: "ollama-forbidden"}, nil
			}),
			OpenAI: router.ProviderFunc(func(ctx context.Context, model, prompt string, images []string, tools []mcp.Tool, out io.Writer) (*router.GenerateResult, error) {
				n := atomic.AddInt32(&openAIHits, 1)
				reqMu.Lock()
				reqModels = append(reqModels, model)
				reqMu.Unlock()
				if n == 1 {
					return &router.GenerateResult{
						ToolCalls: []cmdparse.ToolCall{{ToolCall: "echo_tool", Arguments: map[string]interface{}{"msg": "ping"}}},
					}, nil
				}
				return &router.GenerateResult{Text: "openai-local-ok"}, nil
			}),
		},
	})
	if err != nil {
		t.Fatalf("NewSession failed: %v", err)
	}
	defer sess.Close()

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
		t.Fatalf("expected 0 ollama hits when OpenAIBaseURL is set, got %d", ollamaHits)
	}
	if atomic.LoadInt32(&openAIHits) != 2 {
		t.Fatalf("expected 2 OpenAI requests (tool call + final response), got %d", openAIHits)
	}
	reqMu.Lock()
	defer reqMu.Unlock()
	if reqModels[0] != "llama-3.2-3b-instruct" {
		t.Fatalf("unexpected OpenAI request model: %+v", reqModels)
	}
}

func TestTurn_SetRoutingModeConcurrent_Race(t *testing.T) {
	isolateEngineTestEnv(t)

	sess, err := engine.NewSession(engine.SessionConfig{
		SessionName:    "routing-mode-race",
		RoutingMode:    "local-only",
		LocalFastModel: "gemma4:e2b",
		CoreMemory:     "<core_memory></core_memory>",
		Providers: router.Providers{
			Ollama: router.ProviderFunc(func(ctx context.Context, model, prompt string, images []string, tools []mcp.Tool, out io.Writer) (*router.GenerateResult, error) {
				return &router.GenerateResult{Text: "ok"}, nil
			}),
		},
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

func TestTurn_LocalActiveModel_HybridNeverCallsCloud(t *testing.T) {
	isolateEngineTestEnv(t)

	var cloudHits, ollamaHits int32
	var gotModel string
	var mu sync.Mutex

	sess, err := engine.NewSession(engine.SessionConfig{
		SessionName: "local-active-model-hybrid",
		ActiveModel: "orbit-thinker:latest",
		RoutingMode: "hybrid",
		CoreMemory:  "<core_memory></core_memory>",
		Providers: router.Providers{
			Cloud: router.ProviderFunc(func(ctx context.Context, model, prompt string, images []string, tools []mcp.Tool, out io.Writer) (*router.GenerateResult, error) {
				atomic.AddInt32(&cloudHits, 1)
				return nil, cloud.ErrQuotaExhausted
			}),
			Ollama: router.ProviderFunc(func(ctx context.Context, model, prompt string, images []string, tools []mcp.Tool, out io.Writer) (*router.GenerateResult, error) {
				atomic.AddInt32(&ollamaHits, 1)
				mu.Lock()
				gotModel = model
				mu.Unlock()
				return &router.GenerateResult{Text: "local orbit-thinker reply"}, nil
			}),
		},
	})
	if err != nil {
		t.Fatalf("NewSession failed: %v", err)
	}
	defer sess.Close()

	resp, err := sess.ExecuteTurn(context.Background(), "architect and refactor this service", nil, ioDiscard{})
	if err != nil {
		t.Fatalf("ExecuteTurn failed: %v", err)
	}
	if !strings.Contains(resp, "local orbit-thinker reply") {
		t.Fatalf("unexpected response: %q", resp)
	}
	if atomic.LoadInt32(&cloudHits) != 0 {
		t.Fatalf("expected 0 cloud hits for local ActiveModel orbit-thinker:latest, got %d", cloudHits)
	}
	if atomic.LoadInt32(&ollamaHits) != 1 {
		t.Fatalf("expected 1 ollama hit, got %d", ollamaHits)
	}
	mu.Lock()
	defer mu.Unlock()
	if gotModel != "orbit-thinker:latest" {
		t.Fatalf("expected ollama model orbit-thinker:latest, got %q", gotModel)
	}
}

func TestTurn_NoSpuriousAutoToolWithoutPlaceholder(t *testing.T) {
	isolateEngineTestEnv(t)

	var toolCalls int32
	reg := tools.NewRegistry()
	reg.Register(mcp.Tool{Name: "search_web", InputSchema: mcp.Schema{Type: "object"}}, func(ctx context.Context, args map[string]interface{}) (string, error) {
		atomic.AddInt32(&toolCalls, 1)
		return "search results", nil
	})
	reg.Register(mcp.Tool{Name: "get_current_time", InputSchema: mcp.Schema{Type: "object"}}, func(ctx context.Context, args map[string]interface{}) (string, error) {
		atomic.AddInt32(&toolCalls, 1)
		return "2026-11-19", nil
	})

	var ollamaHits int32
	sess, err := engine.NewSession(engine.SessionConfig{
		SessionName: "no-spurious-autotool",
		ActiveModel: "gemma4:e2b",
		RoutingMode: "local-only",
		Registry:    reg,
		CoreMemory:  "<core_memory></core_memory>",
		Providers: router.Providers{
			Ollama: router.ProviderFunc(func(ctx context.Context, model, prompt string, images []string, tools []mcp.Tool, out io.Writer) (*router.GenerateResult, error) {
				hit := atomic.AddInt32(&ollamaHits, 1)
				if hit == 1 {
					return &router.GenerateResult{Text: "Hello! How can I help you today?"}, nil
				}
				return &router.GenerateResult{Text: "Done."}, nil
			}),
		},
	})
	if err != nil {
		t.Fatalf("NewSession failed: %v", err)
	}
	defer sess.Close()

	resp, err := sess.ExecuteTurn(context.Background(), "search today hello", nil, ioDiscard{})
	if err != nil {
		t.Fatalf("ExecuteTurn failed: %v", err)
	}
	if resp != "Hello! How can I help you today?" {
		t.Fatalf("unexpected response: %q", resp)
	}
	if atomic.LoadInt32(&toolCalls) != 0 {
		t.Fatalf("expected 0 spurious auto-tool executions, got %d", toolCalls)
	}
	if atomic.LoadInt32(&ollamaHits) != 1 {
		t.Fatalf("expected single LLM pass without re-execution, got %d", ollamaHits)
	}
}

func TestExecuteTurn_MemoryRecallPersistenceAndSingleVisionPass(t *testing.T) {
	isolateEngineTestEnv(t)

	t.Run("nil MemoryStore is a clean no-op", func(t *testing.T) {
		var seenPrompt string
		sess, err := engine.NewSession(engine.SessionConfig{
			SessionName: "nil-mem-test",
			RoutingMode: "local-only",
			CoreMemory:  "<core_memory></core_memory>",
			MemoryStore: nil,
			Providers: router.Providers{
				Ollama: router.ProviderFunc(func(ctx context.Context, model, prompt string, images []string, tools []mcp.Tool, out io.Writer) (*router.GenerateResult, error) {
					seenPrompt = prompt
					return &router.GenerateResult{Text: "ok without memory"}, nil
				}),
			},
		})
		if err != nil {
			t.Fatalf("NewSession failed: %v", err)
		}
		defer sess.Close()

		resp, err := sess.ExecuteTurn(context.Background(), "deploy microgravity kubernetes cluster checklist", nil, ioDiscard{})
		if err != nil {
			t.Fatalf("ExecuteTurn failed: %v", err)
		}
		if resp != "ok without memory" {
			t.Fatalf("got %q, want 'ok without memory'", resp)
		}
		if strings.Contains(seenPrompt, "[Relevant context from past sessions]") {
			t.Fatalf("expected no memory injection when MemoryStore == nil")
		}
	})

	t.Run("stores completed turn and recalls relevant context in subsequent session", func(t *testing.T) {
		dbPath := filepath.Join(t.TempDir(), "memory.db")
		store, err := memory.NewMemoryStore(dbPath)
		if err != nil {
			t.Fatalf("NewMemoryStore failed: %v", err)
		}
		defer store.Close()

		// Session 1: persist a domain-specific turn via ExecuteTurn
		sess1, err := engine.NewSession(engine.SessionConfig{
			SessionName: "sess-1",
			RoutingMode: "local-only",
			CoreMemory:  "<core_memory></core_memory>",
			MemoryStore: store,
			Providers: router.Providers{
				Ollama: router.ProviderFunc(func(ctx context.Context, model, prompt string, images []string, tools []mcp.Tool, out io.Writer) (*router.GenerateResult, error) {
					return &router.GenerateResult{Text: "Use PostgreSQL port 5439 with scram-sha-256 authentication for billing."}, nil
				}),
			},
		})
		if err != nil {
			t.Fatalf("NewSession 1 failed: %v", err)
		}
		defer sess1.Close()

		_, err = sess1.ExecuteTurn(context.Background(), "Configure PostgreSQL billing database authentication port", nil, ioDiscard{})
		if err != nil {
			t.Fatalf("Session 1 ExecuteTurn failed: %v", err)
		}

		// Session 2: a fresh session querying the same topic must receive recalled memory context
		var recalledPrompt string
		sess2, err := engine.NewSession(engine.SessionConfig{
			SessionName: "sess-2",
			RoutingMode: "local-only",
			CoreMemory:  "<core_memory></core_memory>",
			MemoryStore: store,
			Providers: router.Providers{
				Ollama: router.ProviderFunc(func(ctx context.Context, model, prompt string, images []string, tools []mcp.Tool, out io.Writer) (*router.GenerateResult, error) {
					recalledPrompt = prompt
					return &router.GenerateResult{Text: "Verified PostgreSQL billing port 5439."}, nil
				}),
			},
		})
		if err != nil {
			t.Fatalf("NewSession 2 failed: %v", err)
		}
		defer sess2.Close()

		_, err = sess2.ExecuteTurn(context.Background(), "Configure PostgreSQL billing database authentication port", nil, ioDiscard{})
		if err != nil {
			t.Fatalf("Session 2 ExecuteTurn failed: %v", err)
		}
		if !strings.Contains(recalledPrompt, "[Relevant context from past sessions]") {
			t.Fatalf("expected recalled memory header in prompt, got:\n%s", recalledPrompt)
		}
		if !strings.Contains(recalledPrompt, "5439") && !strings.Contains(recalledPrompt, "PostgreSQL billing database") {
			t.Fatalf("expected recalled turn content in prompt, got:\n%s", recalledPrompt)
		}

		// Verify trivial prompt does NOT trigger memory injection
		var trivialPrompt string
		sess3, err := engine.NewSession(engine.SessionConfig{
			SessionName: "sess-3",
			RoutingMode: "local-only",
			CoreMemory:  "<core_memory></core_memory>",
			MemoryStore: store,
			Providers: router.Providers{
				Ollama: router.ProviderFunc(func(ctx context.Context, model, prompt string, images []string, tools []mcp.Tool, out io.Writer) (*router.GenerateResult, error) {
					trivialPrompt = prompt
					return &router.GenerateResult{Text: "hello"}, nil
				}),
			},
		})
		if err != nil {
			t.Fatalf("NewSession 3 failed: %v", err)
		}
		defer sess3.Close()

		_, err = sess3.ExecuteTurn(context.Background(), "hi", nil, ioDiscard{})
		if err != nil {
			t.Fatalf("Session 3 ExecuteTurn failed: %v", err)
		}
		if strings.Contains(trivialPrompt, "[Relevant context from past sessions]") {
			t.Fatalf("expected no memory injection for trivial prompt 'hi', got:\n%s", trivialPrompt)
		}
	})

	t.Run("single vision.ExtractImages pass forwards extracted image to Provider", func(t *testing.T) {
		imgPath := filepath.Join(t.TempDir(), "sample.png")
		if err := os.WriteFile(imgPath, []byte("fake-png-bytes"), 0644); err != nil {
			t.Fatalf("write temp image: %v", err)
		}

		var gotImages []string
		var gotPrompt string
		sess, err := engine.NewSession(engine.SessionConfig{
			SessionName: "vision-pass-test",
			RoutingMode: "local-only",
			CoreMemory:  "<core_memory></core_memory>",
			Providers: router.Providers{
				Ollama: router.ProviderFunc(func(ctx context.Context, model, prompt string, images []string, tools []mcp.Tool, out io.Writer) (*router.GenerateResult, error) {
					gotImages = append([]string(nil), images...)
					gotPrompt = prompt
					return &router.GenerateResult{Text: "image analyzed"}, nil
				}),
			},
		})
		if err != nil {
			t.Fatalf("NewSession failed: %v", err)
		}
		defer sess.Close()

		resp, err := sess.ExecuteTurn(context.Background(), fmt.Sprintf("Inspect %q carefully", imgPath), nil, ioDiscard{})
		if err != nil {
			t.Fatalf("ExecuteTurn failed: %v", err)
		}
		if resp != "image analyzed" {
			t.Fatalf("got %q, want 'image analyzed'", resp)
		}
		if len(gotImages) != 1 {
			t.Fatalf("expected exactly 1 extracted image (single pass), got %d", len(gotImages))
		}
		if !strings.Contains(gotPrompt, "[Media (image/png): sample.png]") {
			t.Fatalf("expected media placeholder in prompt, got:\n%s", gotPrompt)
		}
	})
}

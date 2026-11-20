package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/CmfH009/microgravity/internal/mcp"
)

func isolateOpenAITestEnv(t *testing.T) {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("USERPROFILE", tmp)
	t.Setenv("MG_BUDGET_FILE", tmp+"/budget.json")
	t.Setenv("MG_BUDGET_DISABLE", "1")
}

func TestOpenAI_DefaultClientHasResponseHeaderTimeout(t *testing.T) {
	c := NewClient("", "")
	if c.HTTPClient == nil {
		t.Fatal("expected non-nil HTTPClient")
	}
	tr, ok := c.HTTPClient.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("expected *http.Transport, got %T", c.HTTPClient.Transport)
	}
	if tr.ResponseHeaderTimeout <= 0 {
		t.Fatalf("expected ResponseHeaderTimeout > 0, got %v", tr.ResponseHeaderTimeout)
	}
}

func TestOpenAI_EndpointURLNormalization(t *testing.T) {
	tests := []struct {
		baseURL string
		want    string
	}{
		{"http://localhost:1234/v1", "http://localhost:1234/v1/chat/completions"},
		{"http://localhost:1234/v1/", "http://localhost:1234/v1/chat/completions"},
		{"http://localhost:8080", "http://localhost:8080/v1/chat/completions"},
		{"http://localhost:8080/", "http://localhost:8080/v1/chat/completions"},
		{"http://localhost:4000/proxy/v1", "http://localhost:4000/proxy/v1/chat/completions"},
	}

	for _, tc := range tests {
		c := NewClient(tc.baseURL, "")
		if got := c.endpointURL("/chat/completions"); got != tc.want {
			t.Errorf("baseURL=%q -> endpointURL=%q, want %q", tc.baseURL, got, tc.want)
		}
	}
}

func TestOpenAI_StreamingTextAndAuthAndSystemPrompt(t *testing.T) {
	isolateOpenAITestEnv(t)

	var gotPath string
	var gotAuth string
	var gotReq ChatCompletionRequest

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		if err := json.NewDecoder(r.Body).Decode(&gotReq); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(": keepalive\n\n"))
		_, _ = w.Write([]byte("event: message\n"))
		_, _ = w.Write([]byte(`data: {"id":"1","choices":[{"index":0,"delta":{"role":"assistant","content":"Hello "}}]}` + "\n\n"))
		_, _ = w.Write([]byte(`data:{"id":"1","choices":[{"index":0,"delta":{"content":"local world!"}}]}` + "\n\n"))
		_, _ = w.Write([]byte(`data: {"id":"1","choices":[],"usage":{"prompt_tokens":12,"completion_tokens":4}}` + "\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer srv.Close()

	client := NewClient(srv.URL+"/v1", "sk-local-secret")
	var out bytes.Buffer
	res, err := client.GenerateWithTools(
		context.Background(),
		"qwen2.5-coder:7b",
		"You are helpful.\n---CONVERSATION---\nUser: Say hi",
		[]string{"YWJj"},
		nil,
		&out,
	)
	if err != nil {
		t.Fatalf("GenerateWithTools failed: %v", err)
	}
	if res.Text != "Hello local world!" {
		t.Fatalf("expected 'Hello local world!', got %q", res.Text)
	}
	if !strings.Contains(out.String(), "Hello local world!") {
		t.Fatalf("expected streamed output to contain 'Hello local world!', got %q", out.String())
	}
	if gotPath != "/v1/chat/completions" {
		t.Fatalf("expected path /v1/chat/completions, got %q", gotPath)
	}
	if gotAuth != "Bearer sk-local-secret" {
		t.Fatalf("expected Bearer auth header, got %q", gotAuth)
	}
	if gotReq.Model != "qwen2.5-coder:7b" || !gotReq.Stream {
		t.Fatalf("unexpected request model/stream: %+v", gotReq)
	}
	if len(gotReq.Messages) != 2 || gotReq.Messages[0].Role != "system" || gotReq.Messages[1].Role != "user" {
		t.Fatalf("expected system + user messages, got %+v", gotReq.Messages)
	}
}

func TestOpenAI_StreamingToolCallDeltaAssembly(t *testing.T) {
	isolateOpenAITestEnv(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req ChatCompletionRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if len(req.Tools) != 2 {
			t.Errorf("expected 2 tools in request, got %d", len(req.Tools))
		}

		w.Header().Set("Content-Type", "text/event-stream")
		// Interleaved deltas for index 0 and index 1, plus repeated function name on index 0 and zero-arg call on index 1.
		chunks := []string{
			`data: {"choices":[{"index":0,"delta":{"role":"assistant","content":"Checking..."}}]}`,
			`data: {"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"read_file","arguments":"{\"pa"}}]}}]}`,
			`data: {"choices":[{"index":0,"delta":{"tool_calls":[{"index":1,"id":"call_2","type":"function","function":{"name":"get_time","arguments":""}}]}}]}`,
			`data: {"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"name":"read_file","arguments":"th\":\"go.mod\"}"}}]}}]}`,
			`data: {"choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}`,
			`data: [DONE]`,
		}
		for _, c := range chunks {
			_, _ = w.Write([]byte(c + "\n\n"))
		}
	}))
	defer srv.Close()

	tools := []mcp.Tool{
		{
			Name:        "read_file",
			Description: "Read a file",
			InputSchema: mcp.Schema{
				Type:       "object",
				Properties: map[string]interface{}{"path": map[string]interface{}{"type": "string"}},
				Required:   []string{"path"},
			},
		},
		{
			Name:        "get_time",
			Description: "Get current time",
		},
	}

	client := NewClient(srv.URL, "")
	var out bytes.Buffer
	res, err := client.GenerateWithTools(context.Background(), "llama-3.2", "read go.mod", nil, tools, &out)
	if err != nil {
		t.Fatalf("GenerateWithTools failed: %v", err)
	}
	if res.Text != "Checking..." {
		t.Fatalf("expected commentary 'Checking...', got %q", res.Text)
	}
	if len(res.ToolCalls) != 2 {
		t.Fatalf("expected 2 assembled tool calls, got %d: %+v", len(res.ToolCalls), res.ToolCalls)
	}
	if res.ToolCalls[0].ToolCall != "read_file" || res.ToolCalls[0].Arguments["path"] != "go.mod" {
		t.Fatalf("unexpected tool call 0: %+v", res.ToolCalls[0])
	}
	if res.ToolCalls[1].ToolCall != "get_time" || len(res.ToolCalls[1].Arguments) != 0 {
		t.Fatalf("unexpected tool call 1: %+v", res.ToolCalls[1])
	}
}

func TestOpenAI_NonStreamingJSONFallback(t *testing.T) {
	isolateOpenAITestEnv(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"id": "chatcmpl-nonstream",
			"choices": [{
				"index": 0,
				"message": {
					"role": "assistant",
					"content": "done",
					"tool_calls": [{
						"id": "call_9",
						"type": "function",
						"function": {"name": "list_dir", "arguments": "{\"path\":\".\"}"}
					}]
				},
				"finish_reason": "tool_calls"
			}]
		}`))
	}))
	defer srv.Close()

	client := NewClient(srv.URL+"/v1", "")
	res, err := client.GenerateWithTools(context.Background(), "local-model", "list", nil, nil, io.Discard)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Text != "done" || len(res.ToolCalls) != 1 || res.ToolCalls[0].ToolCall != "list_dir" || res.ToolCalls[0].Arguments["path"] != "." {
		t.Fatalf("unexpected non-stream result: %+v", res)
	}
}

func TestOpenAI_ErrorPathsMalformedSSEAndPrematureEOF(t *testing.T) {
	tests := []struct {
		name       string
		handler    http.HandlerFunc
		wantTarget error
		wantSubstr string
	}{
		{
			name: "404 returns ErrModelNotFound",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusNotFound)
				_, _ = w.Write([]byte(`{"error":{"message":"model missing:latest not found"}}`))
			},
			wantTarget: ErrModelNotFound,
			wantSubstr: "missing:latest",
		},
		{
			name: "500 non-200 error preserves bounded body",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = w.Write([]byte(`{"error":{"message":"backend OOM"}}`))
			},
			wantSubstr: "backend OOM",
		},
		{
			name: "Empty 200 body returns ErrEmptyResponse",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				w.WriteHeader(http.StatusOK)
			},
			wantTarget: ErrEmptyResponse,
		},
		{
			name: "Malformed SSE JSON chunk returns error",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = w.Write([]byte("data: {broken-json\n\n"))
			},
			wantSubstr: "malformed openai SSE chunk",
		},
		{
			name: "In-stream SSE error object is surfaced",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = w.Write([]byte(`data: {"error":{"message":"context length exceeded"}}` + "\n\n"))
			},
			wantSubstr: "context length exceeded",
		},
		{
			name: "Malformed streamed tool call arguments JSON returns error",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = w.Write([]byte(`data: {"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"c1","function":{"name":"run","arguments":"{\"bad\":"}}]}}]}` + "\n\n"))
				_, _ = w.Write([]byte("data: [DONE]\n\n"))
			},
			wantSubstr: "malformed tool call arguments",
		},
		{
			name: "Premature EOF without [DONE] returns ErrUnexpectedEOF",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = w.Write([]byte(`data: {"choices":[{"index":0,"delta":{"content":"partial"}}]}` + "\n\n"))
			},
			wantTarget: io.ErrUnexpectedEOF,
			wantSubstr: "without [DONE]",
		},
		{
			name: "Premature chunked TCP close returns error",
			handler: func(w http.ResponseWriter, r *http.Request) {
				hj, ok := w.(http.Hijacker)
				if !ok {
					http.Error(w, "no hijack", http.StatusInternalServerError)
					return
				}
				conn, buf, err := hj.Hijack()
				if err != nil {
					return
				}
				_, _ = buf.WriteString("HTTP/1.1 200 OK\r\nContent-Type: text/event-stream\r\nTransfer-Encoding: chunked\r\n\r\n")
				chunk := `data: {"choices":[{"index":0,"delta":{"content":"hi"}}]}` + "\n\n"
				_, _ = buf.WriteString("3a\r\n" + chunk + "\r\n")
				_ = buf.Flush()
				_ = conn.Close()
			},
			wantSubstr: "stream",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			isolateOpenAITestEnv(t)
			srv := httptest.NewServer(tc.handler)
			defer srv.Close()

			// Exercise nil HTTPClient fallback too.
			client := &Client{BaseURL: srv.URL}
			res, err := client.GenerateWithTools(context.Background(), "local-model", "hello", nil, nil, io.Discard)
			if err == nil {
				t.Fatalf("expected error, got nil (res=%+v)", res)
			}
			if tc.wantTarget != nil && !errors.Is(err, tc.wantTarget) {
				t.Fatalf("expected errors.Is(err, %v), got: %v", tc.wantTarget, err)
			}
			if tc.wantSubstr != "" && !strings.Contains(err.Error(), tc.wantSubstr) {
				t.Fatalf("expected error containing %q, got: %v", tc.wantSubstr, err)
			}
		})
	}
}

func TestOpenAI_ContextCancellation(t *testing.T) {
	t.Run("before request never dials server", func(t *testing.T) {
		isolateOpenAITestEnv(t)
		var dialed int32
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			atomic.AddInt32(&dialed, 1)
			w.WriteHeader(http.StatusOK)
		}))
		defer srv.Close()

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		client := NewClient(srv.URL, "")
		_, err := client.GenerateWithTools(ctx, "local", "hi", nil, nil, io.Discard)
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("expected context.Canceled, got: %v", err)
		}
		if atomic.LoadInt32(&dialed) != 0 {
			t.Fatal("server should not be dialed on pre-cancelled context")
		}
	})

	t.Run("mid-stream cancel aborts immediately", func(t *testing.T) {
		isolateOpenAITestEnv(t)
		firstSent := make(chan struct{})
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")
			flusher, _ := w.(http.Flusher)
			_, _ = w.Write([]byte(`data: {"choices":[{"index":0,"delta":{"content":"chunk1"}}]}` + "\n\n"))
			if flusher != nil {
				flusher.Flush()
			}
			select {
			case <-firstSent:
			default:
				close(firstSent)
			}
			<-r.Context().Done()
		}))
		defer srv.Close()

		client := NewClient(srv.URL, "")
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		go func() {
			<-firstSent
			time.Sleep(20 * time.Millisecond)
			cancel()
		}()

		_, err := client.GenerateWithTools(ctx, "local", "hi", nil, nil, io.Discard)
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("expected context.Canceled, got: %v", err)
		}
	})
}

func TestOpenAI_ListModelsAndHasModel(t *testing.T) {
	isolateOpenAITestEnv(t)

	var paths []string
	var mu sync.Mutex
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		paths = append(paths, r.URL.Path)
		mu.Unlock()

		if r.URL.Path == "/v1/models" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"object":"list","data":[{"id":"qwen2.5-coder-7b"},{"id":"gemma-3-4b"}]}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	// Verify both base URL with and without /v1.
	for _, base := range []string{srv.URL, srv.URL + "/v1"} {
		client := NewClient(base, "")
		models, err := client.ListModels(context.Background())
		if err != nil {
			t.Fatalf("ListModels(%q) failed: %v", base, err)
		}
		if len(models) != 2 || models[0] != "qwen2.5-coder-7b" || models[1] != "gemma-3-4b" {
			t.Fatalf("unexpected models: %+v", models)
		}
		has, err := client.HasModel(context.Background(), "gemma-3-4b")
		if err != nil || !has {
			t.Fatalf("expected HasModel(gemma-3-4b)=true, got (%v, %v)", has, err)
		}
		hasMissing, err := client.HasModel(context.Background(), "nonexistent")
		if err != nil || hasMissing {
			t.Fatalf("expected HasModel(nonexistent)=false, got (%v, %v)", hasMissing, err)
		}
	}

	errSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte("loading model"))
	}))
	defer errSrv.Close()

	if _, err := NewClient(errSrv.URL, "").ListModels(context.Background()); err == nil || !strings.Contains(err.Error(), "503") {
		t.Fatalf("expected 503 error from ListModels, got: %v", err)
	}
}

func TestOpenAI_ConcurrentStress_Race(t *testing.T) {
	isolateOpenAITestEnv(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/models" {
			_, _ = w.Write([]byte(`{"object":"list","data":[{"id":"local-fast"}]}`))
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(`data: {"choices":[{"index":0,"delta":{"content":"ok"}}]}` + "\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer srv.Close()

	client := NewClient(srv.URL+"/v1", "test-token")
	tools := []mcp.Tool{{Name: "ping", InputSchema: mcp.Schema{Type: "object"}}}

	const workers = 12
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 4; j++ {
				var out bytes.Buffer
				text, err := client.Generate(context.Background(), "local-fast", "hi", nil, &out)
				if err != nil {
					t.Errorf("concurrent Generate error: %v", err)
					return
				}
				if text != "ok" {
					t.Errorf("expected 'ok', got %q", text)
					return
				}
				res, err := client.GenerateWithTools(context.Background(), "local-fast", "hi", nil, tools, io.Discard)
				if err != nil || res.Text != "ok" {
					t.Errorf("concurrent GenerateWithTools error: res=%+v err=%v", res, err)
					return
				}
				if _, err := client.ListModels(context.Background()); err != nil {
					t.Errorf("concurrent ListModels error: %v", err)
					return
				}
			}
		}()
	}
	wg.Wait()
}

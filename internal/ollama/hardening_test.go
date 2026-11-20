package ollama

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/CmfH009/microgravity/internal/mcp"
)

func isolateOllamaTestEnv(t *testing.T) {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("USERPROFILE", tmp)
	t.Setenv("MG_BUDGET_FILE", tmp+"/budget.json")
	t.Setenv("MG_BUDGET_DISABLE", "1")
}

func TestOllama_ContextCancelMidStream(t *testing.T) {
	isolateOllamaTestEnv(t)

	firstSent := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-ndjson")
		flusher, _ := w.(http.Flusher)
		_, _ = w.Write([]byte(`{"message":{"role":"assistant","content":"chunk1"},"done":false}` + "\n"))
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

	client := NewClient(srv.URL)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		<-firstSent
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()

	_, err := client.GenerateWithTools(ctx, "gemma4:e2b", "hi", nil, nil, io.Discard)
	if err == nil {
		t.Fatal("expected error on context cancel")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got: %v", err)
	}
}

func TestOllama_ContextCancelBeforeRequest(t *testing.T) {
	isolateOllamaTestEnv(t)

	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	client := NewClient(srv.URL)
	_, err := client.GenerateWithTools(ctx, "gemma4:e2b", "hi", nil, nil, io.Discard)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got: %v", err)
	}
	if called {
		t.Fatal("server should not be dialed when context is already cancelled")
	}
}

func TestOllama_MalformedNDJSONAndPrematureEOF(t *testing.T) {
	tests := []struct {
		name       string
		handler    http.HandlerFunc
		wantErr    bool
		wantTarget error
		wantSubstr string
	}{
		{
			name: "Empty 200 body returns ErrEmptyResponse",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/x-ndjson")
				w.WriteHeader(http.StatusOK)
			},
			wantErr:    true,
			wantTarget: ErrEmptyResponse,
		},
		{
			name: "Malformed NDJSON line returns error",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/x-ndjson")
				_, _ = w.Write([]byte("{bad json}\n"))
			},
			wantErr: true,
		},
		{
			name: "Valid chunk followed by truncated JSON at EOF returns error",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/x-ndjson")
				_, _ = w.Write([]byte(`{"message":{"role":"assistant","content":"hi"},"done":false}` + "\n"))
				_, _ = w.Write([]byte(`{"message":{"role":"assistant"`))
			},
			wantErr: true,
		},
		{
			name: "Valid chunk with done=false then EOF without done=true returns ErrUnexpectedEOF",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/x-ndjson")
				_, _ = w.Write([]byte(`{"message":{"role":"assistant","content":"hi"},"done":false}` + "\n"))
			},
			wantErr:    true,
			wantTarget: io.ErrUnexpectedEOF,
		},
		{
			name: "In-stream JSON error on 200 OK is surfaced",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/x-ndjson")
				_, _ = w.Write([]byte(`{"error":"model runner process terminated"}` + "\n"))
			},
			wantErr:    true,
			wantSubstr: "model runner process terminated",
		},
		{
			name: "404 preserves server body and wraps ErrModelNotFound",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusNotFound)
				_, _ = w.Write([]byte(`{"error":"model 'missing:latest' not found"}`))
			},
			wantErr:    true,
			wantTarget: ErrModelNotFound,
			wantSubstr: "missing:latest",
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
				_, _ = buf.WriteString("HTTP/1.1 200 OK\r\nContent-Type: application/x-ndjson\r\nTransfer-Encoding: chunked\r\n\r\n")
				line := `{"message":{"role":"assistant","content":"hi"},"done":false}` + "\n"
				_, _ = buf.WriteString("3d\r\n" + line + "\r\n")
				_ = buf.Flush()
				_ = conn.Close()
			},
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			isolateOllamaTestEnv(t)
			srv := httptest.NewServer(tc.handler)
			defer srv.Close()

			// Test with nil HTTPClient as well to verify safe fallback.
			client := &Client{BaseURL: srv.URL}
			var out bytes.Buffer
			res, err := client.GenerateWithTools(context.Background(), "gemma4:e2b", "hello", nil, nil, &out)
			if !tc.wantErr {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
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

func TestOllama_PullAndHasModelHardening(t *testing.T) {
	t.Run("Pull surfaces in-stream error", func(t *testing.T) {
		isolateOllamaTestEnv(t)
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"status":"pulling manifest"}` + "\n"))
			_, _ = w.Write([]byte(`{"error":"pull model manifest: file does not exist"}` + "\n"))
		}))
		defer srv.Close()

		client := NewClient(srv.URL)
		err := client.Pull(context.Background(), "nonexistent", nil)
		if err == nil || !strings.Contains(err.Error(), "file does not exist") {
			t.Fatalf("expected pull error to be surfaced, got: %v", err)
		}
	})

	t.Run("HasModel exact match vs :latest without prefix false positive", func(t *testing.T) {
		isolateOllamaTestEnv(t)
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"models":[{"name":"gemma:latest-custom"},{"name":"gemma4:e2b"}]}`))
		}))
		defer srv.Close()

		client := NewClient(srv.URL)
		hasCustom, err := client.HasModel(context.Background(), "gemma")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if hasCustom {
			t.Fatal("HasModel(\"gemma\") must NOT match \"gemma:latest-custom\"")
		}
		hasE2B, err := client.HasModel(context.Background(), "gemma4:e2b")
		if err != nil || !hasE2B {
			t.Fatalf("expected gemma4:e2b=true, got (%v, %v)", hasE2B, err)
		}
	})
}

func TestOllama_ConcurrentGenerateWithTools_Race(t *testing.T) {
	isolateOllamaTestEnv(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/tags" {
			_, _ = w.Write([]byte(`{"models":[{"name":"gemma4:e2b"}]}`))
			return
		}
		w.Header().Set("Content-Type", "application/x-ndjson")
		_, _ = w.Write([]byte(`{"message":{"role":"assistant","content":"local-ok"},"done":true}` + "\n"))
	}))
	defer srv.Close()

	client := NewClient(srv.URL)
	tools := []mcp.Tool{{Name: "ping", InputSchema: mcp.Schema{Type: "object"}}}

	const workers = 12
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 4; j++ {
				var out bytes.Buffer
				res, err := client.GenerateWithTools(context.Background(), "gemma4:e2b", "hi", nil, tools, &out)
				if err != nil {
					t.Errorf("concurrent GenerateWithTools error: %v", err)
					return
				}
				if res.Text != "local-ok" {
					t.Errorf("expected local-ok, got %q", res.Text)
					return
				}
				if _, err := client.HasModel(context.Background(), "gemma4:e2b"); err != nil {
					t.Errorf("concurrent HasModel error: %v", err)
					return
				}
			}
		}()
	}
	wg.Wait()
}

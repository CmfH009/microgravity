package cloud

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/CmfH009/microgravity/internal/authutil"
	"github.com/CmfH009/microgravity/internal/mcp"
	"golang.org/x/oauth2"
)

func isolateCloudTestEnv(t *testing.T) {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("USERPROFILE", tmp)
	t.Setenv("APPDATA", tmp)
	t.Setenv("MG_BUDGET_FILE", tmp+"/budget.json")
	t.Setenv("MG_BUDGET_DISABLE", "1")
	t.Setenv("GEMINI_API_KEY", "")
	t.Setenv("GOOGLE_API_KEY", "")
	t.Setenv("VERTEX_API_KEY", "")
	t.Setenv("VERTEX_PROJECT_ID", "")
	t.Setenv("VERTEX_LOCATION", "")
	t.Setenv("GOOGLE_APPLICATION_CREDENTIALS", "")
}

func TestCloud_ContextCancelMidBackoff(t *testing.T) {
	isolateCloudTestEnv(t)

	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.Header().Set("Retry-After", "5")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":{"code":429,"status":"RESOURCE_EXHAUSTED","message":"rate limited"}}`))
	}))
	defer srv.Close()

	client := NewClient()
	client.APIKey = "test-key"
	client.BaseURL = srv.URL

	ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, err := client.GenerateWithTools(ctx, "gemini-3.8-flash", "hello", nil, nil, io.Discard)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected context deadline/cancel error, got nil")
	}
	if !errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context cancellation error, got: %v", err)
	}
	if elapsed > 1*time.Second {
		t.Fatalf("cancellation during backoff took too long (%v); fallback loop must abort immediately on ctx.Done()", elapsed)
	}
	if got := atomic.LoadInt32(&hits); got > 2 {
		t.Fatalf("expected at most 1-2 requests before aborting on ctx cancel, got %d", got)
	}
}

func TestCloud_ContextCancelMidStream(t *testing.T) {
	isolateCloudTestEnv(t)

	firstChunkSent := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)
		_, _ = w.Write([]byte(`data: {"candidates":[{"content":{"parts":[{"text":"partial"}]}}]}` + "\n\n"))
		if flusher != nil {
			flusher.Flush()
		}
		select {
		case <-firstChunkSent:
		default:
			close(firstChunkSent)
		}
		<-r.Context().Done()
	}))
	defer srv.Close()

	client := NewClient()
	client.APIKey = "test-key"
	client.BaseURL = srv.URL

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		<-firstChunkSent
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()

	var out bytes.Buffer
	_, err := client.GenerateWithTools(ctx, "gemini-3.8-flash", "stream", nil, nil, &out)
	if err == nil {
		t.Fatal("expected error on mid-stream context cancellation")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got: %v", err)
	}
}

func TestCloud_QuotaAndBillingErrors_SentinelAndNoMasking(t *testing.T) {
	tests := []struct {
		name           string
		statusCode     int
		body           string
		ssePayload     string
		wantQuotaErr   bool
		wantBodySubstr string
		maxHits        int32
	}{
		{
			name:           "402 Payment Required fails fast and wraps ErrQuotaExhausted",
			statusCode:     http.StatusPaymentRequired,
			body:           `{"error":{"code":402,"message":"billing account delinquent"}}`,
			wantQuotaErr:   true,
			wantBodySubstr: "billing account delinquent",
			maxHits:        1,
		},
		{
			name:           "403 RESOURCE_EXHAUSTED wraps ErrQuotaExhausted",
			statusCode:     http.StatusForbidden,
			body:           `{"error":{"code":403,"status":"RESOURCE_EXHAUSTED","message":"quota limit reached"}}`,
			wantQuotaErr:   true,
			wantBodySubstr: "quota limit reached",
			maxHits:        1,
		},
		{
			name:           "All 404 endpoints preserve 404 body without reading closed body",
			statusCode:     http.StatusNotFound,
			body:           `{"error":{"code":404,"message":"model missing everywhere"}}`,
			wantQuotaErr:   false,
			wantBodySubstr: "model missing everywhere",
			maxHits:        10,
		},
		{
			name:           "SSE 200 in-stream RESOURCE_EXHAUSTED error is not swallowed",
			statusCode:     http.StatusOK,
			ssePayload:     "data: {\"error\":{\"code\":429,\"status\":\"RESOURCE_EXHAUSTED\",\"message\":\"stream quota exhausted\"}}\n\n",
			wantQuotaErr:   true,
			wantBodySubstr: "stream quota exhausted",
			maxHits:        1,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			isolateCloudTestEnv(t)
			var hits int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				atomic.AddInt32(&hits, 1)
				if tc.ssePayload != "" {
					w.Header().Set("Content-Type", "text/event-stream")
					w.WriteHeader(tc.statusCode)
					_, _ = w.Write([]byte(tc.ssePayload))
					return
				}
				w.WriteHeader(tc.statusCode)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer srv.Close()

			client := NewClient()
			client.APIKey = "test-key"
			client.BaseURL = srv.URL

			_, err := client.GenerateWithTools(context.Background(), "gemini-3.8-flash", "hello", nil, nil, io.Discard)
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if tc.wantQuotaErr && !errors.Is(err, ErrQuotaExhausted) {
				t.Fatalf("expected errors.Is(err, ErrQuotaExhausted), got: %v", err)
			}
			if !strings.Contains(err.Error(), tc.wantBodySubstr) {
				t.Fatalf("expected error to preserve %q, got: %v", tc.wantBodySubstr, err)
			}
			if strings.Contains(err.Error(), "closed response body") {
				t.Fatalf("error leaked closed response body read: %v", err)
			}
			if got := atomic.LoadInt32(&hits); got > tc.maxHits {
				t.Fatalf("expected <= %d hits, got %d", tc.maxHits, got)
			}
		})
	}
}

func TestCloud_MalformedSSEAndPrematureEOFAndEmptyCandidates(t *testing.T) {
	tests := []struct {
		name      string
		handler   http.HandlerFunc
		wantErr   bool
		wantText  string
	}{
		{
			name: "All malformed JSON chunks returns error",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = w.Write([]byte("data: {broken-json\n\n"))
			},
			wantErr: true,
		},
		{
			name: "Empty HTTP 200 body returns error",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				w.WriteHeader(http.StatusOK)
			},
			wantErr: true,
		},
		{
			name: "Premature EOF mid-chunked response returns error",
			handler: func(w http.ResponseWriter, r *http.Request) {
				hj, ok := w.(http.Hijacker)
				if !ok {
					http.Error(w, "hijack unsupported", http.StatusInternalServerError)
					return
				}
				conn, buf, err := hj.Hijack()
				if err != nil {
					return
				}
				_, _ = buf.WriteString("HTTP/1.1 200 OK\r\nContent-Type: text/event-stream\r\nTransfer-Encoding: chunked\r\n\r\n")
				chunk := `data: {"candidates":[{"content":{"parts":[{"text":"hi"}]}}]}` + "\n\n"
				// Write valid chunk header+body, then truncate connection without terminating chunk 0\r\n\r\n.
				_, _ = buf.WriteString("42\r\n" + chunk + "\r\n")
				_ = buf.Flush()
				_ = conn.Close()
			},
			wantErr: true,
		},
		{
			name: "Empty candidates chunk followed by valid text succeeds without panic",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = w.Write([]byte("data: {\"candidates\":[]}\n\n"))
				_, _ = w.Write([]byte("data:{\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"ok\"}]}}]}\n\n"))
			},
			wantErr:  false,
			wantText: "ok",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			isolateCloudTestEnv(t)
			srv := httptest.NewServer(tc.handler)
			defer srv.Close()

			// Exercise nil HTTPClient fallback too.
			client := &Client{
				BaseURL: srv.URL,
				APIKey:  "test-key",
			}

			var out bytes.Buffer
			res, err := client.GenerateWithTools(context.Background(), "gemini-3.8-flash", "test", nil, nil, &out)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil (res=%+v)", res)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if res.Text != tc.wantText {
				t.Fatalf("expected text %q, got %q", tc.wantText, res.Text)
			}
		})
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestCloud_ConcurrentGenerateWithTools_Race(t *testing.T) {
	isolateCloudTestEnv(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(`data: {"candidates":[{"content":{"parts":[{"text":"ok"}]}}]}` + "\n\n"))
	}))
	defer srv.Close()

	// Share one Client with VertexCreds + local RoundTripper across concurrent goroutines
	// to prove isVertexMode never mutates c.BearerToken or races, with zero external network calls.
	vertexClient := &Client{
		HTTPClient: &http.Client{
			Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				body := io.NopCloser(strings.NewReader(`data: {"candidates":[{"content":{"parts":[{"text":"ok"}]}}]}` + "\n\n"))
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
					Body:       body,
					Request:    req,
				}, nil
			}),
		},
		APIKey: "test-key",
		VertexCreds: &authutil.VertexCredentials{
			Mode:      "standard",
			Location:  "us-central1",
			ProjectID: "local-test-proj",
			Token:     &oauth2.Token{AccessToken: "tok-123"},
		},
	}

	localClient := &Client{
		BaseURL:    srv.URL,
		HTTPClient: srv.Client(),
		APIKey:     "test-key",
	}

	const workers = 12
	var wg sync.WaitGroup
	tools := []mcp.Tool{{Name: "ping", InputSchema: mcp.Schema{Type: "object"}}}

	bearerClient := &Client{
		BaseURL:     srv.URL,
		HTTPClient:  srv.Client(),
		BearerToken: "bearer-tok",
	}

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			var activeClient *Client
			switch id % 3 {
			case 0:
				activeClient = localClient
			case 1:
				activeClient = bearerClient
			case 2:
				activeClient = vertexClient
			}
			for j := 0; j < 4; j++ {
				var out bytes.Buffer
				res, err := activeClient.GenerateWithTools(context.Background(), "gemini-3.8-flash", "hi", nil, tools, &out)
				if err != nil {
					t.Errorf("concurrent GenerateWithTools error: %v", err)
					return
				}
				if res.Text != "ok" {
					t.Errorf("expected 'ok', got %q", res.Text)
					return
				}
			}
		}(i)
	}
	wg.Wait()
}

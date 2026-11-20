package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestSendRequestHonorsContextCancellation(t *testing.T) {
	client := NewClient("", nil)
	client.stdin = failingWriteCloser{}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	_, err := client.sendRequest(ctx, "test", nil)
	if err == nil {
		t.Fatal("expected request to fail when MCP context is cancelled")
	}
}

type failingWriteCloser struct{}

func (failingWriteCloser) Write([]byte) (int, error) { return 0, nil }
func (failingWriteCloser) Close() error              { return nil }

type errWriteCloser struct{ err error }

func (e errWriteCloser) Write([]byte) (int, error) { return 0, e.err }
func (e errWriteCloser) Close() error              { return nil }

// TestHelperMCPProcess serves as a local stdio MCP server subprocess for testing.
func TestHelperMCPProcess(t *testing.T) {
	mode := os.Getenv("GO_WANT_MCP_HELPER")
	if mode == "" {
		return
	}
	defer os.Exit(0)

	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 64*1024), 16*1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		var req Request
		if err := json.Unmarshal(line, &req); err != nil {
			continue
		}

		switch req.Method {
		case "initialize":
			resp := map[string]interface{}{
				"jsonrpc": "2.0",
				"id":      req.ID,
				"result":  map[string]interface{}{"protocolVersion": "2024-11-05"},
			}
			b, _ := json.Marshal(resp)
			fmt.Println(string(b))

		case "tools/list":
			if mode == "crash_after_init" {
				os.Exit(1)
			}
			desc := "small tool"
			if mode == "huge_frame" {
				// >256KB payload (well past default 64KB bufio.Scanner limit)
				desc = strings.Repeat("A", 300*1024)
			}
			if mode == "malformed_prefix" {
				// Inject garbage, partial JSON, notification, and a server request with matching ID
				fmt.Println("SERVER LOG: starting tools/list")
				fmt.Println(`{"jsonrpc":"2.0","id":` + fmt.Sprintf("%d", req.ID) + `,"broken":`)
				fmt.Println(`{"jsonrpc":"2.0","method":"notifications/tools/list_changed"}`)
				fmt.Printf(`{"jsonrpc":"2.0","id":%d,"method":"ping"}`+"\n", req.ID)
			}
			resp := map[string]interface{}{
				"jsonrpc": "2.0",
				"id":      req.ID,
				"result": map[string]interface{}{
					"tools": []Tool{{Name: "local_echo", Description: desc}},
				},
			}
			b, _ := json.Marshal(resp)
			fmt.Println(string(b))

		case "resources/list":
			resp := map[string]interface{}{
				"jsonrpc": "2.0",
				"id":      req.ID,
				"result": map[string]interface{}{
					"resources": []Resource{{URI: "file:///local/note.txt", Name: "note"}},
				},
			}
			b, _ := json.Marshal(resp)
			fmt.Println(string(b))

		case "tools/call":
			if mode == "crash_after_init" {
				os.Exit(2)
			}
			resp := map[string]interface{}{
				"jsonrpc": "2.0",
				"id":      req.ID,
				"result": map[string]interface{}{
					"content": []Content{{Type: "text", Text: "ok"}},
				},
			}
			b, _ := json.Marshal(resp)
			fmt.Println(string(b))
		}
	}
}

func newSubprocessMCPClient(t *testing.T, mode string) *Client {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	c := NewClient(exe, []string{"-test.run=^TestHelperMCPProcess$"})
	c.SetEnv(map[string]string{"GO_WANT_MCP_HELPER": mode})
	return c
}

func TestMCPWriteErrorDoesNotReturnNilError(t *testing.T) {
	client := NewClient("", nil)
	client.stdin = errWriteCloser{err: errors.New("broken pipe")}

	_, err := client.sendRequest(context.Background(), "tools/list", nil)
	if err == nil {
		t.Fatal("expected write error to be returned, got nil")
	}
	if !strings.Contains(err.Error(), "broken pipe") {
		t.Fatalf("expected broken pipe error, got: %v", err)
	}
}

func TestMCPStdioLargeFrameOver64KB(t *testing.T) {
	for _, size := range []int{128 * 1024, 512 * 1024, 2 * 1024 * 1024} {
		size := size
		t.Run(fmt.Sprintf("%d_bytes", size), func(t *testing.T) {
			inReader, inWriter := io.Pipe()
			outReader, outWriter := io.Pipe()
			defer inReader.Close()
			defer outWriter.Close()

			c := NewClient("", nil)
			c.stdin = inWriter
			c.stdout = outReader
			go c.readLoop()

			go func() {
				scanner := bufio.NewScanner(inReader)
				for scanner.Scan() {
					var req Request
					if err := json.Unmarshal(scanner.Bytes(), &req); err != nil {
						continue
					}
					payload := strings.Repeat("x", size)
					resp := map[string]interface{}{
						"jsonrpc": "2.0",
						"id":      req.ID,
						"result": map[string]interface{}{
							"tools": []Tool{{Name: "big_tool", Description: payload}},
						},
					}
					b, _ := json.Marshal(resp)
					_, _ = outWriter.Write(append(b, '\n'))
				}
			}()

			tools, err := c.ListTools()
			if err != nil {
				t.Fatalf("ListTools failed on %d byte frame: %v", size, err)
			}
			if len(tools) != 1 || len(tools[0].Description) != size {
				t.Fatalf("expected tool description len %d, got %+v", size, len(tools))
			}
			c.Stop()
		})
	}
}

func TestMCPStdioSubprocessDiscoveryAndHugeFrame(t *testing.T) {
	c := newSubprocessMCPClient(t, "huge_frame")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	summary, err := c.Inspect(ctx)
	if err != nil {
		t.Fatalf("Inspect failed: %v", err)
	}
	if len(summary.Tools) != 1 || len(summary.Tools[0].Description) != 300*1024 {
		t.Fatalf("expected 300KB tool frame, got tools=%d", len(summary.Tools))
	}
}

func TestMCPStdioMalformedAndSpoofedJSONLines(t *testing.T) {
	c := newSubprocessMCPClient(t, "malformed_prefix")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := c.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer c.Stop()

	if err := c.Initialize(); err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}

	tools, err := c.ListTools()
	if err != nil {
		t.Fatalf("ListTools failed after malformed/spoofed lines: %v", err)
	}
	if len(tools) != 1 || tools[0].Name != "local_echo" {
		t.Fatalf("expected real tools/list response, got: %+v", tools)
	}
}

func TestMCPUnexpectedEOFUnblocksAllPendingWaiters(t *testing.T) {
	inReader, inWriter := io.Pipe()
	outReader, outWriter := io.Pipe()
	defer inReader.Close()

	c := NewClient("", nil)
	c.stdin = inWriter
	c.stdout = outReader
	go c.readLoop()

	// Drain stdin asynchronously so writes don't block
	go func() {
		_, _ = io.Copy(io.Discard, inReader)
	}()

	const waiters = 10
	errs := make(chan error, waiters)
	var wg sync.WaitGroup
	for i := 0; i < waiters; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_, err := c.sendRequest(ctx, fmt.Sprintf("op_%d", idx), nil)
			errs <- err
		}(i)
	}

	// Give goroutines time to register pending waiters, then crash stdout with partial line
	time.Sleep(50 * time.Millisecond)
	_, _ = outWriter.Write([]byte(`{"jsonrpc":"2.0","id":1,"partial":`))
	_ = outWriter.Close()

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("pending waiters deadlocked/timed out instead of unblocking on EOF")
	}
	close(errs)
	for err := range errs {
		if err == nil {
			t.Fatal("expected error when stream closed with EOF, got nil")
		}
	}

	// Subsequent sendRequest after EOF must also fail fast (not block 30s!)
	start := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if _, err := c.sendRequest(ctx, "after_eof", nil); err == nil {
		t.Fatal("expected sendRequest after EOF to return error")
	}
	if time.Since(start) > 500*time.Millisecond {
		t.Fatalf("sendRequest after EOF took %v instead of failing fast", time.Since(start))
	}
}

func TestMCPSubprocessCrashUnblocksPendingWaiters(t *testing.T) {
	c := newSubprocessMCPClient(t, "crash_after_init")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := c.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer c.Stop()

	if err := c.Initialize(); err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}

	start := time.Now()
	_, err := c.ListTools()
	if err == nil {
		t.Fatal("expected ListTools to fail when subprocess crashed")
	}
	if time.Since(start) > 3*time.Second {
		t.Fatalf("ListTools took too long (%v) on subprocess crash", time.Since(start))
	}
}

func TestMCPConcurrentStopAndCancellationRace(t *testing.T) {
	runtime.GC()
	baseline := runtime.NumGoroutine()

	for round := 0; round < 10; round++ {
		c := newSubprocessMCPClient(t, "normal")
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		if err := c.Start(ctx); err != nil {
			cancel()
			t.Fatal(err)
		}

		var wg sync.WaitGroup
		for i := 0; i < 6; i++ {
			wg.Add(1)
			go func(idx int) {
				defer wg.Done()
				reqCtx, reqCancel := context.WithTimeout(ctx, 100*time.Millisecond)
				defer reqCancel()
				_, _ = c.sendRequest(reqCtx, "tools/list", nil)
			}(i)
		}
		wg.Add(2)
		go func() {
			defer wg.Done()
			time.Sleep(5 * time.Millisecond)
			c.Stop()
		}()
		go func() {
			defer wg.Done()
			time.Sleep(5 * time.Millisecond)
			c.Stop()
		}()

		wg.Wait()
		cancel()
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		runtime.Gosched()
		if runtime.NumGoroutine() <= baseline+4 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestMCPHTTPClientErrorHandling(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req Request
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad json", http.StatusBadRequest)
			return
		}
		if req.Method == "fail_500" {
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}
		resp := Response{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result:  json.RawMessage(`{"tools":[{"name":"http_tool"}]}`),
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	c := NewHTTPClient(srv.URL, map[string]string{"X-Test": "1"})
	tools, err := c.ListTools()
	if err != nil {
		t.Fatalf("HTTP ListTools failed: %v", err)
	}
	if len(tools) != 1 || tools[0].Name != "http_tool" {
		t.Fatalf("unexpected tools: %+v", tools)
	}

	if _, err := c.sendRequest(context.Background(), "fail_500", nil); err == nil {
		t.Fatal("expected HTTP 500 to return error")
	}
}

func TestMCPInspectHonorsCanceledContext(t *testing.T) {
	c := newSubprocessMCPClient(t, "crash_after_init")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Inspect hits crash_after_init on tools/list -> must NOT report READY when connection died.
	if _, err := c.Inspect(ctx); err == nil {
		t.Fatal("expected Inspect to return error when MCP server crashes during discovery")
	}

	// Pre-canceled context must fail immediately.
	c2 := newSubprocessMCPClient(t, "normal")
	canceledCtx, cancel2 := context.WithCancel(context.Background())
	cancel2()
	if _, err := c2.Inspect(canceledCtx); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled from Inspect, got: %v", err)
	}
}

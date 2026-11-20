# Streaming Responses Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Modify the LLM clients (`ollama` and `cloud`) to support streaming responses so the user sees tokens as they are generated. The clients will write directly to an `io.Writer` while accumulating the final response string for chat history.

**Architecture:** 
- Change `Generate` signatures to `Generate(ctx, model, prompt string, out io.Writer) (string, error)`.
- Update `ollama` client to set `stream: true` and read JSON lines.
- Update `cloud` client to use `streamGenerateContent?alt=sse` and read SSE events.
- Update `cmd/run.go` and `cmd/chat.go` to pass `os.Stdout`.

**Tech Stack:** Go, standard `bufio` and `encoding/json`.

---

### Task 1: Update Ollama Client for Streaming

**Files:**
- Modify: `C:\AgyHut\microgravity\internal\ollama\client.go`
- Modify: `C:\AgyHut\microgravity\internal\ollama\client_test.go`

- [ ] **Step 1: Update client code**
Update `internal/ollama/client.go` to modify `GenerateRequest` (Stream: true) and read the response line by line:
```go
package ollama

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// ... existing Client and NewClient ...

type Client struct {
	BaseURL    string
	HTTPClient *http.Client
}

func NewClient(baseURL string) *Client {
	if baseURL == "" {
		baseURL = "http://localhost:11434"
	}
	return &Client{
		BaseURL:    baseURL,
		HTTPClient: &http.Client{},
	}
}

type GenerateRequest struct {
	Model  string `json:"model"`
	Prompt string `json:"prompt"`
	Stream bool   `json:"stream"`
}

type GenerateResponse struct {
	Response string `json:"response"`
	Done     bool   `json:"done"`
}

func (c *Client) Generate(ctx context.Context, model, prompt string, out io.Writer) (string, error) {
	reqBody := GenerateRequest{
		Model:  model,
		Prompt: prompt,
		Stream: true,
	}
	buf := new(bytes.Buffer)
	if err := json.NewEncoder(buf).Encode(reqBody); err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", c.BaseURL+"/api/generate", buf)
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	var fullResponse strings.Builder
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Bytes()
		var genResp GenerateResponse
		if err := json.Unmarshal(line, &genResp); err == nil {
			fmt.Fprint(out, genResp.Response)
			fullResponse.WriteString(genResp.Response)
			if genResp.Done {
				break
			}
		}
	}
	fmt.Fprintln(out) // Print final newline
	return fullResponse.String(), scanner.Err()
}

// ... existing PullRequest and Pull methods ...
type PullRequest struct {
	Model  string `json:"model"`
	Stream bool   `json:"stream"`
}

func (c *Client) Pull(ctx context.Context, model string) error {
	reqBody := PullRequest{
		Model:  model,
		Stream: false,
	}
	buf := new(bytes.Buffer)
	if err := json.NewEncoder(buf).Encode(reqBody); err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", c.BaseURL+"/api/pull", buf)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status code during pull: %d", resp.StatusCode)
	}

	return nil
}
```

- [ ] **Step 2: Update Tests**
Update `client_test.go` to pass a mock `io.Writer`:
```go
package ollama

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClient_Generate(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-ndjson")
		w.Write([]byte(`{"response": "Hello ", "done": false}` + "\n"))
		w.Write([]byte(`{"response": "from Gemma!", "done": true}` + "\n"))
	}))
	defer server.Close()

	client := NewClient(server.URL)
	var out bytes.Buffer
	response, err := client.Generate(context.Background(), "gemma2", "Hello", &out)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := "Hello from Gemma!"
	if response != expected {
		t.Errorf("expected %q, got %q", expected, response)
	}
	if out.String() != expected+"\n" {
		t.Errorf("expected out %q, got %q", expected+"\n", out.String())
	}
}

func TestClient_Pull(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := NewClient(server.URL)
	err := client.Pull(context.Background(), "gemma2")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
```

- [ ] **Step 3: Commit**
```bash
git add internal/ollama
git commit -m "feat: add streaming support to ollama client"
```

### Task 2: Update Cloud Client for Streaming

**Files:**
- Modify: `C:\AgyHut\microgravity\internal\cloud\client.go`
- Modify: `C:\AgyHut\microgravity\internal\cloud\client_test.go`

- [ ] **Step 1: Update client code**
Update `internal/cloud/client.go` to use SSE:
```go
package cloud

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

type Client struct {
	BaseURL    string
	APIKey     string
	HTTPClient *http.Client
}

func NewClient(apiKey string) *Client {
	return &Client{
		BaseURL:    "https://generativelanguage.googleapis.com",
		APIKey:     apiKey,
		HTTPClient: &http.Client{},
	}
}

type Part struct {
	Text string `json:"text"`
}

type Content struct {
	Parts []Part `json:"parts"`
}

type GenerateRequest struct {
	Contents []Content `json:"contents"`
}

type GenerateResponse struct {
	Candidates []struct {
		Content struct {
			Parts []struct {
				Text string `json:"text"`
			} `json:"parts"`
		} `json:"content"`
	} `json:"candidates"`
}

func (c *Client) Generate(ctx context.Context, model, prompt string, out io.Writer) (string, error) {
	if c.APIKey == "" {
		return "", fmt.Errorf("API key is required for cloud generation")
	}

	reqBody := GenerateRequest{
		Contents: []Content{{Parts: []Part{{Text: prompt}}}},
	}
	buf := new(bytes.Buffer)
	if err := json.NewEncoder(buf).Encode(reqBody); err != nil {
		return "", err
	}

	url := fmt.Sprintf("%s/v1beta/models/%s:streamGenerateContent?alt=sse&key=%s", c.BaseURL, model, c.APIKey)
	req, err := http.NewRequestWithContext(ctx, "POST", url, buf)
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("unexpected status code from cloud: %d", resp.StatusCode)
	}

	var fullResponse strings.Builder
	scanner := bufio.NewScanner(resp.Body)
	
	// Read SSE
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "data: ") {
			data := strings.TrimPrefix(line, "data: ")
			var genResp GenerateResponse
			if err := json.Unmarshal([]byte(data), &genResp); err == nil {
				if len(genResp.Candidates) > 0 && len(genResp.Candidates[0].Content.Parts) > 0 {
					text := genResp.Candidates[0].Content.Parts[0].Text
					fmt.Fprint(out, text)
					fullResponse.WriteString(text)
				}
			}
		}
	}
	fmt.Fprintln(out) // Final newline
	return fullResponse.String(), scanner.Err()
}
```

- [ ] **Step 2: Update Tests**
```go
package cloud

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClient_Generate(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Write([]byte(`data: {"candidates": [{"content": {"parts": [{"text": "Hello "}]}}]}` + "\n\n"))
		w.Write([]byte(`data: {"candidates": [{"content": {"parts": [{"text": "Cloud!"}]}}]}` + "\n\n"))
	}))
	defer server.Close()

	client := NewClient("test-key")
	client.BaseURL = server.URL

	var out bytes.Buffer
	response, err := client.Generate(context.Background(), "gemma", "Hello", &out)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := "Hello Cloud!"
	if response != expected {
		t.Errorf("expected %q, got %q", expected, response)
	}
}
```

- [ ] **Step 3: Commit**
```bash
git add internal/cloud
git commit -m "feat: add streaming sse support to cloud client"
```

### Task 3: Update Commands

**Files:**
- Modify: `C:\AgyHut\microgravity\cmd\run.go`
- Modify: `C:\AgyHut\microgravity\cmd\chat.go`

- [ ] **Step 1: Update cmd/run.go**
Find `ollamaClient.Generate` and `cloudClient.Generate`, pass `os.Stdout`. Remove `fmt.Println(resp)` since it streams now. (Use `multi_replace_file_content` or just overwrite the logic blocks).

- [ ] **Step 2: Update cmd/chat.go**
Pass `os.Stdout` to both `Generate` methods. Remove `fmt.Println("Gemma:", responseText)` because streaming will print it inline.

- [ ] **Step 3: Commit**
```bash
git add cmd/run.go cmd/chat.go
git commit -m "feat: wire streaming output to run and chat commands"
```

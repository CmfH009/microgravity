# Cloud Gemma Integration Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement the cloud execution route by building a REST client for the Gemini Developer API to access Gemma 2 models for complex reasoning.

**Architecture:** We will create an `internal/cloud` package containing an HTTP client that communicates with the `generativelanguage.googleapis.com` endpoint using an API key from the environment. The `cmd/run.go` logic will be updated to replace the stub and perform real cloud calls when the heuristic router categorizes a prompt as `complex`.

**Tech Stack:** Go, standard `net/http` package.

---

### Task 1: Implement Cloud Client

**Files:**
- Create: `C:\AgyHut\microgravity\internal\cloud\client.go`
- Create: `C:\AgyHut\microgravity\internal\cloud\client_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/cloud/client_test.go`:
```go
package cloud

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClient_Generate(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1beta/models/gemma-2-9b-it:generateContent" {
			t.Errorf("expected path /v1beta/models/gemma-2-9b-it:generateContent, got %s", r.URL.Path)
		}
		if r.URL.Query().Get("key") != "test-key" {
			t.Errorf("expected key test-key, got %s", r.URL.Query().Get("key"))
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"candidates": [{"content": {"parts": [{"text": "Hello from Cloud Gemma!"}]}}]}`))
	}))
	defer server.Close()

	client := NewClient("test-key")
	client.BaseURL = server.URL // Override for testing

	response, err := client.Generate(context.Background(), "gemma-2-9b-it", "Hello")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if response != "Hello from Cloud Gemma!" {
		t.Errorf("expected 'Hello from Cloud Gemma!', got '%s'", response)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cloud/... -v`
Expected: FAIL (client not defined)

- [ ] **Step 3: Write minimal implementation**

Create `internal/cloud/client.go`:
```go
package cloud

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
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

// Request and Response structures based on the Gemini API specs
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

func (c *Client) Generate(ctx context.Context, model, prompt string) (string, error) {
	if c.APIKey == "" {
		return "", fmt.Errorf("API key is required for cloud generation")
	}

	reqBody := GenerateRequest{
		Contents: []Content{
			{
				Parts: []Part{
					{Text: prompt},
				},
			},
		},
	}
	buf := new(bytes.Buffer)
	if err := json.NewEncoder(buf).Encode(reqBody); err != nil {
		return "", err
	}

	url := fmt.Sprintf("%s/v1beta/models/%s:generateContent?key=%s", c.BaseURL, model, c.APIKey)
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

	var genResp GenerateResponse
	if err := json.NewDecoder(resp.Body).Decode(&genResp); err != nil {
		return "", err
	}

	if len(genResp.Candidates) == 0 || len(genResp.Candidates[0].Content.Parts) == 0 {
		return "", fmt.Errorf("empty response from cloud")
	}

	return genResp.Candidates[0].Content.Parts[0].Text, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/cloud/... -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/cloud
git commit -m "feat: add cloud client for Gemma API"
```

### Task 2: Wire Cloud Client in run command

**Files:**
- Modify: `C:\AgyHut\microgravity\cmd\run.go`

- [ ] **Step 1: Add cloud configuration flags and logic**

Modify `cmd/run.go` to import the cloud package, add a flag for the cloud model, retrieve the API key from the environment, and replace the stubbed complex logic.

Update `cmd/run.go`:
```go
package cmd

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/purav/microgravity/internal/cloud"
	"github.com/purav/microgravity/internal/ollama"
	"github.com/purav/microgravity/internal/router"
	"github.com/purav/microgravity/internal/updater"
)

var (
	modelName  string
	baseURL    string
	cloudModel string
)

var runCmd = &cobra.Command{
	Use:   "run [prompt]",
	Short: "Run a prompt through the Gemma CLI model router",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		prompt := args[0]

		// Instantiate clients
		ollamaClient := ollama.NewClient(baseURL)
		r := router.NewRouter()

		// Start background updater daemon
		daemon := updater.NewDaemon(ollamaClient, modelName)
		daemon.Start()
		defer daemon.Stop()

		complexity := r.Classify(prompt)
		fmt.Printf("[Router] Classifying prompt complexity: %s\n", complexity)

		if complexity == router.ComplexitySimple {
			fmt.Printf("[Router] Routing to local model (%s)...\n", modelName)
			resp, err := ollamaClient.Generate(context.Background(), modelName, prompt)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error generating locally: %v\n", err)
				os.Exit(1)
			}
			fmt.Println(resp)
		} else {
			fmt.Printf("[Router] Routing to Google Cloud (%s)...\n", cloudModel)
			apiKey := os.Getenv("GOOGLE_API_KEY")
			if apiKey == "" {
				fmt.Fprintln(os.Stderr, "Error: GOOGLE_API_KEY environment variable is not set for complex tasks")
				os.Exit(1)
			}
			cloudClient := cloud.NewClient(apiKey)
			resp, err := cloudClient.Generate(context.Background(), cloudModel, prompt)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error generating from cloud: %v\n", err)
				os.Exit(1)
			}
			fmt.Println(resp)
		}
	},
}

func init() {
	runCmd.Flags().StringVarP(&modelName, "model", "m", "gemma2:2b", "Ollama model name to use")
	runCmd.Flags().StringVarP(&baseURL, "url", "u", "http://localhost:11434", "Ollama API base URL")
	runCmd.Flags().StringVarP(&cloudModel, "cloud-model", "c", "gemma-2-9b-it", "Cloud Gemma model to use")
	rootCmd.AddCommand(runCmd)
}
```

- [ ] **Step 2: Commit**

```bash
git add cmd/run.go
git commit -m "feat: wire cloud client to router for complex tasks"
```

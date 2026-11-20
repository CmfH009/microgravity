# Model Context Protocol (MCP) Integration Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement a lightweight MCP client in Go that communicates via JSON-RPC over Stdio to dynamically load tools from external servers and make them available to the CLI.

**Architecture:** We will create an `internal/mcp` package with a JSON-RPC client capable of spawning an external MCP server process (e.g., a node script or python binary), initializing the connection, retrieving a list of tools, and executing a tool.

**Tech Stack:** Go, standard `os/exec` and `encoding/json` packages.

---

### Task 1: Define MCP Protocol Types and JSON-RPC Wrapper

**Files:**
- Create: `C:\AgyHut\microgravity\internal\mcp\protocol.go`

- [ ] **Step 1: Write MCP Types and JSON-RPC Structs**
Create `internal/mcp/protocol.go`:
```go
package mcp

import "encoding/json"

// JSON-RPC Basics
type Request struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      int         `json:"id"`
	Method  string      `json:"method"`
	Params  interface{} `json:"params,omitempty"`
}

type Response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int             `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *Error          `json:"error,omitempty"`
}

type Error struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// MCP Specifics
type InitializeParams struct {
	ClientInfo ClientInfo `json:"clientInfo"`
}

type ClientInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type Tool struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	InputSchema Schema `json:"inputSchema"`
}

type Schema struct {
	Type       string                 `json:"type"`
	Properties map[string]interface{} `json:"properties"`
	Required   []string               `json:"required,omitempty"`
}

type ListToolsResult struct {
	Tools []Tool `json:"tools"`
}

type CallToolParams struct {
	Name      string                 `json:"name"`
	Arguments map[string]interface{} `json:"arguments"`
}

type CallToolResult struct {
	Content []Content `json:"content"`
	IsError bool      `json:"isError,omitempty"`
}

type Content struct {
	Type string `json:"type"`
	Text string `json:"text"`
}
```

- [ ] **Step 2: Commit**
```bash
git add internal/mcp
git commit -m "feat: define MCP json-rpc types and schemas"
```

### Task 2: Implement Stdio MCP Client

**Files:**
- Create: `C:\AgyHut\microgravity\internal\mcp\client.go`
- Create: `C:\AgyHut\microgravity\internal\mcp\client_test.go`

- [ ] **Step 1: Write the failing test**
Create `internal/mcp/client_test.go`:
```go
package mcp

import (
	"context"
	"testing"
)

// In a real scenario we'd mock the os/exec.Cmd stdio pipes.
// Here we just test instantiation and interface adherence.
func TestNewClient(t *testing.T) {
	client := NewClient("dummy", []string{"arg1"})
	if client == nil {
		t.Fatal("expected client to be instantiated")
	}
	if client.command != "dummy" {
		t.Errorf("expected dummy command, got %s", client.command)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**
Run: `go test ./internal/mcp/... -v`
Expected: FAIL

- [ ] **Step 3: Write implementation**
Create `internal/mcp/client.go`:
```go
package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"sync"
)

type Client struct {
	command string
	args    []string
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	stdout  io.ReadCloser
	
	reqID     int
	reqMutex  sync.Mutex
	pending   map[int]chan Response
	pendMutex sync.Mutex
}

func NewClient(command string, args []string) *Client {
	return &Client{
		command: command,
		args:    args,
		pending: make(map[int]chan Response),
	}
}

func (c *Client) Start(ctx context.Context) error {
	c.cmd = exec.CommandContext(ctx, c.command, c.args...)
	
	stdin, err := c.cmd.StdinPipe()
	if err != nil {
		return err
	}
	c.stdin = stdin

	stdout, err := c.cmd.StdoutPipe()
	if err != nil {
		return err
	}
	c.stdout = stdout

	if err := c.cmd.Start(); err != nil {
		return err
	}

	go c.readLoop()
	return nil
}

func (c *Client) Stop() {
	if c.cmd != nil && c.cmd.Process != nil {
		c.cmd.Process.Kill()
	}
}

func (c *Client) readLoop() {
	scanner := bufio.NewScanner(c.stdout)
	for scanner.Scan() {
		line := scanner.Bytes()
		var resp Response
		if err := json.Unmarshal(line, &resp); err == nil {
			c.pendMutex.Lock()
			if ch, ok := c.pending[resp.ID]; ok {
				ch <- resp
				delete(c.pending, resp.ID)
			}
			c.pendMutex.Unlock()
		}
	}
}

func (c *Client) sendRequest(method string, params interface{}) (*Response, error) {
	c.reqMutex.Lock()
	c.reqID++
	id := c.reqID
	c.reqMutex.Unlock()

	req := Request{
		JSONRPC: "2.0",
		ID:      id,
		Method:  method,
		Params:  params,
	}

	reqBytes, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	reqBytes = append(reqBytes, '\n')

	ch := make(chan Response, 1)
	c.pendMutex.Lock()
	c.pending[id] = ch
	c.pendMutex.Unlock()

	if _, err := c.stdin.Write(reqBytes); err != nil {
		c.pendMutex.Lock()
		delete(c.pending, id)
		c.pendMutex.Unlock()
		return nil, err
	}

	resp := <-ch
	if resp.Error != nil {
		return nil, fmt.Errorf("rpc error: %s (code %d)", resp.Error.Message, resp.Error.Code)
	}
	return &resp, nil
}

func (c *Client) Initialize() error {
	params := InitializeParams{
		ClientInfo: ClientInfo{Name: "microgravity", Version: "1.0.0"},
	}
	_, err := c.sendRequest("initialize", params)
	return err
}

func (c *Client) ListTools() ([]Tool, error) {
	resp, err := c.sendRequest("tools/list", nil)
	if err != nil {
		return nil, err
	}
	var res ListToolsResult
	if err := json.Unmarshal(resp.Result, &res); err != nil {
		return nil, err
	}
	return res.Tools, nil
}

func (c *Client) CallTool(name string, args map[string]interface{}) ([]Content, error) {
	params := CallToolParams{Name: name, Arguments: args}
	resp, err := c.sendRequest("tools/call", params)
	if err != nil {
		return nil, err
	}
	var res CallToolResult
	if err := json.Unmarshal(resp.Result, &res); err != nil {
		return nil, err
	}
	return res.Content, nil
}
```

- [ ] **Step 4: Run test to verify it passes**
Run: `go test ./internal/mcp/... -v`
Expected: PASS

- [ ] **Step 5: Commit**
```bash
git add internal/mcp
git commit -m "feat: implement mcp stdio client"
```

### Task 3: Wire MCP Client in CLI

**Files:**
- Modify: `C:\AgyHut\microgravity\cmd\run.go`

- [ ] **Step 1: Add mcp server config flag and init logic**
Modify `cmd/run.go` to add an `--mcp-server` flag. If provided, the CLI will spawn it, initialize MCP, print out available tools, and pass them as context to the router log (for now, actual integration into the LLM system prompt will be fully realized in the Chat REPL phase).

Update `cmd/run.go`:
```go
package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/purav/microgravity/internal/cloud"
	"github.com/purav/microgravity/internal/mcp"
	"github.com/purav/microgravity/internal/ollama"
	"github.com/purav/microgravity/internal/router"
	"github.com/purav/microgravity/internal/updater"
)

var (
	modelName  string
	baseURL    string
	cloudModel string
	mcpServer  string
)

var runCmd = &cobra.Command{
	Use:   "run [prompt]",
	Short: "Run a prompt through the Gemma CLI model router",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		prompt := args[0]
		ctx := context.Background()

		// Optional MCP Initialization
		if mcpServer != "" {
			parts := strings.Split(mcpServer, " ")
			mcpClient := mcp.NewClient(parts[0], parts[1:])
			if err := mcpClient.Start(ctx); err != nil {
				fmt.Fprintf(os.Stderr, "Failed to start MCP server: %v\n", err)
			} else {
				defer mcpClient.Stop()
				if err := mcpClient.Initialize(); err != nil {
					fmt.Fprintf(os.Stderr, "Failed to initialize MCP: %v\n", err)
				} else {
					tools, _ := mcpClient.ListTools()
					fmt.Printf("[MCP] Connected. Available tools: %d\n", len(tools))
					for _, t := range tools {
						fmt.Printf("      - %s: %s\n", t.Name, t.Description)
					}
				}
			}
		}

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
			resp, err := ollamaClient.Generate(ctx, modelName, prompt)
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
			resp, err := cloudClient.Generate(ctx, cloudModel, prompt)
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
	runCmd.Flags().StringVarP(&mcpServer, "mcp-server", "s", "", "Command to start an MCP stdio server (e.g., 'node server.js')")
	rootCmd.AddCommand(runCmd)
}
```

- [ ] **Step 2: Commit**
```bash
git add cmd/run.go
git commit -m "feat: wire MCP client in run command to discover tools"
```

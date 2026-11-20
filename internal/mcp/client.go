package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

type Client struct {
	command string
	args    []string
	url     string
	headers map[string]string
	env     map[string]string
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	stdout  io.ReadCloser
	writeMu sync.Mutex

	reqID     int
	reqMutex  sync.Mutex
	pending   map[int]chan Response
	pendMutex sync.Mutex
}

func NewClient(command string, args []string) *Client {
	if strings.HasPrefix(command, "http://") || strings.HasPrefix(command, "https://") {
		return NewHTTPClient(command, nil)
	}
	return &Client{
		command: command,
		args:    args,
		pending: make(map[int]chan Response),
	}
}

func NewHTTPClient(url string, headers map[string]string) *Client {
	if headers == nil {
		headers = make(map[string]string)
	}
	return &Client{
		url:     url,
		headers: headers,
		pending: make(map[int]chan Response),
	}
}

func (c *Client) SetEnv(env map[string]string) {
	c.env = env
}

func (c *Client) SetHeaders(headers map[string]string) {
	c.headers = headers
}

func (c *Client) Start(ctx context.Context) error {
	if c.url != "" {
		// HTTP client does not need process spawning
		return nil
	}

	c.cmd = exec.CommandContext(ctx, c.command, c.args...)
	if len(c.env) > 0 {
		c.cmd.Env = os.Environ()
		for k, v := range c.env {
			c.cmd.Env = append(c.cmd.Env, fmt.Sprintf("%s=%s", k, v))
		}
	}

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
		_ = c.cmd.Process.Kill()
	}
}

func (c *Client) readLoop() {
	if c.stdout == nil {
		return
	}
	defer func() {
		c.pendMutex.Lock()
		defer c.pendMutex.Unlock()
		for id, ch := range c.pending {
			ch <- Response{ID: id, Error: &Error{Code: -32000, Message: "MCP server connection closed"}}
			delete(c.pending, id)
		}
	}()
	scanner := bufio.NewScanner(c.stdout)
	scanBuf := make([]byte, 1024*1024)
	scanner.Buffer(scanBuf, 50*1024*1024) // 50MB max capacity for large tool outputs
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

func (c *Client) sendRequest(ctx context.Context, method string, params interface{}) (*Response, error) {
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

	if c.url != "" {
		requestCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()

		httpReq, err := http.NewRequestWithContext(requestCtx, "POST", c.url, bytes.NewReader(reqBytes))
		if err != nil {
			return nil, err
		}
		httpReq.Header.Set("Content-Type", "application/json")
		for k, v := range c.headers {
			httpReq.Header.Set(k, v)
		}

		httpClient := &http.Client{Timeout: 30 * time.Second}
		resp, err := httpClient.Do(httpReq)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()

		var r Response
		if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
			return nil, err
		}
		if r.Error != nil {
			return nil, fmt.Errorf("rpc error: %s (code %d)", r.Error.Message, r.Error.Code)
		}
		return &r, nil
	}

	reqBytes = append(reqBytes, '\n')
	ch := make(chan Response, 1)
	c.pendMutex.Lock()
	c.pending[id] = ch
	c.pendMutex.Unlock()

	c.writeMu.Lock()
	_, writeErr := c.stdin.Write(reqBytes)
	c.writeMu.Unlock()
	if writeErr != nil {
		c.pendMutex.Lock()
		delete(c.pending, id)
		c.pendMutex.Unlock()
		return nil, err
	}

	var resp Response
	select {
	case resp = <-ch:
	case <-ctx.Done():
		c.pendMutex.Lock()
		delete(c.pending, id)
		c.pendMutex.Unlock()
		return nil, ctx.Err()
	case <-time.After(30 * time.Second):
		c.pendMutex.Lock()
		delete(c.pending, id)
		c.pendMutex.Unlock()
		return nil, fmt.Errorf("MCP request %d timed out", id)
	}
	if resp.Error != nil {
		return nil, fmt.Errorf("rpc error: %s (code %d)", resp.Error.Message, resp.Error.Code)
	}
	return &resp, nil
}

func (c *Client) Initialize() error {
	params := InitializeParams{
		ClientInfo: ClientInfo{Name: "microgravity", Version: "1.1.0"},
	}
	_, err := c.sendRequest(context.Background(), "initialize", params)
	return err
}

func (c *Client) ListTools() ([]Tool, error) {
	resp, err := c.sendRequest(context.Background(), "tools/list", nil)
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
	resp, err := c.sendRequest(context.Background(), "tools/call", params)
	if err != nil {
		return nil, err
	}
	var res CallToolResult
	if err := json.Unmarshal(resp.Result, &res); err != nil {
		return nil, err
	}
	return res.Content, nil
}

func (c *Client) ListResources() ([]Resource, error) {
	resp, err := c.sendRequest(context.Background(), "resources/list", nil)
	if err != nil {
		return nil, err
	}
	var res ListResourcesResult
	if err := json.Unmarshal(resp.Result, &res); err != nil {
		return nil, err
	}
	return res.Resources, nil
}

func (c *Client) ReadResource(uri string) ([]ResourceContent, error) {
	params := ReadResourceParams{URI: uri}
	resp, err := c.sendRequest(context.Background(), "resources/read", params)
	if err != nil {
		return nil, err
	}
	var res ReadResourceResult
	if err := json.Unmarshal(resp.Result, &res); err != nil {
		return nil, err
	}
	return res.Contents, nil
}

type InspectSummary struct {
	Command   string     `json:"command"`
	Tools     []Tool     `json:"tools"`
	Resources []Resource `json:"resources"`
	Status    string     `json:"status"`
}

func (c *Client) Inspect(ctx context.Context) (*InspectSummary, error) {
	if err := c.Start(ctx); err != nil {
		return nil, fmt.Errorf("failed to start mcp server process: %w", err)
	}
	defer c.Stop()

	if err := c.Initialize(); err != nil {
		return nil, fmt.Errorf("failed to initialize mcp connection: %w", err)
	}

	tools, err := c.ListTools()
	if err != nil {
		tools = []Tool{}
	}

	resources, err := c.ListResources()
	if err != nil {
		resources = []Resource{}
	}

	cmdLabel := c.command
	if c.url != "" {
		cmdLabel = c.url
	}

	return &InspectSummary{
		Command:   cmdLabel,
		Tools:     tools,
		Resources: resources,
		Status:    "READY",
	}, nil
}

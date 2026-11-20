package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

const (
	defaultRequestTimeout = 30 * time.Second
	maxStdioFrameBytes    = 50 * 1024 * 1024 // 50MB max capacity for large tool outputs
)

type rpcIncoming struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      *int            `json:"id"`
	Method  string          `json:"method,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *Error          `json:"error,omitempty"`
}

type Client struct {
	mu      sync.Mutex
	command string
	args    []string
	url     string
	headers map[string]string
	env     map[string]string

	cmd        *exec.Cmd
	stdin      io.WriteCloser
	stdout     io.ReadCloser
	started    bool
	readDone   chan struct{}
	httpClient *http.Client
	writeMu    sync.Mutex

	reqID     int
	reqMutex  sync.Mutex
	pending   map[int]chan Response
	pendMutex sync.Mutex
	closed    bool
	closeErr  error
	closeCh   chan struct{}
}

func NewClient(command string, args []string) *Client {
	if strings.HasPrefix(command, "http://") || strings.HasPrefix(command, "https://") {
		return NewHTTPClient(command, nil)
	}
	return &Client{
		command: command,
		args:    append([]string{}, args...),
		pending: make(map[int]chan Response),
		closeCh: make(chan struct{}),
	}
}

func NewHTTPClient(url string, headers map[string]string) *Client {
	hdrCopy := make(map[string]string, len(headers))
	for k, v := range headers {
		hdrCopy[k] = v
	}
	return &Client{
		url:        url,
		headers:    hdrCopy,
		pending:    make(map[int]chan Response),
		closeCh:    make(chan struct{}),
		httpClient: &http.Client{Timeout: defaultRequestTimeout},
	}
}

func (c *Client) SetEnv(env map[string]string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if env == nil {
		c.env = nil
		return
	}
	cp := make(map[string]string, len(env))
	for k, v := range env {
		cp[k] = v
	}
	c.env = cp
}

func (c *Client) SetHeaders(headers map[string]string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	cp := make(map[string]string, len(headers))
	for k, v := range headers {
		cp[k] = v
	}
	c.headers = cp
}

func (c *Client) Start(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if c.url != "" {
		// HTTP client does not need process spawning
		return nil
	}

	c.pendMutex.Lock()
	closed := c.closed
	c.pendMutex.Unlock()
	if closed {
		return fmt.Errorf("MCP client is closed")
	}

	c.mu.Lock()
	if c.started {
		c.mu.Unlock()
		return fmt.Errorf("MCP client already started")
	}

	cmd := exec.CommandContext(ctx, c.command, c.args...)
	if len(c.env) > 0 {
		cmd.Env = os.Environ()
		for k, v := range c.env {
			cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", k, v))
		}
	}

	stdin, err := cmd.StdinPipe()
	if err != nil {
		c.mu.Unlock()
		return err
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		c.mu.Unlock()
		return err
	}

	if err := cmd.Start(); err != nil {
		_ = stdin.Close()
		_ = stdout.Close()
		c.mu.Unlock()
		return err
	}

	readDone := make(chan struct{})
	c.cmd = cmd
	c.stdin = stdin
	c.stdout = stdout
	c.started = true
	c.readDone = readDone
	c.mu.Unlock()

	go c.runReadLoop(stdout, cmd, readDone)
	return nil
}

// Stop terminates the MCP client connection and releases process resources.
func (c *Client) Stop() {
	_ = c.Close()
}

// Close closes the MCP client idempotently, unblocks all pending waiters, and reaps any child process.
func (c *Client) Close() error {
	c.closeWaiters(errors.New("MCP server connection closed"))

	c.mu.Lock()
	stdin := c.stdin
	c.stdin = nil
	cmd := c.cmd
	readDone := c.readDone
	c.mu.Unlock()

	if stdin != nil {
		c.writeMu.Lock()
		_ = stdin.Close()
		c.writeMu.Unlock()
	}

	if cmd != nil && cmd.Process != nil {
		_ = cmd.Process.Kill()
	}

	if readDone != nil {
		timer := time.NewTimer(2 * time.Second)
		defer timer.Stop()
		select {
		case <-readDone:
		case <-timer.C:
		}
	}

	return nil
}

func (c *Client) isClosed() bool {
	c.pendMutex.Lock()
	defer c.pendMutex.Unlock()
	return c.closed
}

func (c *Client) closeWaiters(reason error) {
	if reason == nil {
		reason = errors.New("MCP server connection closed")
	}

	c.pendMutex.Lock()
	if !c.closed {
		c.closed = true
		c.closeErr = reason
		if c.closeCh != nil {
			close(c.closeCh)
		}
	}
	errMsg := c.closeErr.Error()
	for id, ch := range c.pending {
		select {
		case ch <- Response{ID: id, Error: &Error{Code: -32000, Message: errMsg}}:
		default:
		}
		delete(c.pending, id)
	}
	c.pendMutex.Unlock()
}

func (c *Client) readLoop() {
	c.mu.Lock()
	stdout := c.stdout
	cmd := c.cmd
	readDone := c.readDone
	c.mu.Unlock()
	c.runReadLoop(stdout, cmd, readDone)
}

func (c *Client) runReadLoop(stdout io.ReadCloser, cmd *exec.Cmd, readDone chan struct{}) {
	if readDone != nil {
		defer close(readDone)
	}
	if stdout == nil {
		c.closeWaiters(errors.New("MCP stdout pipe is nil"))
		return
	}

	var scanErr error
	var waitErr error
	defer func() {
		_ = stdout.Close()
		if cmd != nil {
			waitErr = cmd.Wait()
		}
		reason := errors.New("MCP server connection closed")
		if scanErr != nil {
			reason = fmt.Errorf("MCP stream read error: %w", scanErr)
		} else if waitErr != nil {
			reason = fmt.Errorf("MCP server exited: %w", waitErr)
		}
		c.closeWaiters(reason)
	}()

	scanner := bufio.NewScanner(stdout)
	scanBuf := make([]byte, 64*1024)
	scanner.Buffer(scanBuf, maxStdioFrameBytes)
	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		var msg rpcIncoming
		if err := json.Unmarshal(line, &msg); err != nil {
			// Skip partial/malformed JSON or stray log lines without terminating the stream.
			continue
		}
		// Ignore server-to-client notifications/requests so they never spoof or steal client responses.
		if msg.Method != "" || msg.ID == nil {
			continue
		}
		resp := Response{
			JSONRPC: msg.JSONRPC,
			ID:      *msg.ID,
			Result:  msg.Result,
			Error:   msg.Error,
		}
		c.pendMutex.Lock()
		if ch, ok := c.pending[resp.ID]; ok {
			select {
			case ch <- resp:
			default:
			}
			delete(c.pending, resp.ID)
		}
		c.pendMutex.Unlock()
	}
	scanErr = scanner.Err()
}

func (c *Client) sendRequest(ctx context.Context, method string, params interface{}) (*Response, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

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
		requestCtx, cancel := context.WithTimeout(ctx, defaultRequestTimeout)
		defer cancel()

		httpReq, err := http.NewRequestWithContext(requestCtx, "POST", c.url, bytes.NewReader(reqBytes))
		if err != nil {
			return nil, err
		}
		httpReq.Header.Set("Content-Type", "application/json")
		c.mu.Lock()
		for k, v := range c.headers {
			httpReq.Header.Set(k, v)
		}
		httpClient := c.httpClient
		c.mu.Unlock()
		if httpClient == nil {
			httpClient = &http.Client{Timeout: defaultRequestTimeout}
		}

		resp, err := httpClient.Do(httpReq)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()

		body, readErr := io.ReadAll(io.LimitReader(resp.Body, maxStdioFrameBytes))
		if readErr != nil {
			return nil, readErr
		}

		var r Response
		if err := json.Unmarshal(body, &r); err != nil {
			if resp.StatusCode < 200 || resp.StatusCode >= 300 {
				return nil, fmt.Errorf("MCP HTTP error %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
			}
			return nil, err
		}
		if r.Error != nil {
			return nil, fmt.Errorf("rpc error: %s (code %d)", r.Error.Message, r.Error.Code)
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return nil, fmt.Errorf("MCP HTTP error %d", resp.StatusCode)
		}
		return &r, nil
	}

	c.mu.Lock()
	stdin := c.stdin
	c.mu.Unlock()
	if stdin == nil {
		return nil, fmt.Errorf("MCP client stdin is not open")
	}

	reqBytes = append(reqBytes, '\n')
	ch := make(chan Response, 1)

	c.pendMutex.Lock()
	if c.closed {
		cerr := c.closeErr
		c.pendMutex.Unlock()
		if cerr != nil {
			return nil, cerr
		}
		return nil, fmt.Errorf("MCP server connection closed")
	}
	if c.pending == nil {
		c.pending = make(map[int]chan Response)
	}
	c.pending[id] = ch
	closeCh := c.closeCh
	c.pendMutex.Unlock()

	c.writeMu.Lock()
	_, writeErr := stdin.Write(reqBytes)
	c.writeMu.Unlock()
	if writeErr != nil {
		c.pendMutex.Lock()
		delete(c.pending, id)
		c.pendMutex.Unlock()
		return nil, writeErr
	}

	timer := time.NewTimer(defaultRequestTimeout)
	defer timer.Stop()

	var resp Response
	select {
	case resp = <-ch:
	case <-closeCh:
		select {
		case resp = <-ch:
		default:
			c.pendMutex.Lock()
			delete(c.pending, id)
			cerr := c.closeErr
			c.pendMutex.Unlock()
			if cerr != nil {
				return nil, cerr
			}
			return nil, fmt.Errorf("MCP server connection closed")
		}
	case <-ctx.Done():
		c.pendMutex.Lock()
		delete(c.pending, id)
		c.pendMutex.Unlock()
		return nil, ctx.Err()
	case <-timer.C:
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
	return c.InitializeContext(context.Background())
}

func (c *Client) InitializeContext(ctx context.Context) error {
	params := InitializeParams{
		ProtocolVersion: "2024-11-05",
		Capabilities:    map[string]interface{}{},
		ClientInfo:      ClientInfo{Name: "microgravity", Version: "1.1.0"},
	}
	_, err := c.sendRequest(ctx, "initialize", params)
	return err
}

func (c *Client) ListTools() ([]Tool, error) {
	return c.ListToolsContext(context.Background())
}

func (c *Client) ListToolsContext(ctx context.Context) ([]Tool, error) {
	resp, err := c.sendRequest(ctx, "tools/list", nil)
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
	return c.CallToolContext(context.Background(), name, args)
}

func (c *Client) CallToolContext(ctx context.Context, name string, args map[string]interface{}) ([]Content, error) {
	params := CallToolParams{Name: name, Arguments: args}
	resp, err := c.sendRequest(ctx, "tools/call", params)
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
	return c.ListResourcesContext(context.Background())
}

func (c *Client) ListResourcesContext(ctx context.Context) ([]Resource, error) {
	resp, err := c.sendRequest(ctx, "resources/list", nil)
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
	return c.ReadResourceContext(context.Background(), uri)
}

func (c *Client) ReadResourceContext(ctx context.Context, uri string) ([]ResourceContent, error) {
	params := ReadResourceParams{URI: uri}
	resp, err := c.sendRequest(ctx, "resources/read", params)
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
	if ctx == nil {
		ctx = context.Background()
	}
	if err := c.Start(ctx); err != nil {
		return nil, fmt.Errorf("failed to start mcp server process: %w", err)
	}
	defer c.Stop()

	if err := c.InitializeContext(ctx); err != nil {
		return nil, fmt.Errorf("failed to initialize mcp connection: %w", err)
	}

	tools, err := c.ListToolsContext(ctx)
	if err != nil {
		if ctx.Err() != nil || c.isClosed() {
			return nil, fmt.Errorf("failed to list mcp tools: %w", err)
		}
		tools = []Tool{}
	}

	resources, err := c.ListResourcesContext(ctx)
	if err != nil {
		if ctx.Err() != nil || c.isClosed() {
			return nil, fmt.Errorf("failed to list mcp resources: %w", err)
		}
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

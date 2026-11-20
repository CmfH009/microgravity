package ollama

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/CmfH009/microgravity/internal/budget"
	"github.com/CmfH009/microgravity/internal/cmdparse"
	"github.com/CmfH009/microgravity/internal/mcp"
)

const (
	defaultBaseURL    = "http://localhost:11434"
	maxErrorBodyBytes = 64 * 1024
)

var (
	// ErrEmptyResponse indicates the Ollama HTTP 200 stream ended without valid chunks.
	ErrEmptyResponse = errors.New("ollama returned empty response")
	// ErrModelNotFound indicates Ollama returned 404 for the requested model.
	ErrModelNotFound = errors.New("ollama model not found")
)

var EnableDebug = false

type Client struct {
	BaseURL    string
	HTTPClient *http.Client
}

func newDefaultHTTPClient() *http.Client {
	transport := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   15 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		ResponseHeaderTimeout: 45 * time.Second,
		IdleConnTimeout:       90 * time.Second,
		MaxIdleConns:          32,
		MaxIdleConnsPerHost:   8,
	}
	return &http.Client{
		Transport: transport,
		Timeout:   10 * time.Minute,
	}
}

func NewClient(baseURL string) *Client {
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	return &Client{
		BaseURL:    strings.TrimRight(baseURL, "/"),
		HTTPClient: newDefaultHTTPClient(),
	}
}

func (c *Client) httpClient() *http.Client {
	if c != nil && c.HTTPClient != nil {
		return c.HTTPClient
	}
	return newDefaultHTTPClient()
}

func (c *Client) baseURL() string {
	if c == nil || strings.TrimSpace(c.BaseURL) == "" {
		return defaultBaseURL
	}
	return strings.TrimRight(c.BaseURL, "/")
}

func drainAndClose(body io.ReadCloser) {
	if body == nil {
		return
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(body, maxErrorBodyBytes))
	_ = body.Close()
}

func readBoundedErrorBody(body io.ReadCloser) string {
	if body == nil {
		return ""
	}
	data, _ := io.ReadAll(io.LimitReader(body, maxErrorBodyBytes))
	_, _ = io.Copy(io.Discard, io.LimitReader(body, maxErrorBodyBytes))
	_ = body.Close()
	return strings.TrimSpace(string(data))
}

func EnsureOllama() {
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get(defaultBaseURL + "/")
	if err == nil {
		drainAndClose(resp.Body)
		return
	}

	// Not responding, check if installed
	_, err = exec.LookPath("ollama")
	if err != nil {
		fmt.Fprintln(os.Stderr, "\n⚠️ Ollama is not running and doesn't seem to be installed.")
		fmt.Fprintln(os.Stderr, "To use local models, please install it from https://ollama.com/")
		return
	}

	fmt.Println("🚀 Starting Ollama daemon in the background...")
	cmd := exec.Command("ollama", "serve")
	if err := cmd.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to start Ollama: %v\n", err)
		return
	}
	// Give it a moment to boot
	time.Sleep(2 * time.Second)
}

type OllamaTool struct {
	Type     string                   `json:"type"` // "function"
	Function OllamaFunctionDefinition `json:"function"`
}

type OllamaFunctionDefinition struct {
	Name        string      `json:"name"`
	Description string      `json:"description"`
	Parameters  interface{} `json:"parameters"` // standard JSON Schema object
}

type OllamaFunction = OllamaFunctionDefinition

type OllamaToolCall struct {
	Function OllamaToolCallFunction `json:"function"`
}

type OllamaToolCallFunction struct {
	Name      string                 `json:"name"`
	Arguments map[string]interface{} `json:"arguments"`
}

type GenerateResult struct {
	Text      string
	ToolCalls []cmdparse.ToolCall
}

type ChatMessage struct {
	Role      string           `json:"role"`
	Content   string           `json:"content"`
	Images    []string         `json:"images,omitempty"`
	ToolCalls []OllamaToolCall `json:"tool_calls,omitempty"`
}

type ChatRequest struct {
	Model    string        `json:"model"`
	Messages []ChatMessage `json:"messages"`
	Tools    []OllamaTool  `json:"tools,omitempty"`
	Stream   bool          `json:"stream"`
}

type ChatResponse struct {
	Message  ChatMessage `json:"message"`
	Response string      `json:"response,omitempty"`
	Done     bool        `json:"done"`
	Error    string      `json:"error,omitempty"`
}

// MCPToOllamaTools converts MCP tool definitions to Ollama tools format.
func MCPToOllamaTools(tools []mcp.Tool) []OllamaTool {
	var out []OllamaTool
	for _, t := range tools {
		schema := t.InputSchema
		if schema.Type == "" {
			schema.Type = "object"
		}
		if schema.Properties == nil {
			schema.Properties = make(map[string]interface{})
		}
		out = append(out, OllamaTool{
			Type: "function",
			Function: OllamaFunctionDefinition{
				Name:        t.Name,
				Description: t.Description,
				Parameters:  schema,
			},
		})
	}
	return out
}

var mcpToOllamaTools = MCPToOllamaTools

func (c *Client) GenerateWithTools(ctx context.Context, model, prompt string, images []string, tools []mcp.Tool, out io.Writer) (*GenerateResult, error) {
	if c == nil {
		return nil, fmt.Errorf("ollama client is nil")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if out == nil {
		out = io.Discard
	}
	var messages []ChatMessage

	// Split system prompt from user/conversation prompt if present
	delimiter := "\n---CONVERSATION---\n"
	userIdx := strings.Index(prompt, delimiter)
	if userIdx != -1 {
		systemContent := strings.TrimSpace(prompt[:userIdx])
		userContent := strings.TrimSpace(prompt[userIdx+len(delimiter):])
		if systemContent != "" {
			messages = append(messages, ChatMessage{
				Role:    "system",
				Content: systemContent,
			})
		}
		messages = append(messages, ChatMessage{
			Role:    "user",
			Content: userContent,
			Images:  images,
		})
	} else {
		messages = append(messages, ChatMessage{
			Role:    "user",
			Content: prompt,
			Images:  images,
		})
	}

	reqBody := ChatRequest{
		Model:    model,
		Messages: messages,
		Stream:   true,
	}
	if len(tools) > 0 {
		reqBody.Tools = MCPToOllamaTools(tools)
	}

	buf := new(bytes.Buffer)
	if err := json.NewEncoder(buf).Encode(reqBody); err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", c.baseURL()+"/api/chat", buf)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	if EnableDebug {
		fmt.Printf("[DEBUG] Sending request to Ollama API for model %s...\n", model)
	}

	resp, err := c.httpClient().Do(req)
	if err != nil {
		if EnableDebug {
			fmt.Printf("[DEBUG] HTTP Request failed: %v\n", err)
		}
		if ctx.Err() != nil {
			return nil, fmt.Errorf("ollama request cancelled: %w", ctx.Err())
		}
		return nil, fmt.Errorf("ollama request failed for model %s: %w", model, err)
	}
	defer drainAndClose(resp.Body)
	if EnableDebug {
		fmt.Printf("[DEBUG] HTTP Request succeeded. Status: %d\n", resp.StatusCode)
	}

	if resp.StatusCode != http.StatusOK {
		errBody := readBoundedErrorBody(resp.Body)
		if resp.StatusCode == http.StatusNotFound {
			return nil, fmt.Errorf("%w: local AI model %s unavailable or downloading: %s", ErrModelNotFound, model, errBody)
		}
		return nil, fmt.Errorf("unexpected status code from ollama: %d, body: %s", resp.StatusCode, errBody)
	}

	var fullResponse strings.Builder
	var toolCalls []cmdparse.ToolCall
	scanner := bufio.NewScanner(resp.Body)
	scanBuf := make([]byte, 1024*1024)
	scanner.Buffer(scanBuf, 10*1024*1024) // 10MB max capacity

	chunks := 0
	validChunks := 0
	receivedDone := false

	for scanner.Scan() {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("ollama stream cancelled: %w", err)
		}
		chunks++
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		var genResp ChatResponse
		if err := json.Unmarshal(line, &genResp); err != nil {
			if EnableDebug {
				fmt.Printf("[DEBUG] Failed to unmarshal chunk %d: %v. Line snippet: %s\n", chunks, err, string(line[:min(len(line), 100)]))
			}
			return nil, fmt.Errorf("malformed ollama NDJSON chunk %d: %w", chunks, err)
		}
		if genResp.Error != "" {
			return nil, fmt.Errorf("ollama stream error: %s", genResp.Error)
		}
		validChunks++
		if validChunks == 1 && EnableDebug {
			fmt.Printf("[DEBUG] First chunk successfully parsed\n")
		}
		content := genResp.Message.Content
		if content == "" && genResp.Response != "" {
			content = genResp.Response
		}
		fmt.Fprint(out, content)
		fullResponse.WriteString(content)

		for _, tc := range genResp.Message.ToolCalls {
			name := tc.Function.Name
			args := tc.Function.Arguments

			if name == "" {
				// Fragmented argument chunk without function name:
				// Merge arguments into the active/latest tool call.
				if len(toolCalls) > 0 {
					lastIdx := len(toolCalls) - 1
					if toolCalls[lastIdx].Arguments == nil {
						toolCalls[lastIdx].Arguments = make(map[string]interface{})
					}
					for k, v := range args {
						toolCalls[lastIdx].Arguments[k] = v
					}
				}
			} else {
				// Chunk contains a function name.
				// Check if this is a delta continuation of the current active tool call.
				isContinuation := false
				if len(toolCalls) > 0 && toolCalls[len(toolCalls)-1].ToolCall == name {
					lastArgs := toolCalls[len(toolCalls)-1].Arguments
					if len(lastArgs) == 0 || len(args) == 0 {
						isContinuation = true
					} else {
						collides := false
						for k, v := range args {
							if existingVal, exists := lastArgs[k]; exists {
								if existingVal != v {
									collides = true
									break
								}
							}
						}
						if !collides {
							isContinuation = true
						}
					}
				}

				if isContinuation {
					lastIdx := len(toolCalls) - 1
					if toolCalls[lastIdx].Arguments == nil {
						toolCalls[lastIdx].Arguments = make(map[string]interface{})
					}
					for k, v := range args {
						toolCalls[lastIdx].Arguments[k] = v
					}
				} else {
					newArgs := make(map[string]interface{})
					for k, v := range args {
						newArgs[k] = v
					}
					toolCalls = append(toolCalls, cmdparse.ToolCall{
						ToolCall:  name,
						Arguments: newArgs,
					})
				}
			}
		}

		if genResp.Done {
			receivedDone = true
			if EnableDebug {
				fmt.Printf("[DEBUG] Received done=true on chunk %d\n", chunks)
			}
			break
		}
	}

	if scanErr := scanner.Err(); scanErr != nil {
		if EnableDebug {
			fmt.Printf("[DEBUG] Scanner error after %d chunks: %v\n", chunks, scanErr)
		}
		if ctx.Err() != nil {
			return nil, fmt.Errorf("ollama stream cancelled: %w", ctx.Err())
		}
		return nil, fmt.Errorf("ollama stream read error: %w", scanErr)
	}
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("ollama stream cancelled: %w", err)
	}
	if validChunks == 0 {
		return nil, fmt.Errorf("%w: %w", ErrEmptyResponse, io.ErrUnexpectedEOF)
	}
	if !receivedDone {
		return nil, fmt.Errorf("ollama stream ended prematurely without done=true: %w", io.ErrUnexpectedEOF)
	}

	fmt.Fprintln(out) // Print final newline

	// Record token usage against the daily budget. Ollama does not report
	// token counts in its streaming response, so fall back to a ~4 chars/token
	// estimate for both prompt and completion.
	budget.Record(model, estimateTokens(prompt), estimateTokens(fullResponse.String()))

	return &GenerateResult{
		Text:      fullResponse.String(),
		ToolCalls: toolCalls,
	}, nil
}

func (c *Client) Generate(ctx context.Context, model, prompt string, images []string, out io.Writer) (string, error) {
	res, err := c.GenerateWithTools(ctx, model, prompt, images, nil, out)
	if res == nil {
		return "", err
	}
	return res.Text, err
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// estimateTokens approximates token count from text length (~4 chars/token).
func estimateTokens(s string) int {
	return (len(s) + 3) / 4
}

type PullRequest struct {
	Model  string `json:"model"`
	Stream bool   `json:"stream"`
}

type PullProgress struct {
	Status    string `json:"status"`
	Digest    string `json:"digest"`
	Total     int64  `json:"total"`
	Completed int64  `json:"completed"`
	Error     string `json:"error,omitempty"`
}

func (c *Client) Pull(ctx context.Context, model string, onProgress func(PullProgress)) error {
	if c == nil {
		return fmt.Errorf("ollama client is nil")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	reqBody := PullRequest{
		Model:  model,
		Stream: true,
	}
	buf := new(bytes.Buffer)
	if err := json.NewEncoder(buf).Encode(reqBody); err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", c.baseURL()+"/api/pull", buf)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient().Do(req)
	if err != nil {
		return err
	}
	defer drainAndClose(resp.Body)

	if resp.StatusCode != http.StatusOK {
		errBody := readBoundedErrorBody(resp.Body)
		return fmt.Errorf("unexpected status code during pull: %d, body: %s", resp.StatusCode, errBody)
	}

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	for scanner.Scan() {
		if err := ctx.Err(); err != nil {
			return err
		}
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		var p PullProgress
		if err := json.Unmarshal(line, &p); err != nil {
			return fmt.Errorf("malformed pull progress JSON: %w", err)
		}
		if p.Error != "" {
			return fmt.Errorf("ollama pull error: %s", p.Error)
		}
		if onProgress != nil {
			onProgress(p)
		}
	}
	if scanErr := scanner.Err(); scanErr != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return scanErr
	}
	return ctx.Err()
}

type ModelListResponse struct {
	Models []struct {
		Name string `json:"name"`
	} `json:"models"`
}

// ListModels returns the names of all locally downloaded Ollama models.
func (c *Client) ListModels(ctx context.Context) ([]string, error) {
	if c == nil {
		return nil, fmt.Errorf("ollama client is nil")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, "GET", c.baseURL()+"/api/tags", nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.httpClient().Do(req)
	if err != nil {
		return nil, err
	}
	defer drainAndClose(resp.Body)

	if resp.StatusCode != http.StatusOK {
		errBody := readBoundedErrorBody(resp.Body)
		return nil, fmt.Errorf("failed to get tags: status %d, body: %s", resp.StatusCode, errBody)
	}

	var list ModelListResponse
	if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
		return nil, fmt.Errorf("decode tags response: %w", err)
	}

	names := make([]string, 0, len(list.Models))
	for _, m := range list.Models {
		if m.Name != "" {
			names = append(names, m.Name)
		}
	}
	return names, nil
}

// HasModel checks if a model is already downloaded locally.
func (c *Client) HasModel(ctx context.Context, modelName string) (bool, error) {
	names, err := c.ListModels(ctx)
	if err != nil {
		return false, err
	}
	return MatchModelName(names, modelName), nil
}

// MatchModelName reports whether modelName exists in downloaded model names,
// checking exact match or ":latest" when modelName has no explicit tag.
func MatchModelName(names []string, modelName string) bool {
	if modelName == "" {
		return false
	}
	hasTag := strings.Contains(modelName, ":")
	for _, name := range names {
		if name == modelName {
			return true
		}
		if !hasTag && name == modelName+":latest" {
			return true
		}
	}
	return false
}

package openai

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
	"sort"
	"strings"
	"time"

	"github.com/CmfH009/microgravity/internal/budget"
	"github.com/CmfH009/microgravity/internal/cmdparse"
	"github.com/CmfH009/microgravity/internal/mcp"
)

const (
	defaultBaseURL    = "http://localhost:11434/v1"
	maxErrorBodyBytes = 64 * 1024
)

var (
	// ErrEmptyResponse indicates the OpenAI-compatible HTTP 200 response ended with no valid content or chunks.
	ErrEmptyResponse = errors.New("openai-compatible backend returned empty response")
	// ErrModelNotFound indicates the OpenAI-compatible backend returned HTTP 404 for the requested model.
	ErrModelNotFound = errors.New("openai-compatible model not found")
)

// Client communicates with any OpenAI-compatible HTTP endpoint
// (llama-server, LM Studio, vLLM, or Ollama /v1).
type Client struct {
	BaseURL    string
	APIKey     string
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

// NewClient creates an OpenAI-compatible client targeting baseURL (defaults to http://localhost:11434/v1).
func NewClient(baseURL, apiKey string) *Client {
	trimmed := strings.TrimSpace(baseURL)
	if trimmed == "" {
		trimmed = defaultBaseURL
	}
	return &Client{
		BaseURL:    strings.TrimRight(trimmed, "/"),
		APIKey:     strings.TrimSpace(apiKey),
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
	return strings.TrimRight(strings.TrimSpace(c.BaseURL), "/")
}

func (c *Client) endpointURL(path string) string {
	base := c.baseURL()
	if strings.HasSuffix(base, "/v1") {
		return base + path
	}
	return base + "/v1" + path
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

func estimateTokens(s string) int {
	return (len(s) + 3) / 4
}

// Tool describes a function tool definition in the OpenAI chat completions schema.
type Tool struct {
	Type     string             `json:"type"`
	Function FunctionDefinition `json:"function"`
}

// FunctionDefinition describes a callable tool's metadata and JSON schema parameters.
type FunctionDefinition struct {
	Name        string      `json:"name"`
	Description string      `json:"description,omitempty"`
	Parameters  interface{} `json:"parameters,omitempty"`
}

// ChatMessage represents a single message in an OpenAI chat completion request.
type ChatMessage struct {
	Role    string      `json:"role"`
	Content interface{} `json:"content"`
}

// ChatCompletionRequest is the request payload for POST /v1/chat/completions.
type ChatCompletionRequest struct {
	Model    string        `json:"model"`
	Messages []ChatMessage `json:"messages"`
	Tools    []Tool        `json:"tools,omitempty"`
	Stream   bool          `json:"stream"`
}

// GenerateResult holds the accumulated text and parsed tool calls from a completion.
type GenerateResult struct {
	Text      string
	ToolCalls []cmdparse.ToolCall
}

// APIError captures an error object embedded inside an OpenAI-compatible HTTP/SSE payload.
type APIError struct {
	Message string      `json:"message"`
	Type    string      `json:"type,omitempty"`
	Code    interface{} `json:"code,omitempty"`
}

type streamToolCallDelta struct {
	Index    *int   `json:"index,omitempty"`
	ID       string `json:"id,omitempty"`
	Type     string `json:"type,omitempty"`
	Function struct {
		Name      string `json:"name,omitempty"`
		Arguments string `json:"arguments,omitempty"`
	} `json:"function"`
}

type streamChoice struct {
	Index int `json:"index"`
	Delta struct {
		Role      string                `json:"role,omitempty"`
		Content   string                `json:"content,omitempty"`
		ToolCalls []streamToolCallDelta `json:"tool_calls,omitempty"`
	} `json:"delta"`
	FinishReason *string `json:"finish_reason,omitempty"`
}

type nonStreamToolCall struct {
	ID       string `json:"id,omitempty"`
	Type     string `json:"type,omitempty"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

type nonStreamChoice struct {
	Index   int `json:"index"`
	Message struct {
		Role      string              `json:"role"`
		Content   string              `json:"content"`
		ToolCalls []nonStreamToolCall `json:"tool_calls,omitempty"`
	} `json:"message"`
	FinishReason string `json:"finish_reason,omitempty"`
}

type streamUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
}

type chatCompletionChunk struct {
	ID      string         `json:"id,omitempty"`
	Choices []streamChoice `json:"choices"`
	Usage   *streamUsage   `json:"usage,omitempty"`
	Error   *APIError      `json:"error,omitempty"`
}

type chatCompletionResponse struct {
	ID      string            `json:"id,omitempty"`
	Choices []nonStreamChoice `json:"choices"`
	Usage   *streamUsage      `json:"usage,omitempty"`
	Error   *APIError         `json:"error,omitempty"`
}

type toolCallAccumulator struct {
	id      string
	name    string
	argsBuf strings.Builder
}

// MCPToOpenAITools converts MCP tool schemas into OpenAI function tool definitions.
func MCPToOpenAITools(tools []mcp.Tool) []Tool {
	if len(tools) == 0 {
		return nil
	}
	out := make([]Tool, 0, len(tools))
	for _, t := range tools {
		schema := t.InputSchema
		if schema.Type == "" {
			schema.Type = "object"
		}
		if schema.Properties == nil {
			schema.Properties = make(map[string]interface{})
		}
		out = append(out, Tool{
			Type: "function",
			Function: FunctionDefinition{
				Name:        t.Name,
				Description: t.Description,
				Parameters:  schema,
			},
		})
	}
	return out
}

func buildMessages(prompt string, images []string) []ChatMessage {
	buildUserContent := func(text string) interface{} {
		if len(images) == 0 {
			return text
		}
		parts := []map[string]interface{}{
			{"type": "text", "text": text},
		}
		for _, img := range images {
			url := img
			if !strings.HasPrefix(url, "data:") {
				url = "data:image/jpeg;base64," + url
			}
			parts = append(parts, map[string]interface{}{
				"type":      "image_url",
				"image_url": map[string]string{"url": url},
			})
		}
		return parts
	}

	delimiter := "\n---CONVERSATION---\n"
	if userIdx := strings.Index(prompt, delimiter); userIdx != -1 {
		systemContent := strings.TrimSpace(prompt[:userIdx])
		userContent := strings.TrimSpace(prompt[userIdx+len(delimiter):])
		var msgs []ChatMessage
		if systemContent != "" {
			msgs = append(msgs, ChatMessage{
				Role:    "system",
				Content: systemContent,
			})
		}
		msgs = append(msgs, ChatMessage{
			Role:    "user",
			Content: buildUserContent(userContent),
		})
		return msgs
	}

	return []ChatMessage{
		{
			Role:    "user",
			Content: buildUserContent(prompt),
		},
	}
}

func resolveToolCallIndex(tc streamToolCallDelta, accum map[int]*toolCallAccumulator, lastIdx *int) int {
	if tc.Index != nil {
		idx := *tc.Index
		if idx < 0 {
			idx = 0
		}
		*lastIdx = idx
		return idx
	}
	if tc.ID != "" {
		keys := make([]int, 0, len(accum))
		for k := range accum {
			keys = append(keys, k)
		}
		sort.Ints(keys)
		for _, k := range keys {
			if accum[k].id == tc.ID {
				*lastIdx = k
				return k
			}
		}
		next := nextFreeIndex(accum)
		*lastIdx = next
		return next
	}
	if tc.Function.Name != "" {
		if *lastIdx >= 0 {
			if existing, ok := accum[*lastIdx]; ok && existing.name == "" {
				return *lastIdx
			}
		}
		next := nextFreeIndex(accum)
		*lastIdx = next
		return next
	}
	if *lastIdx >= 0 {
		return *lastIdx
	}
	*lastIdx = 0
	return 0
}

func nextFreeIndex(accum map[int]*toolCallAccumulator) int {
	next := 0
	for {
		if _, exists := accum[next]; !exists {
			return next
		}
		next++
	}
}

func finalizeToolCalls(accum map[int]*toolCallAccumulator) ([]cmdparse.ToolCall, error) {
	if len(accum) == 0 {
		return nil, nil
	}
	keys := make([]int, 0, len(accum))
	for k := range accum {
		keys = append(keys, k)
	}
	sort.Ints(keys)

	calls := make([]cmdparse.ToolCall, 0, len(keys))
	for _, idx := range keys {
		acc := accum[idx]
		if strings.TrimSpace(acc.name) == "" {
			return nil, fmt.Errorf("openai tool call at index %d missing function name", idx)
		}
		argsMap := make(map[string]interface{})
		rawArgs := strings.TrimSpace(acc.argsBuf.String())
		if rawArgs != "" {
			if err := json.Unmarshal([]byte(rawArgs), &argsMap); err != nil {
				return nil, fmt.Errorf("malformed tool call arguments for %q: %w", acc.name, err)
			}
			if argsMap == nil {
				argsMap = make(map[string]interface{})
			}
		}
		calls = append(calls, cmdparse.ToolCall{
			ToolCall:  acc.name,
			Arguments: argsMap,
		})
	}
	return calls, nil
}

// GenerateWithTools streams a chat completion with optional tool definitions from an
// OpenAI-compatible /v1/chat/completions endpoint.
func (c *Client) GenerateWithTools(ctx context.Context, model, prompt string, images []string, tools []mcp.Tool, out io.Writer) (*GenerateResult, error) {
	if c == nil {
		return nil, fmt.Errorf("openai client is nil")
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

	reqBody := ChatCompletionRequest{
		Model:    model,
		Messages: buildMessages(prompt, images),
		Stream:   true,
	}
	if len(tools) > 0 {
		reqBody.Tools = MCPToOpenAITools(tools)
	}

	buf := new(bytes.Buffer)
	if err := json.NewEncoder(buf).Encode(reqBody); err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpointURL("/chat/completions"), buf)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	if key := strings.TrimSpace(c.APIKey); key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}

	resp, err := c.httpClient().Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, fmt.Errorf("openai request cancelled: %w", ctx.Err())
		}
		return nil, fmt.Errorf("openai request failed for model %s: %w", model, err)
	}
	defer drainAndClose(resp.Body)

	if resp.StatusCode != http.StatusOK {
		errBody := readBoundedErrorBody(resp.Body)
		if resp.StatusCode == http.StatusNotFound {
			return nil, fmt.Errorf("%w: model %s unavailable: %s", ErrModelNotFound, model, errBody)
		}
		return nil, fmt.Errorf("unexpected status code from openai-compatible endpoint: %d, body: %s", resp.StatusCode, errBody)
	}

	contentType := strings.ToLower(resp.Header.Get("Content-Type"))
	if strings.Contains(contentType, "application/json") {
		return c.decodeNonStreamingResponse(ctx, model, prompt, resp.Body, out)
	}

	var fullResponse strings.Builder
	toolAccum := make(map[int]*toolCallAccumulator)
	lastToolIdx := -1
	var usage *streamUsage

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 64*1024), 10*1024*1024)

	chunks := 0
	validChunks := 0
	receivedDone := false

	for scanner.Scan() {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("openai stream cancelled: %w", ctx.Err())
		}
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, ":") {
			continue
		}

		var data string
		if strings.HasPrefix(line, "data:") {
			data = strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		} else if strings.HasPrefix(line, "event:") || strings.HasPrefix(line, "id:") || strings.HasPrefix(line, "retry:") {
			continue
		} else if strings.HasPrefix(line, "{") {
			data = line
		} else {
			return nil, fmt.Errorf("unexpected non-SSE line in openai stream: %q", line)
		}

		if data == "" {
			continue
		}
		if data == "[DONE]" {
			receivedDone = true
			break
		}

		chunks++
		var chunk chatCompletionChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			return nil, fmt.Errorf("malformed openai SSE chunk %d: %w", chunks, err)
		}
		if chunk.Error != nil {
			return nil, fmt.Errorf("openai stream error: %s", chunk.Error.Message)
		}

		validChunks++
		if chunk.Usage != nil {
			usage = chunk.Usage
		}
		if len(chunk.Choices) > 0 {
			delta := chunk.Choices[0].Delta
			if delta.Content != "" {
				fmt.Fprint(out, delta.Content)
				fullResponse.WriteString(delta.Content)
			}
			for _, tc := range delta.ToolCalls {
				idx := resolveToolCallIndex(tc, toolAccum, &lastToolIdx)
				acc, exists := toolAccum[idx]
				if !exists {
					acc = &toolCallAccumulator{}
					toolAccum[idx] = acc
				}
				if tc.ID != "" && acc.id == "" {
					acc.id = tc.ID
				}
				if tc.Function.Name != "" && acc.name == "" {
					acc.name = tc.Function.Name
				}
				if tc.Function.Arguments != "" {
					acc.argsBuf.WriteString(tc.Function.Arguments)
				}
			}
		}
	}

	if scanErr := scanner.Err(); scanErr != nil {
		if ctx.Err() != nil {
			return nil, fmt.Errorf("openai stream cancelled: %w", ctx.Err())
		}
		return nil, fmt.Errorf("openai stream read error: %w", scanErr)
	}
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("openai stream cancelled: %w", err)
	}
	if validChunks == 0 {
		return nil, fmt.Errorf("%w: %w", ErrEmptyResponse, io.ErrUnexpectedEOF)
	}
	if !receivedDone {
		return nil, fmt.Errorf("openai SSE stream ended prematurely without [DONE]: %w", io.ErrUnexpectedEOF)
	}

	toolCalls, err := finalizeToolCalls(toolAccum)
	if err != nil {
		return nil, err
	}

	fmt.Fprintln(out)

	promptTokens := estimateTokens(prompt)
	compTokens := estimateTokens(fullResponse.String())
	if usage != nil && (usage.PromptTokens > 0 || usage.CompletionTokens > 0) {
		promptTokens = usage.PromptTokens
		compTokens = usage.CompletionTokens
	}
	budget.Record(model, promptTokens, compTokens)

	return &GenerateResult{
		Text:      fullResponse.String(),
		ToolCalls: toolCalls,
	}, nil
}

func (c *Client) decodeNonStreamingResponse(ctx context.Context, model, prompt string, body io.Reader, out io.Writer) (*GenerateResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	data, err := io.ReadAll(io.LimitReader(body, 16*1024*1024))
	if err != nil {
		return nil, fmt.Errorf("read openai response: %w", err)
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, fmt.Errorf("%w: empty JSON body", ErrEmptyResponse)
	}
	var resp chatCompletionResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("malformed openai JSON response: %w", err)
	}
	if resp.Error != nil {
		return nil, fmt.Errorf("openai response error: %s", resp.Error.Message)
	}
	if len(resp.Choices) == 0 {
		return nil, fmt.Errorf("%w: no choices returned", ErrEmptyResponse)
	}

	msg := resp.Choices[0].Message
	if msg.Content != "" {
		fmt.Fprint(out, msg.Content)
	}
	var toolCalls []cmdparse.ToolCall
	for i, tc := range msg.ToolCalls {
		if strings.TrimSpace(tc.Function.Name) == "" {
			return nil, fmt.Errorf("openai tool call at index %d missing function name", i)
		}
		argsMap := make(map[string]interface{})
		rawArgs := strings.TrimSpace(tc.Function.Arguments)
		if rawArgs != "" {
			if err := json.Unmarshal([]byte(rawArgs), &argsMap); err != nil {
				return nil, fmt.Errorf("malformed tool call arguments for %q: %w", tc.Function.Name, err)
			}
			if argsMap == nil {
				argsMap = make(map[string]interface{})
			}
		}
		toolCalls = append(toolCalls, cmdparse.ToolCall{
			ToolCall:  tc.Function.Name,
			Arguments: argsMap,
		})
	}

	fmt.Fprintln(out)

	promptTokens := estimateTokens(prompt)
	compTokens := estimateTokens(msg.Content)
	if resp.Usage != nil && (resp.Usage.PromptTokens > 0 || resp.Usage.CompletionTokens > 0) {
		promptTokens = resp.Usage.PromptTokens
		compTokens = resp.Usage.CompletionTokens
	}
	budget.Record(model, promptTokens, compTokens)

	return &GenerateResult{
		Text:      msg.Content,
		ToolCalls: toolCalls,
	}, nil
}

// Generate streams a plain text completion without tool definitions.
func (c *Client) Generate(ctx context.Context, model, prompt string, images []string, out io.Writer) (string, error) {
	res, err := c.GenerateWithTools(ctx, model, prompt, images, nil, out)
	if res == nil {
		return "", err
	}
	return res.Text, err
}

type modelListResponse struct {
	Object string `json:"object"`
	Data   []struct {
		ID string `json:"id"`
	} `json:"data"`
}

// ListModels queries GET /v1/models and returns available model IDs.
func (c *Client) ListModels(ctx context.Context) ([]string, error) {
	if c == nil {
		return nil, fmt.Errorf("openai client is nil")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.endpointURL("/models"), nil)
	if err != nil {
		return nil, err
	}
	if key := strings.TrimSpace(c.APIKey); key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}

	resp, err := c.httpClient().Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, fmt.Errorf("openai models request cancelled: %w", ctx.Err())
		}
		return nil, fmt.Errorf("openai models request failed: %w", err)
	}
	defer drainAndClose(resp.Body)

	if resp.StatusCode != http.StatusOK {
		errBody := readBoundedErrorBody(resp.Body)
		return nil, fmt.Errorf("failed to list models: status %d, body: %s", resp.StatusCode, errBody)
	}

	var list modelListResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4*1024*1024)).Decode(&list); err != nil {
		return nil, fmt.Errorf("decode /v1/models response: %w", err)
	}

	ids := make([]string, 0, len(list.Data))
	for _, m := range list.Data {
		if id := strings.TrimSpace(m.ID); id != "" {
			ids = append(ids, id)
		}
	}
	return ids, nil
}

// HasModel reports whether modelID is returned by ListModels.
func (c *Client) HasModel(ctx context.Context, modelID string) (bool, error) {
	models, err := c.ListModels(ctx)
	if err != nil {
		return false, err
	}
	for _, m := range models {
		if m == modelID {
			return true, nil
		}
	}
	return false, nil
}

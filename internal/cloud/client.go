package cloud

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/purav/microgravity/internal/authutil"
	"github.com/purav/microgravity/internal/budget"
	"github.com/purav/microgravity/internal/cmdparse"
	"github.com/purav/microgravity/internal/config"
	"github.com/purav/microgravity/internal/mcp"
)

type Client struct {
	BaseURL     string
	HTTPClient  *http.Client
	APIKey      string
	BearerToken string
	VertexCreds *authutil.VertexCredentials
}

func NewClient() *Client {
	transport := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   15 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		TLSHandshakeTimeout:   15 * time.Second,
		ResponseHeaderTimeout: 30 * time.Second,
		IdleConnTimeout:       90 * time.Second,
		MaxIdleConns:          100,
		MaxIdleConnsPerHost:   10,
		ExpectContinueTimeout: 1 * time.Second,
	}

	return &Client{
		BaseURL: "https://generativelanguage.googleapis.com",
		HTTPClient: &http.Client{
			Transport: transport,
			Timeout:   10 * time.Minute,
		},
	}
}

type InlineData struct {
	MimeType string `json:"mimeType"`
	Data     string `json:"data"`
}

type GeminiFunctionCall struct {
	Name string                 `json:"name"`
	Args map[string]interface{} `json:"args"`
}

type GeminiFunctionResponse struct {
	Name     string                 `json:"name"`
	Response map[string]interface{} `json:"response"`
}

type Part struct {
	Text         string              `json:"text,omitempty"`
	InlineData   *InlineData         `json:"inlineData,omitempty"`
	FunctionCall *GeminiFunctionCall `json:"functionCall,omitempty"`
}

type Content struct {
	Role  string `json:"role,omitempty"`
	Parts []Part `json:"parts"`
}

type ThinkingConfig struct {
	ThinkingBudget int `json:"thinkingBudget,omitempty"`
}

type GenerationConfig struct {
	Temperature     *float64        `json:"temperature,omitempty"`
	TopP            *float64        `json:"topP,omitempty"`
	MaxOutputTokens int             `json:"maxOutputTokens,omitempty"`
	ThinkingConfig  *ThinkingConfig `json:"thinkingConfig,omitempty"`
}

type GeminiSchema struct {
	Type        string                   `json:"type"` // "OBJECT", "STRING", "INTEGER", "NUMBER", "BOOLEAN", "ARRAY"
	Description string                   `json:"description,omitempty"`
	Properties  map[string]*GeminiSchema `json:"properties,omitempty"`
	Required    []string                 `json:"required,omitempty"`
	Items       *GeminiSchema            `json:"items,omitempty"`
	Enum        []string                 `json:"enum,omitempty"`
}

type GeminiFunctionDeclaration struct {
	Name        string        `json:"name"`
	Description string        `json:"description"`
	Parameters  *GeminiSchema `json:"parameters,omitempty"`
}

type GeminiTool struct {
	FunctionDeclarations []GeminiFunctionDeclaration `json:"functionDeclarations,omitempty"`
}

type GenerateRequest struct {
	SystemInstruction *Content          `json:"system_instruction,omitempty"`
	Contents          []Content         `json:"contents"`
	Tools             []GeminiTool      `json:"tools,omitempty"`
	GenerationConfig  *GenerationConfig `json:"generationConfig,omitempty"`
}

// UsageMetadata reports token accounting attached to Gemini stream chunks.
type UsageMetadata struct {
	PromptTokenCount     int `json:"promptTokenCount"`
	CandidatesTokenCount int `json:"candidatesTokenCount"`
	TotalTokenCount      int `json:"totalTokenCount"`
}

type GenerateResponse struct {
	Candidates []struct {
		Content struct {
			Parts []Part `json:"parts"`
		} `json:"content"`
	} `json:"candidates"`
	UsageMetadata *UsageMetadata `json:"usageMetadata,omitempty"`
}

type GenerateResult struct {
	Text      string
	ToolCalls []cmdparse.ToolCall
}

// estimateTokens approximates token count from text length (~4 chars/token).
func estimateTokens(s string) int {
	return (len(s) + 3) / 4
}

// MCPToGeminiTools converts MCP tool definitions to Gemini API function declarations.
func MCPToGeminiTools(tools []mcp.Tool) []GeminiTool {
	if len(tools) == 0 {
		return nil
	}
	var decls []GeminiFunctionDeclaration
	for _, t := range tools {
		decls = append(decls, GeminiFunctionDeclaration{
			Name:        t.Name,
			Description: t.Description,
			Parameters:  mcpSchemaToGeminiSchema(t.InputSchema),
		})
	}
	return []GeminiTool{{FunctionDeclarations: decls}}
}

var mcpToGeminiTools = MCPToGeminiTools

func mcpSchemaToGeminiSchema(s mcp.Schema) *GeminiSchema {
	sType := s.Type
	if sType == "" {
		sType = "object"
	}
	gs := &GeminiSchema{
		Type:       toGeminiType(sType),
		Properties: make(map[string]*GeminiSchema),
		Required:   s.Required,
	}
	if gs.Type == "" {
		gs.Type = "OBJECT"
	}
	for k, v := range s.Properties {
		if propMap, ok := v.(map[string]interface{}); ok {
			gs.Properties[k] = mapToGeminiSchema(propMap)
		}
	}
	return gs
}

func mapToGeminiSchema(m map[string]interface{}) *GeminiSchema {
	rawType, _ := m["type"].(string)
	gType := toGeminiType(rawType)
	if gType == "" {
		gType = "STRING"
	}
	desc, _ := m["description"].(string)

	var enumVals []string
	if rawEnum, ok := m["enum"].([]string); ok {
		enumVals = rawEnum
	} else if rawEnumSlice, ok := m["enum"].([]interface{}); ok {
		for _, e := range rawEnumSlice {
			if es, ok := e.(string); ok {
				enumVals = append(enumVals, es)
			}
		}
	}

	gs := &GeminiSchema{
		Type:        gType,
		Description: desc,
		Enum:        enumVals,
	}

	if gType == "ARRAY" {
		if itemsRaw, ok := m["items"].(map[string]interface{}); ok {
			gs.Items = mapToGeminiSchema(itemsRaw)
		} else {
			// Mandate: Gemini requires Items on ARRAY schemas
			gs.Items = &GeminiSchema{Type: "STRING"}
		}
	} else if gType == "OBJECT" {
		if propsRaw, ok := m["properties"].(map[string]interface{}); ok {
			gs.Properties = make(map[string]*GeminiSchema)
			for pk, pv := range propsRaw {
				if pvm, ok := pv.(map[string]interface{}); ok {
					gs.Properties[pk] = mapToGeminiSchema(pvm)
				}
			}
		}
		if reqRaw, ok := m["required"].([]string); ok {
			gs.Required = reqRaw
		} else if reqSlice, ok := m["required"].([]interface{}); ok {
			for _, r := range reqSlice {
				if rs, ok := r.(string); ok {
					gs.Required = append(gs.Required, rs)
				}
			}
		}
	}

	return gs
}

func toGeminiType(t string) string {
	switch strings.ToLower(t) {
	case "string":
		return "STRING"
	case "integer", "int":
		return "INTEGER"
	case "number", "float":
		return "NUMBER"
	case "boolean", "bool":
		return "BOOLEAN"
	case "array":
		return "ARRAY"
	case "object":
		return "OBJECT"
	case "":
		return ""
	default:
		return "STRING"
	}
}

func ResolveCloudModel(modelID string) string {
	lower := strings.ToLower(strings.TrimSpace(modelID))
	lower = strings.TrimPrefix(lower, "models/")

	switch lower {
	// Pro-tier aliases → gemini-3.1-pro-preview
	case "gemini-3.1-pro", "gemini-3.1-pro-preview", "gemini-3-pro", "gemini-pro", "gemini-2.5-pro",
		"gemini-2.0-pro", "gemini-2.0-pro-exp", "pro":
		return "gemini-3.1-pro-preview"

	// Flash aliases → gemini-3.8-flash
	case "gemini-3.8-flash", "gemini-3-flash", "gemini-flash", "flash":
		return "gemini-3.8-flash"

	case "gemini-3.7-flash":
		return "gemini-3.7-flash"

	case "gemini-3.6-flash":
		return "gemini-3.6-flash"

	case "gemini-3.5-flash", "gemini-2.5-flash":
		return "gemini-3.8-flash"

	// Flash-Lite aliases → gemini-2.0-flash-lite
	case "gemini-3.5-flash-lite", "gemini-2.5-flash-lite", "gemini-2.0-flash-lite", "flash-lite":
		return "gemini-2.0-flash-lite"

	// Thinking/reasoning → gemini-2.0-flash-thinking
	case "gemini-2.0-flash-thinking", "gemini-2.0-flash-thinking-exp",
		"thinking", "reasoning":
		return "gemini-2.0-flash-thinking-exp-01-21"

	// Direct Gemini 2.0 / 1.5 versions
	case "gemini-2.0-flash":
		return "gemini-2.0-flash"
	case "gemini-1.5-pro":
		return "gemini-1.5-pro"
	case "gemini-1.5-flash":
		return "gemini-1.5-flash"

	// Gemma cloud models pass through as-is
	case "gemma-2-27b-it", "gemma-2-9b-it":
		return lower

	// Local gemma models shouldn't reach cloud, fallback to flash
	default:
		if strings.HasPrefix(lower, "gemma") || strings.HasPrefix(lower, "orbit-") {
			return "gemini-3.8-flash"
		}
		// Already a real API model name (e.g. "gemini-1.5-flash", "gemini-1.5-pro")
		return lower
	}
}

func (c *Client) executeRequestWithRetry(ctx context.Context, apiKey, url string, reqBody GenerateRequest) (*http.Response, error) {
	maxAttempts := 3
	var lastErr error
	var resp *http.Response

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		buf := new(bytes.Buffer)
		if err := json.NewEncoder(buf).Encode(reqBody); err != nil {
			return nil, err
		}

		req, err := http.NewRequestWithContext(ctx, "POST", url, buf)
		if err != nil {
			return nil, err
		}
		if c.BearerToken != "" {
			req.Header.Set("Authorization", "Bearer "+c.BearerToken)
		} else if apiKey != "" {
			req.Header.Set("x-goog-api-key", apiKey)
		} else if token := config.GetToken(); token != nil && token.AccessToken != "" {
			req.Header.Set("Authorization", "Bearer "+token.AccessToken)
		}
		req.Header.Set("Content-Type", "application/json")

		var retryAfter time.Duration
		resp, err = c.HTTPClient.Do(req)
		if err == nil {
			if isTransientStatus(resp.StatusCode) {
				// Throttling (429) and gateway/server faults (500/502/503/504)
				// are worth retrying on the same model before the caller falls
				// back to a different model endpoint.
				retryAfter = parseRetryAfter(resp.Header.Get("Retry-After"))
				resp.Body.Close()
				lastErr = fmt.Errorf("transient cloud server error (status %d)", resp.StatusCode)
			} else {
				return resp, nil
			}
		} else {
			// Network-level failures (timeouts, resets) are retryable too.
			lastErr = err
		}

		// Don't sleep after the last attempt
		if attempt < maxAttempts {
			if sleepErr := sleepCtx(ctx, backoffDelay(attempt, retryAfter)); sleepErr != nil {
				return nil, sleepErr
			}
		}
	}

	return nil, fmt.Errorf("request failed after %d attempts: %w", maxAttempts, lastErr)
}

// isTransientStatus reports whether an HTTP status is worth retrying.
func isTransientStatus(code int) bool {
	switch code {
	case http.StatusTooManyRequests, http.StatusInternalServerError,
		http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return true
	}
	return false
}

// parseRetryAfter reads a Retry-After header expressed in seconds.
func parseRetryAfter(v string) time.Duration {
	v = strings.TrimSpace(v)
	if v == "" {
		return 0
	}
	secs, err := strconv.Atoi(v)
	if err != nil || secs < 0 {
		return 0
	}
	return time.Duration(secs) * time.Second
}

// backoffDelay computes exponential backoff with jitter, honoring a
// server-provided Retry-After hint. Capped so agents stay responsive.
func backoffDelay(attempt int, retryAfter time.Duration) time.Duration {
	d := time.Duration(250*(1<<(attempt-1))) * time.Millisecond
	if retryAfter > d {
		d = retryAfter
	}
	if cap := 8 * time.Second; d > cap {
		d = cap
	}
	// Up to +25% jitter to avoid thundering-herd retries.
	jitter := time.Duration(rand.Int63n(int64(d/4) + 1))
	return d + jitter
}

// sleepCtx sleeps for d or until ctx is done, returning ctx.Err() on cancel.
func sleepCtx(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func (c *Client) GenerateWithTools(ctx context.Context, model, prompt string, images []string, tools []mcp.Tool, out io.Writer) (*GenerateResult, error) {
	if out == nil {
		out = io.Discard
	}

	targetModel := ResolveCloudModel(model)

	// Budget guardrail: fail fast once the daily token/cost cap is reached
	// instead of silently burning more budget.
	if err := budget.Check(); err != nil {
		return nil, err
	}

	// Check if Vertex AI is configured or requested
	authType := config.GetAuthType()
	isVertex := strings.HasPrefix(authType, "vertex") || os.Getenv("VERTEX_API_KEY") != "" || os.Getenv("VERTEX_PROJECT_ID") != "" || c.VertexCreds != nil

	var isVertexMode bool
	var vertexCreds *authutil.VertexCredentials

	if isVertex {
		if c.VertexCreds != nil {
			vertexCreds = c.VertexCreds
			isVertexMode = true
		} else if vCreds, vErr := authutil.ResolveVertexCredentials(ctx); vErr == nil && vCreds != nil {
			vertexCreds = vCreds
			isVertexMode = true
		}
	}

	apiKey := os.Getenv("GEMINI_API_KEY")
	if apiKey == "" {
		apiKey = c.APIKey
	}

	if isVertexMode {
		if vertexCreds.Mode == "express" {
			apiKey = vertexCreds.APIKey
			c.BearerToken = ""
		} else if vertexCreds.Token != nil {
			c.BearerToken = vertexCreds.Token.AccessToken
			apiKey = ""
		}
	} else {
		if apiKey == "" {
			if validKey, err := authutil.EnsureValidTokenAndAPIKey(); err == nil && validKey != "" {
				apiKey = validKey
			} else {
				apiKey = config.GetAutoAPIKey()
			}
		}
		if apiKey == "" {
			return nil, fmt.Errorf("Google account auth failed or API key not provisioned. Please run 'mg auth' (AI Studio) or 'mg auth vertex' (Vertex AI).")
		}
	}

	parts := []Part{{Text: prompt}}
	for _, img := range images {
		parts = append(parts, Part{
			InlineData: &InlineData{
				MimeType: "image/jpeg",
				Data:     img,
			},
		})
	}

	var reqBody GenerateRequest
	delimiter := "\n---CONVERSATION---\n"
	idx := strings.Index(prompt, delimiter)
	if idx != -1 {
		systemContent := strings.TrimSpace(prompt[:idx])
		userContent := strings.TrimSpace(prompt[idx+len(delimiter):])
		if systemContent != "" {
			reqBody.SystemInstruction = &Content{Parts: []Part{{Text: systemContent}}}
		}

		userContent = strings.TrimSuffix(userContent, "Agent:")
		userContent = strings.TrimSuffix(userContent, "Agent: ")
		userContent = strings.TrimSpace(userContent)
		if strings.Contains(userContent, "Tool Output:") {
			userContent += "\n\n[System Directive]: Tool outputs received above. Continue autonomous implementation to fulfill the user's request. If more tools are required (e.g. write_to_file, replace_file_content, run_command), output tool calls now. Otherwise, summarize the completed work."
		}

		// Re-process parts for user prompt to include images if they exist
		userParts := []Part{{Text: userContent}}
		for _, b64Img := range images {
			userParts = append(userParts, Part{
				InlineData: &InlineData{
					MimeType: "image/jpeg",
					Data:     b64Img,
				},
			})
		}
		reqBody.Contents = []Content{{Parts: userParts}}
	} else {
		reqBody.Contents = []Content{{Parts: parts}}
	}

	if len(tools) > 0 {
		reqBody.Tools = MCPToGeminiTools(tools)
	}

	if strings.Contains(targetModel, "thinking") {
		reqBody.GenerationConfig = &GenerationConfig{
			ThinkingConfig: &ThinkingConfig{ThinkingBudget: 2048},
		}
	}

	// Candidate endpoints to try if 404 or 429 quota exhaustion is encountered
	endpointsToTry := []string{targetModel, "gemini-3.8-flash", "gemini-3.7-flash", "gemini-3.6-flash", "gemini-3.1-pro-preview", "gemini-2.0-flash"}
	seen := make(map[string]bool)

	var resp *http.Response
	var err error

	for _, ep := range endpointsToTry {
		if seen[ep] {
			continue
		}
		seen[ep] = true

		var url string
		if isVertexMode {
			url = authutil.BuildVertexEndpoint(vertexCreds.Mode, vertexCreds.Location, vertexCreds.ProjectID, ep)
		} else {
			url = fmt.Sprintf("%s/v1beta/models/%s:streamGenerateContent?alt=sse", c.BaseURL, ep)
		}

		resp, err = c.executeRequestWithRetry(ctx, apiKey, url, reqBody)

		if err == nil && resp != nil && resp.StatusCode == http.StatusOK {
			break // Successfully got 200 OK response
		}
		if resp != nil && (resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode == http.StatusNotFound) {
			// Close response body and try next model candidate in fallback loop
			resp.Body.Close()
			continue
		}
		if resp != nil && resp.Body != nil {
			break // Keep response for error formatting if other status code
		}
	}

	if err != nil {
		return nil, fmt.Errorf("network/TLS handshake or request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		if resp.StatusCode == http.StatusBadRequest && strings.Contains(string(bodyBytes), "API key not valid") {
			return nil, fmt.Errorf("The Google Cloud API key configured in `mg auth` is invalid or was deleted. Please configure a new one by running `mg auth` again, or set the GEMINI_API_KEY environment variable manually.")
		}
		return nil, fmt.Errorf("unexpected status code from cloud: %d, body: %s", resp.StatusCode, string(bodyBytes))
	}

	var fullResponse strings.Builder
	var toolCalls []cmdparse.ToolCall
	var activeToolCall *cmdparse.ToolCall
	scanner := bufio.NewScanner(resp.Body)
	scanBuf := make([]byte, 1024*1024)
	scanner.Buffer(scanBuf, 10*1024*1024) // 10MB max capacity

	// Latest usage metadata reported by the API (present on stream chunks).
	var usage *UsageMetadata

	// Read SSE
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "data: ") {
			data := strings.TrimPrefix(line, "data: ")
			var genResp GenerateResponse
			if err := json.Unmarshal([]byte(data), &genResp); err == nil {
				if genResp.UsageMetadata != nil {
					usage = genResp.UsageMetadata
				}
				if len(genResp.Candidates) > 0 {
					for _, part := range genResp.Candidates[0].Content.Parts {
						if part.Text != "" {
							fmt.Fprint(out, part.Text)
							fullResponse.WriteString(part.Text)
						}
						if part.FunctionCall != nil {
							fc := part.FunctionCall
							if fc.Name != "" {
								if activeToolCall != nil && activeToolCall.ToolCall == "" {
									activeToolCall.ToolCall = fc.Name
									for k, v := range fc.Args {
										activeToolCall.Arguments[k] = v
									}
								} else if activeToolCall != nil && activeToolCall.ToolCall == fc.Name {
									for k, v := range fc.Args {
										activeToolCall.Arguments[k] = v
									}
								} else {
									if activeToolCall != nil && activeToolCall.ToolCall != "" {
										toolCalls = append(toolCalls, *activeToolCall)
									}
									args := make(map[string]interface{})
									for k, v := range fc.Args {
										args[k] = v
									}
									activeToolCall = &cmdparse.ToolCall{
										ToolCall:  fc.Name,
										Arguments: args,
									}
								}
							} else {
								if activeToolCall == nil {
									activeToolCall = &cmdparse.ToolCall{
										Arguments: make(map[string]interface{}),
									}
								}
								for k, v := range fc.Args {
									activeToolCall.Arguments[k] = v
								}
							}
						}
					}
				}
			}
		}
	}
	if activeToolCall != nil && activeToolCall.ToolCall != "" {
		toolCalls = append(toolCalls, *activeToolCall)
		activeToolCall = nil
	}
	fmt.Fprintln(out) // Final newline

	// Record real token usage against the daily budget. Gemini reports usage
	// metadata on stream chunks; fall back to a ~4 chars/token estimate when
	// the API omitted it (e.g. proxied or error-truncated streams).
	promptTokens, completionTokens := 0, 0
	if usage != nil && (usage.PromptTokenCount > 0 || usage.TotalTokenCount > 0) {
		promptTokens = usage.PromptTokenCount
		completionTokens = usage.CandidatesTokenCount
		if completionTokens == 0 && usage.TotalTokenCount > promptTokens {
			completionTokens = usage.TotalTokenCount - promptTokens
		}
	} else {
		promptTokens = estimateTokens(prompt)
		completionTokens = estimateTokens(fullResponse.String())
	}
	budget.Record(targetModel, promptTokens, completionTokens)

	return &GenerateResult{
		Text:      fullResponse.String(),
		ToolCalls: toolCalls,
	}, scanner.Err()
}

// Generate calls GenerateWithTools with nil tools for backward compatibility.
func (c *Client) Generate(ctx context.Context, model, prompt string, images []string, out io.Writer) (string, error) {
	res, err := c.GenerateWithTools(ctx, model, prompt, images, nil, out)
	if res == nil {
		return "", err
	}
	return res.Text, err
}

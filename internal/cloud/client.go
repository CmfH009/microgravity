package cloud

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/CmfH009/microgravity/internal/authutil"
	"github.com/CmfH009/microgravity/internal/budget"
	"github.com/CmfH009/microgravity/internal/cmdparse"
	"github.com/CmfH009/microgravity/internal/config"
	"github.com/CmfH009/microgravity/internal/httputil"
	"github.com/CmfH009/microgravity/internal/mcp"
	"github.com/CmfH009/microgravity/internal/router"
)

const (
	defaultBaseURL    = "https://generativelanguage.googleapis.com"
	maxErrorBodyBytes = httputil.MaxErrorBodyBytes
)

var (
	// ErrQuotaExhausted indicates the cloud endpoint exhausted quota or billing.
	ErrQuotaExhausted = errors.New("cloud quota or billing exhausted")
	// ErrEmptyResponse indicates an HTTP 200 stream ended with no valid chunks.
	ErrEmptyResponse = errors.New("cloud returned empty response")
)

type Client struct {
	BaseURL     string
	HTTPClient  *http.Client
	APIKey      string
	BearerToken string
	VertexCreds *authutil.VertexCredentials
}

var _ router.Provider = (*Client)(nil)

func NewClient() *Client {
	return &Client{
		BaseURL: defaultBaseURL,
		HTTPClient: httputil.NewClient(httputil.ClientConfig{
			TLSHandshakeTimeout:   15 * time.Second,
			ResponseHeaderTimeout: 30 * time.Second,
			MaxIdleConns:          100,
			MaxIdleConnsPerHost:   10,
			ExpectContinueTimeout: 1 * time.Second,
		}),
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
	Name         string        `json:"name"`
	Description  string        `json:"description"`
	Parameters   *GeminiSchema `json:"parameters,omitempty"`
	DeferLoading bool          `json:"defer_loading,omitempty"`
}

type GeminiTool struct {
	FunctionDeclarations []GeminiFunctionDeclaration `json:"functionDeclarations,omitempty"`
	Type                 string                      `json:"type,omitempty"`
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

// APIError describes an error object embedded in a Gemini HTTP or SSE response.
type APIError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Status  string `json:"status"`
}

type GenerateResponse struct {
	Candidates []struct {
		Content struct {
			Parts []Part `json:"parts"`
		} `json:"content"`
	} `json:"candidates"`
	UsageMetadata *UsageMetadata `json:"usageMetadata,omitempty"`
	Error         *APIError      `json:"error,omitempty"`
}

type GenerateResult = router.GenerateResult

// estimateTokens approximates token count from text length (~4 chars/token).
func estimateTokens(s string) int {
	return httputil.EstimateTokens(s)
}

// MCPToGeminiTools converts MCP tool definitions to Gemini API function declarations.
func MCPToGeminiTools(tools []mcp.Tool) []GeminiTool {
	return MCPToGeminiToolsWithRetrieval(tools, nil)
}

// MCPToGeminiToolsWithRetrieval converts MCP tool definitions and marks non-core tools with
// defer_loading: true plus a tool_search entry when coreTools is provided and non-empty.
func MCPToGeminiToolsWithRetrieval(tools []mcp.Tool, coreTools map[string]bool) []GeminiTool {
	if len(tools) == 0 {
		return nil
	}
	var decls []GeminiFunctionDeclaration
	hasDeferred := false
	for _, t := range tools {
		deferLoad := len(coreTools) > 0 && !coreTools[t.Name]
		if deferLoad {
			hasDeferred = true
		}
		decls = append(decls, GeminiFunctionDeclaration{
			Name:         t.Name,
			Description:  t.Description,
			Parameters:   mcpSchemaToGeminiSchema(t.InputSchema),
			DeferLoading: deferLoad,
		})
	}
	res := []GeminiTool{{FunctionDeclarations: decls}}
	if hasDeferred {
		res = append(res, GeminiTool{Type: "tool_search"})
	}
	return res
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

func (c *Client) httpClient() *http.Client {
	if c != nil && c.HTTPClient != nil {
		return c.HTTPClient
	}
	return &http.Client{Timeout: 10 * time.Minute}
}

func (c *Client) baseURL() string {
	if c == nil || strings.TrimSpace(c.BaseURL) == "" {
		return defaultBaseURL
	}
	return strings.TrimRight(c.BaseURL, "/")
}

func drainAndClose(body io.ReadCloser) {
	httputil.DrainAndClose(body)
}

func readBoundedErrorBody(body io.ReadCloser) string {
	return httputil.ReadBoundedErrorBody(body)
}

func isQuotaBody(body string) bool {
	lower := strings.ToLower(body)
	return strings.Contains(lower, "resource_exhausted") ||
		strings.Contains(lower, "quota") ||
		strings.Contains(lower, "billing") ||
		strings.Contains(lower, "rate limit") ||
		strings.Contains(lower, "ratelimit") ||
		strings.Contains(lower, "insufficient_quota") ||
		strings.Contains(lower, "payment required")
}

func formatCloudStatusError(statusCode int, body string) error {
	if statusCode == http.StatusBadRequest && strings.Contains(body, "API key not valid") {
		return fmt.Errorf("The Google Cloud API key configured in `mg auth` is invalid or was deleted. Please configure a new one by running `mg auth` again, or set the GEMINI_API_KEY environment variable manually (status %d): %s", statusCode, body)
	}
	if statusCode == http.StatusTooManyRequests || statusCode == http.StatusPaymentRequired || isQuotaBody(body) {
		return fmt.Errorf("%w (status %d): %s", ErrQuotaExhausted, statusCode, body)
	}
	return fmt.Errorf("unexpected status code from cloud: %d, body: %s", statusCode, body)
}

func (c *Client) executeRequestWithRetry(ctx context.Context, apiKey, url string, reqBody GenerateRequest) (*http.Response, error) {
	bearer := ""
	if c != nil {
		bearer = c.BearerToken
	}
	return c.executeRequestWithRetryAuth(ctx, apiKey, bearer, url, reqBody)
}

func (c *Client) executeRequestWithRetryAuth(ctx context.Context, apiKey, bearerToken, url string, reqBody GenerateRequest) (*http.Response, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	payload, err := json.Marshal(reqBody)
	if err != nil {
		return nil, err
	}

	maxAttempts := 3
	var lastErr error
	httpClient := c.httpClient()

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(payload))
		if err != nil {
			return nil, err
		}
		if bearerToken != "" {
			req.Header.Set("Authorization", "Bearer "+bearerToken)
		} else if apiKey != "" {
			req.Header.Set("x-goog-api-key", apiKey)
		} else if token := config.GetToken(); token != nil && token.AccessToken != "" {
			req.Header.Set("Authorization", "Bearer "+token.AccessToken)
		}
		req.Header.Set("Content-Type", "application/json")

		var retryAfter time.Duration
		resp, err := httpClient.Do(req)
		if err == nil {
			if isTransientStatus(resp.StatusCode) {
				// Throttling (429) and gateway/server faults (500/502/503/504)
				// are worth retrying on the same model before the caller falls
				// back to a different model endpoint.
				retryAfter = parseRetryAfter(resp.Header.Get("Retry-After"))
				errBody := readBoundedErrorBody(resp.Body)
				if resp.StatusCode == http.StatusTooManyRequests || isQuotaBody(errBody) {
					lastErr = fmt.Errorf("%w: transient cloud server error (status %d): %s", ErrQuotaExhausted, resp.StatusCode, errBody)
					// When the server explicitly reports quota exhaustion without a Retry-After hint,
					// stop retrying the same model so GenerateWithTools can fail over immediately.
					if resp.StatusCode == http.StatusTooManyRequests && retryAfter == 0 && isQuotaBody(errBody) {
						return nil, fmt.Errorf("request failed on attempt %d: %w", attempt, lastErr)
					}
				} else {
					lastErr = fmt.Errorf("transient cloud server error (status %d): %s", resp.StatusCode, errBody)
				}
			} else {
				return resp, nil
			}
		} else {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
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
	if c == nil {
		return nil, fmt.Errorf("cloud client is nil")
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

	targetModel := ResolveCloudModel(model)

	// Budget guardrail: fail fast once the daily token/cost cap is reached
	// instead of silently burning more budget.
	if err := budget.Check(); err != nil {
		return nil, err
	}

	baseURL := c.baseURL()
	isCustomBaseURL := baseURL != defaultBaseURL

	// Check if Vertex AI is configured or requested
	authType := config.GetAuthType()
	isVertex := c.VertexCreds != nil || (!isCustomBaseURL && (strings.HasPrefix(authType, "vertex") || os.Getenv("VERTEX_API_KEY") != "" || os.Getenv("VERTEX_PROJECT_ID") != ""))

	var isVertexMode bool
	var vertexCreds *authutil.VertexCredentials
	var vertexErr error

	if isVertex {
		if c.VertexCreds != nil {
			vertexCreds = c.VertexCreds
			isVertexMode = true
		} else if vCreds, vErr := authutil.ResolveVertexCredentials(ctx); vErr == nil && vCreds != nil {
			vertexCreds = vCreds
			isVertexMode = true
		} else if vErr != nil {
			vertexErr = vErr
		}
	}

	apiKey := c.APIKey
	if apiKey == "" {
		apiKey = os.Getenv("GEMINI_API_KEY")
	}
	if apiKey == "" {
		apiKey = os.Getenv("GOOGLE_API_KEY")
	}
	bearerToken := c.BearerToken

	if isVertexMode {
		if vertexCreds.Mode == "express" {
			apiKey = vertexCreds.APIKey
			bearerToken = ""
		} else if vertexCreds.Token != nil {
			bearerToken = vertexCreds.Token.AccessToken
			apiKey = ""
		}
	} else if apiKey == "" && bearerToken == "" {
		var authErr error
		if validKey, err := authutil.EnsureValidTokenAndAPIKey(); err == nil && validKey != "" {
			apiKey = validKey
		} else {
			authErr = err
			apiKey = config.GetAutoAPIKey()
		}
		if apiKey == "" && bearerToken == "" {
			if token := config.GetToken(); token != nil && token.AccessToken != "" {
				bearerToken = token.AccessToken
			}
		}
		if apiKey == "" && bearerToken == "" {
			if vertexErr != nil {
				return nil, fmt.Errorf("Google account auth failed (vertex resolution: %w). Please run 'mg auth' (AI Studio) or 'mg auth vertex' (Vertex AI)", vertexErr)
			}
			if authErr != nil {
				return nil, fmt.Errorf("Google account auth failed (%w) or API key not provisioned. Please run 'mg auth' (AI Studio) or 'mg auth vertex' (Vertex AI)", authErr)
			}
			return nil, fmt.Errorf("Google account auth failed or API key not provisioned. Please run 'mg auth' (AI Studio) or 'mg auth vertex' (Vertex AI)")
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
	var lastErr error

	for _, ep := range endpointsToTry {
		if seen[ep] {
			continue
		}
		seen[ep] = true
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		var url string
		if isVertexMode {
			url = authutil.BuildVertexEndpoint(vertexCreds.Mode, vertexCreds.Location, vertexCreds.ProjectID, ep)
		} else {
			url = fmt.Sprintf("%s/v1beta/models/%s:streamGenerateContent?alt=sse", baseURL, ep)
		}

		candidateResp, reqErr := c.executeRequestWithRetryAuth(ctx, apiKey, bearerToken, url, reqBody)
		if reqErr != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			lastErr = reqErr
			continue
		}
		if candidateResp == nil {
			lastErr = fmt.Errorf("empty response from cloud endpoint %s", ep)
			continue
		}
		if candidateResp.StatusCode == http.StatusOK {
			resp = candidateResp
			break
		}

		errBody := readBoundedErrorBody(candidateResp.Body)
		statusErr := formatCloudStatusError(candidateResp.StatusCode, errBody)
		lastErr = statusErr

		if candidateResp.StatusCode == http.StatusNotFound {
			continue
		}
		// Billing/quota (402), non-retryable quota errors, and auth errors fail fast without scanning fallback models.
		return nil, statusErr
	}

	if resp == nil {
		if lastErr != nil {
			return nil, fmt.Errorf("network/TLS handshake or request failed: %w", lastErr)
		}
		return nil, fmt.Errorf("cloud request failed: no endpoints succeeded")
	}
	defer drainAndClose(resp.Body)

	var fullResponse strings.Builder
	var toolCalls []cmdparse.ToolCall
	var activeToolCall *cmdparse.ToolCall
	scanner := bufio.NewScanner(resp.Body)
	scanBuf := make([]byte, 1024*1024)
	scanner.Buffer(scanBuf, 10*1024*1024) // 10MB max capacity

	// Latest usage metadata reported by the API (present on stream chunks).
	var usage *UsageMetadata
	validChunks := 0
	malformedChunks := 0
	var lastParseErr error

	// Read SSE
	for scanner.Scan() {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		line := scanner.Text()
		var data string
		if strings.HasPrefix(line, "data:") {
			data = strings.TrimPrefix(line, "data:")
			data = strings.TrimPrefix(data, " ")
		} else if strings.HasPrefix(strings.TrimSpace(line), "{") {
			data = strings.TrimSpace(line)
		} else {
			continue
		}

		trimmed := strings.TrimSpace(data)
		if trimmed == "" || trimmed == "[DONE]" {
			continue
		}

		var genResp GenerateResponse
		if err := json.Unmarshal([]byte(trimmed), &genResp); err != nil {
			malformedChunks++
			lastParseErr = err
			continue
		}
		if genResp.Error != nil {
			errMsg := genResp.Error.Message
			if genResp.Error.Status != "" {
				errMsg = genResp.Error.Status + ": " + errMsg
			}
			if genResp.Error.Code == http.StatusTooManyRequests ||
				genResp.Error.Code == http.StatusPaymentRequired ||
				strings.EqualFold(genResp.Error.Status, "RESOURCE_EXHAUSTED") ||
				isQuotaBody(errMsg) {
				return nil, fmt.Errorf("%w: cloud stream error (code %d): %s", ErrQuotaExhausted, genResp.Error.Code, errMsg)
			}
			return nil, fmt.Errorf("cloud stream error (code %d): %s", genResp.Error.Code, errMsg)
		}

		validChunks++
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
							if activeToolCall.Arguments == nil {
								activeToolCall.Arguments = make(map[string]interface{})
							}
							for k, v := range fc.Args {
								activeToolCall.Arguments[k] = v
							}
						} else if activeToolCall != nil && activeToolCall.ToolCall == fc.Name {
							if activeToolCall.Arguments == nil {
								activeToolCall.Arguments = make(map[string]interface{})
							}
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
						if activeToolCall.Arguments == nil {
							activeToolCall.Arguments = make(map[string]interface{})
						}
						for k, v := range fc.Args {
							activeToolCall.Arguments[k] = v
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

	if scanErr := scanner.Err(); scanErr != nil {
		if ctx.Err() != nil {
			return nil, fmt.Errorf("cloud stream cancelled: %w", ctx.Err())
		}
		return nil, fmt.Errorf("cloud stream read error: %w", scanErr)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if validChunks == 0 {
		if malformedChunks > 0 {
			return nil, fmt.Errorf("malformed cloud SSE stream (%d invalid chunks): %w", malformedChunks, lastParseErr)
		}
		return nil, fmt.Errorf("%w: %w", ErrEmptyResponse, io.ErrUnexpectedEOF)
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
	}, nil
}

// Generate calls GenerateWithTools with nil tools for backward compatibility.
func (c *Client) Generate(ctx context.Context, model, prompt string, images []string, out io.Writer) (string, error) {
	res, err := c.GenerateWithTools(ctx, model, prompt, images, nil, out)
	if res == nil {
		return "", err
	}
	return res.Text, err
}

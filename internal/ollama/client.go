package ollama

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
	"time"

	"github.com/CmfH009/microgravity/internal/budget"
	"github.com/CmfH009/microgravity/internal/cmdparse"
	"github.com/CmfH009/microgravity/internal/mcp"
)

var EnableDebug = false

type Client struct {
	BaseURL    string
	HTTPClient *http.Client
}

func NewClient(baseURL string) *Client {
	if baseURL == "" {
		baseURL = "http://localhost:11434"
	}
	return &Client{
		BaseURL: baseURL,
		HTTPClient: &http.Client{
			Timeout: 10 * time.Minute,
		},
	}
}

func EnsureOllama() {
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get("http://localhost:11434/")
	if err == nil {
		resp.Body.Close()
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

	req, err := http.NewRequestWithContext(ctx, "POST", c.BaseURL+"/api/chat", buf)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	if EnableDebug {
		fmt.Printf("[DEBUG] Sending request to Ollama API for model %s...\n", model)
	}

	// 30s timeout for initial response / model load
	clientCtx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()

	reqWithTimeout := req.WithContext(clientCtx)
	resp, err := c.HTTPClient.Do(reqWithTimeout)
	if err != nil {
		if EnableDebug {
			fmt.Printf("[DEBUG] HTTP Request failed: %v\n", err)
		}
		return nil, fmt.Errorf("local model %s load timeout (45s): %w", model, err)
	}
	defer resp.Body.Close()
	if EnableDebug {
		fmt.Printf("[DEBUG] HTTP Request succeeded. Status: %d\n", resp.StatusCode)
	}

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		if resp.StatusCode == 404 {
			return nil, fmt.Errorf("The local AI model is currently downloading in the background! Please wait a moment and try again.")
		}
		return nil, fmt.Errorf("unexpected status code: %d, body: %s", resp.StatusCode, string(bodyBytes))
	}

	var fullResponse strings.Builder
	var toolCalls []cmdparse.ToolCall
	scanner := bufio.NewScanner(resp.Body)
	scanBuf := make([]byte, 1024*1024)
	scanner.Buffer(scanBuf, 10*1024*1024) // 10MB max capacity

	chunks := 0
	for scanner.Scan() {
		chunks++
		line := scanner.Bytes()
		var genResp ChatResponse
		err := json.Unmarshal(line, &genResp)
		if err != nil {
			if EnableDebug {
				fmt.Printf("[DEBUG] Failed to unmarshal chunk %d: %v. Line snippet: %s\n", chunks, err, string(line[:min(len(line), 100)]))
			}
			continue
		}
		if chunks == 1 && EnableDebug {
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
			if EnableDebug {
				fmt.Printf("[DEBUG] Received done=true on chunk %d\n", chunks)
			}
			break
		}
	}

	if err := scanner.Err(); err != nil && EnableDebug {
		fmt.Printf("[DEBUG] Scanner error after %d chunks: %v\n", chunks, err)
	}

	fmt.Fprintln(out) // Print final newline

	// Record token usage against the daily budget. Ollama does not report
	// token counts in its streaming response, so fall back to a ~4 chars/token
	// estimate for both prompt and completion.
	budget.Record(model, estimateTokens(prompt), estimateTokens(fullResponse.String()))

	return &GenerateResult{
		Text:      fullResponse.String(),
		ToolCalls: toolCalls,
	}, scanner.Err()
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
}

func (c *Client) Pull(ctx context.Context, model string, onProgress func(PullProgress)) error {
	reqBody := PullRequest{
		Model:  model,
		Stream: true,
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

	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		if onProgress != nil {
			var p PullProgress
			if err := json.Unmarshal(scanner.Bytes(), &p); err == nil {
				onProgress(p)
			}
		}
	}
	return scanner.Err()
}

type ModelListResponse struct {
	Models []struct {
		Name string `json:"name"`
	} `json:"models"`
}

// HasModel checks if a model is already downloaded locally.
func (c *Client) HasModel(ctx context.Context, modelName string) (bool, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", c.BaseURL+"/api/tags", nil)
	if err != nil {
		return false, err
	}

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return false, fmt.Errorf("failed to get tags: %d", resp.StatusCode)
	}

	var list ModelListResponse
	if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
		return false, err
	}

	for _, m := range list.Models {
		if m.Name == modelName {
			return true, nil
		}
		// Also check without tag if modelName has no tag
		if !strings.Contains(modelName, ":") && strings.HasPrefix(m.Name, modelName+":latest") {
			return true, nil
		}
	}
	return false, nil
}

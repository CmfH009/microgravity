package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/CmfH009/microgravity/internal/cloud"
	"github.com/CmfH009/microgravity/internal/ollama"
	"github.com/CmfH009/microgravity/internal/router"
	"github.com/spf13/cobra"
)

var port string

type CompletionRequest struct {
	Model       string  `json:"model"`
	Prompt      string  `json:"prompt"`
	Stream      bool    `json:"stream"`
	Temperature float64 `json:"temperature,omitempty"`
	MaxTokens   int     `json:"max_tokens,omitempty"`
	Messages    []struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	} `json:"messages"`
}

type openAIChatStreamWriter struct {
	w       http.ResponseWriter
	flusher http.Flusher
	id      string
	model   string
}

func (s *openAIChatStreamWriter) Write(p []byte) (n int, err error) {
	chunk := string(p)
	if chunk == "" {
		return len(p), nil
	}
	chunkObj := map[string]interface{}{
		"id":      s.id,
		"object":  "chat.completion.chunk",
		"created": time.Now().Unix(),
		"model":   s.model,
		"choices": []map[string]interface{}{
			{
				"index": 0,
				"delta": map[string]interface{}{
					"content": chunk,
				},
				"finish_reason": nil,
			},
		},
	}
	data, err := json.Marshal(chunkObj)
	if err == nil {
		fmt.Fprintf(s.w, "data: %s\n\n", data)
		if s.flusher != nil {
			s.flusher.Flush()
		}
	}
	return len(p), nil
}

type openAITextStreamWriter struct {
	w       http.ResponseWriter
	flusher http.Flusher
	id      string
	model   string
}

func (s *openAITextStreamWriter) Write(p []byte) (n int, err error) {
	chunk := string(p)
	if chunk == "" {
		return len(p), nil
	}
	chunkObj := map[string]interface{}{
		"id":      s.id,
		"object":  "text_completion",
		"created": time.Now().Unix(),
		"model":   s.model,
		"choices": []map[string]interface{}{
			{
				"index":         0,
				"text":          chunk,
				"finish_reason": nil,
			},
		},
	}
	data, err := json.Marshal(chunkObj)
	if err == nil {
		fmt.Fprintf(s.w, "data: %s\n\n", data)
		if s.flusher != nil {
			s.flusher.Flush()
		}
	}
	return len(p), nil
}

func extractPrompt(body CompletionRequest) string {
	prompt := body.Prompt
	if prompt == "" && len(body.Messages) > 0 {
		if len(body.Messages) == 1 {
			prompt = body.Messages[0].Content
		} else {
			var sb strings.Builder
			for _, m := range body.Messages {
				if m.Role == "system" {
					sb.WriteString(m.Content)
					sb.WriteString("\n---CONVERSATION---\n")
				} else {
					sb.WriteString(strings.ToUpper(m.Role))
					sb.WriteString(": ")
					sb.WriteString(m.Content)
					sb.WriteString("\n")
				}
			}
			prompt = sb.String()
		}
	}
	return strings.TrimSpace(prompt)
}

func resolveModelAndBackend(requestedModel string, prompt string, r *router.Router, defaultOllama string, defaultCloud string) (string, bool) {
	lower := strings.ToLower(requestedModel)
	if strings.Contains(lower, "cloud") || strings.Contains(lower, "gemini") || strings.Contains(lower, "9b") {
		return defaultCloud, false
	}
	if strings.Contains(lower, "ollama") || strings.Contains(lower, "2b") || strings.Contains(lower, "local") {
		return defaultOllama, true
	}
	complexity := r.Classify(prompt)
	if complexity == router.ComplexitySimple {
		return defaultOllama, true
	}
	return defaultCloud, false
}

var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Start a local API server for IDE code completion",
	Run: func(cmd *cobra.Command, args []string) {
		ollama.EnsureOllama()
		ollamaClient := ollama.NewClient(baseURL)
		r := router.NewRouter()

		// Legacy /generate endpoint that IDEs can hook into
		http.HandleFunc("/generate", func(w http.ResponseWriter, req *http.Request) {
			w.Header().Set("Access-Control-Allow-Origin", "*")
			w.Header().Set("Access-Control-Allow-Headers", "*")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS, PUT, DELETE")
			if req.Method == "OPTIONS" {
				return
			}

			var body CompletionRequest
			_ = json.NewDecoder(req.Body).Decode(&body)

			prompt := extractPrompt(body)
			if prompt == "" {
				http.Error(w, "Prompt is empty", 400)
				return
			}

			activeModel, isSimple := resolveModelAndBackend(body.Model, prompt, r, modelName, cloudModel)
			fmt.Printf("[API] Request received. Model: %s (local: %v)\n", activeModel, isSimple)

			if isSimple {
				_, err := ollamaClient.Generate(req.Context(), activeModel, prompt, nil, w)
				if err != nil {
					fmt.Printf("API Error: %v\n", err)
				}
			} else {
				cloudClient := cloud.NewClient()
				_, err := cloudClient.Generate(req.Context(), activeModel, prompt, nil, w)
				if err != nil {
					fmt.Printf("API Error: %v\n", err)
				}
			}
		})

		// Status and Health endpoints
		http.HandleFunc("/health", func(w http.ResponseWriter, req *http.Request) {
			w.Header().Set("Access-Control-Allow-Origin", "*")
			w.Header().Set("Access-Control-Allow-Headers", "*")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS, PUT, DELETE")
			if req.Method == "OPTIONS" {
				return
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{"status": "healthy", "service": "microgravity-serve"})
		})

		http.HandleFunc("/status", func(w http.ResponseWriter, req *http.Request) {
			w.Header().Set("Access-Control-Allow-Origin", "*")
			w.Header().Set("Access-Control-Allow-Headers", "*")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS, PUT, DELETE")
			if req.Method == "OPTIONS" {
				return
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"status":    "online",
				"service":   "microgravity-serve",
				"port":      port,
				"model":     modelName,
				"cloud":     cloudModel,
				"endpoints": []string{"/health", "/status", "/generate", "/v1/models", "/v1/chat/completions", "/v1/completions"},
			})
		})

		// OpenAI compatible models endpoint
		modelsHandler := func(w http.ResponseWriter, req *http.Request) {
			w.Header().Set("Access-Control-Allow-Origin", "*")
			w.Header().Set("Access-Control-Allow-Headers", "*")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS, PUT, DELETE")
			if req.Method == "OPTIONS" {
				return
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"object": "list",
				"data": []map[string]interface{}{
					{"id": modelName, "object": "model", "owned_by": "ollama"},
					{"id": cloudModel, "object": "model", "owned_by": "cloud"},
					{"id": "microgravity", "object": "model", "owned_by": "microgravity"},
				},
			})
		}
		http.HandleFunc("/v1/models", modelsHandler)
		http.HandleFunc("/models", modelsHandler)

		// OpenAI compatible chat completions endpoint
		http.HandleFunc("/v1/chat/completions", func(w http.ResponseWriter, req *http.Request) {
			w.Header().Set("Access-Control-Allow-Origin", "*")
			w.Header().Set("Access-Control-Allow-Headers", "*")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS, PUT, DELETE")
			if req.Method == "OPTIONS" {
				return
			}

			var body CompletionRequest
			if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusBadRequest)
				json.NewEncoder(w).Encode(map[string]interface{}{
					"error": map[string]interface{}{"message": "Invalid JSON payload", "type": "invalid_request_error"},
				})
				return
			}

			prompt := extractPrompt(body)
			if prompt == "" {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusBadRequest)
				json.NewEncoder(w).Encode(map[string]interface{}{
					"error": map[string]interface{}{"message": "Prompt is empty", "type": "invalid_request_error"},
				})
				return
			}

			activeModel, isSimple := resolveModelAndBackend(body.Model, prompt, r, modelName, cloudModel)
			fmt.Printf("[API v1] Chat completion received. Model: %s (local: %v)\n", activeModel, isSimple)

			reqId := fmt.Sprintf("chatcmpl-%d", time.Now().UnixNano())

			if body.Stream {
				w.Header().Set("Content-Type", "text/event-stream")
				w.Header().Set("Cache-Control", "no-cache")
				w.Header().Set("Connection", "keep-alive")

				flusher, _ := w.(http.Flusher)
				if flusher != nil {
					flusher.Flush()
				}

				streamWriter := &openAIChatStreamWriter{
					w:       w,
					flusher: flusher,
					id:      reqId,
					model:   activeModel,
				}

				if isSimple {
					_, _ = ollamaClient.Generate(req.Context(), activeModel, prompt, nil, streamWriter)
				} else {
					cloudClient := cloud.NewClient()
					_, _ = cloudClient.Generate(req.Context(), activeModel, prompt, nil, streamWriter)
				}

				doneObj := map[string]interface{}{
					"id":      reqId,
					"object":  "chat.completion.chunk",
					"created": time.Now().Unix(),
					"model":   activeModel,
					"choices": []map[string]interface{}{
						{
							"index":         0,
							"delta":         map[string]interface{}{},
							"finish_reason": "stop",
						},
					},
				}
				doneBytes, _ := json.Marshal(doneObj)
				fmt.Fprintf(w, "data: %s\n\ndata: [DONE]\n\n", doneBytes)
				if flusher != nil {
					flusher.Flush()
				}
				return
			}

			// Non-streaming response:
			var respText string
			var err error
			if isSimple {
				respText, err = ollamaClient.Generate(req.Context(), activeModel, prompt, nil, io.Discard)
			} else {
				cloudClient := cloud.NewClient()
				respText, err = cloudClient.Generate(req.Context(), activeModel, prompt, nil, io.Discard)
			}

			if err != nil {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusInternalServerError)
				json.NewEncoder(w).Encode(map[string]interface{}{
					"error": map[string]interface{}{"message": err.Error(), "type": "api_error"},
				})
				return
			}

			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"id":      reqId,
				"object":  "chat.completion",
				"created": time.Now().Unix(),
				"model":   activeModel,
				"choices": []map[string]interface{}{
					{
						"index": 0,
						"message": map[string]interface{}{
							"role":    "assistant",
							"content": strings.TrimSpace(respText),
						},
						"finish_reason": "stop",
					},
				},
				"usage": map[string]interface{}{
					"prompt_tokens":     len(prompt) / 4,
					"completion_tokens": len(respText) / 4,
					"total_tokens":      (len(prompt) + len(respText)) / 4,
				},
			})
		})

		// OpenAI compatible legacy completions endpoint
		http.HandleFunc("/v1/completions", func(w http.ResponseWriter, req *http.Request) {
			w.Header().Set("Access-Control-Allow-Origin", "*")
			w.Header().Set("Access-Control-Allow-Headers", "*")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS, PUT, DELETE")
			if req.Method == "OPTIONS" {
				return
			}

			var body CompletionRequest
			if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusBadRequest)
				json.NewEncoder(w).Encode(map[string]interface{}{
					"error": map[string]interface{}{"message": "Invalid JSON payload", "type": "invalid_request_error"},
				})
				return
			}

			prompt := extractPrompt(body)
			if prompt == "" {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusBadRequest)
				json.NewEncoder(w).Encode(map[string]interface{}{
					"error": map[string]interface{}{"message": "Prompt is empty", "type": "invalid_request_error"},
				})
				return
			}

			activeModel, isSimple := resolveModelAndBackend(body.Model, prompt, r, modelName, cloudModel)
			reqId := fmt.Sprintf("cmpl-%d", time.Now().UnixNano())

			if body.Stream {
				w.Header().Set("Content-Type", "text/event-stream")
				w.Header().Set("Cache-Control", "no-cache")
				w.Header().Set("Connection", "keep-alive")

				flusher, _ := w.(http.Flusher)
				if flusher != nil {
					flusher.Flush()
				}

				writer := &openAITextStreamWriter{
					w:       w,
					flusher: flusher,
					id:      reqId,
					model:   activeModel,
				}

				if isSimple {
					_, _ = ollamaClient.Generate(req.Context(), activeModel, prompt, nil, writer)
				} else {
					cloudClient := cloud.NewClient()
					_, _ = cloudClient.Generate(req.Context(), activeModel, prompt, nil, writer)
				}

				doneObj := map[string]interface{}{
					"id":      reqId,
					"object":  "text_completion",
					"created": time.Now().Unix(),
					"model":   activeModel,
					"choices": []map[string]interface{}{
						{
							"index":         0,
							"text":          "",
							"finish_reason": "stop",
						},
					},
				}
				doneBytes, _ := json.Marshal(doneObj)
				fmt.Fprintf(w, "data: %s\n\ndata: [DONE]\n\n", doneBytes)
				if flusher != nil {
					flusher.Flush()
				}
				return
			}

			var respText string
			var err error
			if isSimple {
				respText, err = ollamaClient.Generate(req.Context(), activeModel, prompt, nil, io.Discard)
			} else {
				cloudClient := cloud.NewClient()
				respText, err = cloudClient.Generate(req.Context(), activeModel, prompt, nil, io.Discard)
			}

			if err != nil {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusInternalServerError)
				json.NewEncoder(w).Encode(map[string]interface{}{
					"error": map[string]interface{}{"message": err.Error(), "type": "api_error"},
				})
				return
			}

			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"id":      reqId,
				"object":  "text_completion",
				"created": time.Now().Unix(),
				"model":   activeModel,
				"choices": []map[string]interface{}{
					{
						"index":         0,
						"text":          strings.TrimSpace(respText),
						"finish_reason": "stop",
					},
				},
				"usage": map[string]interface{}{
					"prompt_tokens":     len(prompt) / 4,
					"completion_tokens": len(respText) / 4,
					"total_tokens":      (len(prompt) + len(respText)) / 4,
				},
			})
		})

		fmt.Printf("🚀 Gemma Code Completion Server running on http://localhost:%s\n", port)
		fmt.Printf("Point your IDE extension (like Continue.dev) to http://localhost:%s/v1 or /generate\n", port)
		if err := http.ListenAndServe(":"+port, nil); err != nil {
			fmt.Fprintf(os.Stderr, "Server failed: %v\n", err)
		}
	},
}

func init() {
	serveCmd.Flags().StringVarP(&port, "port", "p", "8082", "Port to listen on")
	serveCmd.Flags().StringVarP(&modelName, "serve-model", "m", "gemma2:2b", "Ollama model")
	serveCmd.Flags().StringVarP(&baseURL, "serve-url", "u", "http://localhost:11434", "Ollama URL")
	serveCmd.Flags().StringVarP(&cloudModel, "serve-cloud-model", "c", "gemma-2-9b-it", "Cloud model")
	rootCmd.AddCommand(serveCmd)
}

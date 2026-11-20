package indexer

import (
	"bytes"
	"context"
	"encoding/json"
	"hash/fnv"
	"math"
	"net/http"
	"strings"
	"time"
)

type Embedder interface {
	Embed(ctx context.Context, text string) ([]float32, error)
}

type LexicalEmbedder struct {
	Dimension int
}

func NewLexicalEmbedder(dim int) *LexicalEmbedder {
	if dim <= 0 {
		dim = 384
	}
	return &LexicalEmbedder{Dimension: dim}
}

func (l *LexicalEmbedder) Embed(ctx context.Context, text string) ([]float32, error) {
	vec := make([]float32, l.Dimension)
	words := strings.Fields(strings.ToLower(text))
	if len(words) == 0 {
		return vec, nil
	}

	for _, word := range words {
		h := fnv.New32a()
		h.Write([]byte(word))
		idx := int(h.Sum32()) % l.Dimension
		vec[idx] += 1.0
	}

	// Normalize vector L2 norm
	var norm float64
	for _, val := range vec {
		norm += float64(val * val)
	}
	norm = math.Sqrt(norm)
	if norm > 0 {
		for i := range vec {
			vec[i] = float32(float64(vec[i]) / norm)
		}
	}

	return vec, nil
}

type HybridEmbedder struct {
	OllamaBaseURL string
	Model         string
	HTTPClient    *http.Client
	Fallback      Embedder
}

func NewHybridEmbedder(ollamaURL, model string) *HybridEmbedder {
	if ollamaURL == "" {
		ollamaURL = "http://localhost:11434"
	}
	if model == "" {
		model = "nomic-embed-text"
	}
	return &HybridEmbedder{
		OllamaBaseURL: ollamaURL,
		Model:         model,
		HTTPClient:    &http.Client{Timeout: 3 * time.Second},
		Fallback:      NewLexicalEmbedder(384),
	}
}

type ollamaEmbedRequest struct {
	Model  string `json:"model"`
	Prompt string `json:"prompt"`
}

type ollamaEmbedResponse struct {
	Embedding []float32 `json:"embedding"`
}

func (h *HybridEmbedder) Embed(ctx context.Context, text string) ([]float32, error) {
	reqBody := ollamaEmbedRequest{
		Model:  h.Model,
		Prompt: text,
	}
	buf := new(bytes.Buffer)
	if err := json.NewEncoder(buf).Encode(reqBody); err != nil {
		return h.Fallback.Embed(ctx, text)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", h.OllamaBaseURL+"/api/embeddings", buf)
	if err != nil {
		return h.Fallback.Embed(ctx, text)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := h.HTTPClient.Do(req)
	if err != nil || resp.StatusCode != http.StatusOK {
		if resp != nil {
			resp.Body.Close()
		}
		return h.Fallback.Embed(ctx, text)
	}
	defer resp.Body.Close()

	var embedResp ollamaEmbedResponse
	if err := json.NewDecoder(resp.Body).Decode(&embedResp); err != nil || len(embedResp.Embedding) == 0 {
		return h.Fallback.Embed(ctx, text)
	}

	return embedResp.Embedding, nil
}

package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/CmfH009/microgravity/internal/config"
	"github.com/CmfH009/microgravity/internal/huggingface"
	"github.com/CmfH009/microgravity/internal/ollama"
)

func setupCmdPullTestEnv(t *testing.T) string {
	t.Helper()
	tmp := setupCmdModelsTestEnv(t)

	origGGUF := pullGGUF
	origOllama := pullOllama
	origDir := pullDir
	origQuant := pullQuant
	origSelect := pullSelect

	t.Cleanup(func() {
		pullGGUF = origGGUF
		pullOllama = origOllama
		pullDir = origDir
		pullQuant = origQuant
		pullSelect = origSelect
	})
	return tmp
}

func TestPullCmd_HFSchemeDirectGGUF(t *testing.T) {
	tmp := setupCmdPullTestEnv(t)
	const payload = "DIRECT_HF_GGUF_PAYLOAD_DATA"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/models/Qwen/Qwen2.5-Coder-7B-Instruct-GGUF":
			_, _ = w.Write([]byte(`{"id":"Qwen/Qwen2.5-Coder-7B-Instruct-GGUF","siblings":[
				{"rfilename":"qwen-q4_k_m.gguf","size":4000000000},
				{"rfilename":"qwen-q8_0.gguf","size":8000000000}
			]}`))
		case "/Qwen/Qwen2.5-Coder-7B-Instruct-GGUF/resolve/main/qwen-q4_k_m.gguf":
			_, _ = w.Write([]byte(payload))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	hfClientFactory = func() *huggingface.Client { return huggingface.NewClient(srv.URL) }
	availableRAMGBFn = func(ctx context.Context) float64 { return 16.0 }

	destDir := filepath.Join(tmp, "models")
	var buf bytes.Buffer
	pullCmd.SetOut(&buf)
	pullDir = destDir
	pullSelect = true

	if err := pullCmd.RunE(pullCmd, []string{"hf://Qwen/Qwen2.5-Coder-7B-Instruct-GGUF:Q4_K_M"}); err != nil {
		t.Fatalf("pull hf:// failed: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(destDir, "qwen-q4_k_m.gguf"))
	if err != nil || string(got) != payload {
		t.Fatalf("expected downloaded GGUF content, err=%v got=%q", err, string(got))
	}
	if active := config.GetActiveModel(); active != "qwen-q4_k_m.gguf" {
		t.Fatalf("expected active model qwen-q4_k_m.gguf, got %q", active)
	}
}

func TestPullCmd_HFCoOllamaAndGGUFFlag(t *testing.T) {
	t.Run("hf.co/... defaults to Ollama pull", func(t *testing.T) {
		setupCmdPullTestEnv(t)

		var pulledModel string
		ollamaSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/api/tags":
				_, _ = w.Write([]byte(`{"models":[]}`))
			case "/api/pull":
				body, _ := io.ReadAll(r.Body)
				var req ollama.PullRequest
				_ = json.Unmarshal(body, &req)
				pulledModel = req.Model
				_, _ = w.Write([]byte(`{"status":"pulling manifest"}` + "\n" + `{"status":"downloading","digest":"sha256:abc","total":100,"completed":100}` + "\n" + `{"status":"success"}` + "\n"))
			default:
				w.WriteHeader(http.StatusNotFound)
			}
		}))
		defer ollamaSrv.Close()

		baseURL = ollamaSrv.URL
		ollamaClientFactory = ollama.NewClient

		var buf bytes.Buffer
		pullCmd.SetOut(&buf)
		pullSelect = true

		if err := pullCmd.RunE(pullCmd, []string{"hf.co/Qwen/Qwen2.5-Coder-7B-Instruct-GGUF:Q4_K_M"}); err != nil {
			t.Fatalf("pull hf.co failed: %v", err)
		}

		want := "hf.co/Qwen/Qwen2.5-Coder-7B-Instruct-GGUF:Q4_K_M"
		if pulledModel != want {
			t.Fatalf("expected Ollama pull %q, got %q", want, pulledModel)
		}
		if active := config.GetActiveModel(); active != want {
			t.Fatalf("expected active model %q, got %q", want, active)
		}
	})

	t.Run("hf.co/... with --gguf downloads GGUF directly", func(t *testing.T) {
		tmp := setupCmdPullTestEnv(t)
		const payload = "FORCED_GGUF_PAYLOAD"

		hfSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/api/models/unsloth/Phi-4-GGUF":
				_, _ = w.Write([]byte(`{"id":"unsloth/Phi-4-GGUF","siblings":[{"rfilename":"phi-4-Q4_K_M.gguf","size":4000000000}]}`))
			case "/unsloth/Phi-4-GGUF/resolve/main/phi-4-Q4_K_M.gguf":
				_, _ = w.Write([]byte(payload))
			default:
				w.WriteHeader(http.StatusNotFound)
			}
		}))
		defer hfSrv.Close()

		hfClientFactory = func() *huggingface.Client { return huggingface.NewClient(hfSrv.URL) }
		availableRAMGBFn = func(ctx context.Context) float64 { return 16.0 }

		destDir := filepath.Join(tmp, "models")
		var buf bytes.Buffer
		pullCmd.SetOut(&buf)
		pullGGUF = true
		pullDir = destDir

		if err := pullCmd.RunE(pullCmd, []string{"hf.co/unsloth/Phi-4-GGUF"}); err != nil {
			t.Fatalf("pull hf.co --gguf failed: %v", err)
		}
		got, err := os.ReadFile(filepath.Join(destDir, "phi-4-Q4_K_M.gguf"))
		if err != nil || string(got) != payload {
			t.Fatalf("expected downloaded GGUF, err=%v got=%q", err, string(got))
		}
	})

	t.Run("hf:// non-GGUF repo falls back to Ollama pull", func(t *testing.T) {
		setupCmdPullTestEnv(t)

		hfSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"id":"owner/safetensors-only","siblings":[{"rfilename":"model.safetensors","size":1024}]}`))
		}))
		defer hfSrv.Close()

		var pulledModel string
		ollamaSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/api/tags":
				_, _ = w.Write([]byte(`{"models":[]}`))
			case "/api/pull":
				body, _ := io.ReadAll(r.Body)
				var req ollama.PullRequest
				_ = json.Unmarshal(body, &req)
				pulledModel = req.Model
				_, _ = w.Write([]byte(`{"status":"success"}` + "\n"))
			default:
				w.WriteHeader(http.StatusNotFound)
			}
		}))
		defer ollamaSrv.Close()

		hfClientFactory = func() *huggingface.Client { return huggingface.NewClient(hfSrv.URL) }
		baseURL = ollamaSrv.URL
		ollamaClientFactory = ollama.NewClient

		var buf bytes.Buffer
		pullCmd.SetOut(&buf)
		if err := pullCmd.RunE(pullCmd, []string{"hf://owner/safetensors-only"}); err != nil {
			t.Fatalf("pull non-gguf fallback failed: %v", err)
		}
		if pulledModel != "hf.co/owner/safetensors-only" {
			t.Fatalf("expected Ollama fallback hf.co/owner/safetensors-only, got %q", pulledModel)
		}
	})
}

func TestPullCmd_StandardOllamaAndFlagValidation(t *testing.T) {
	setupCmdPullTestEnv(t)

	var pulled string
	ollamaSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/tags":
			_, _ = w.Write([]byte(`{"models":[]}`))
		case "/api/pull":
			body, _ := io.ReadAll(r.Body)
			var req ollama.PullRequest
			_ = json.Unmarshal(body, &req)
			pulled = req.Model
			// Short digest verifies no out-of-bounds panic
			_, _ = w.Write([]byte(`{"status":"downloading","digest":"short","total":50,"completed":50}` + "\n" + `{"status":"success"}` + "\n"))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer ollamaSrv.Close()

	baseURL = ollamaSrv.URL
	ollamaClientFactory = ollama.NewClient

	var buf bytes.Buffer
	pullCmd.SetOut(&buf)
	if err := pullCmd.RunE(pullCmd, []string{"gemma4:e2b"}); err != nil {
		t.Fatalf("standard ollama pull failed: %v", err)
	}
	if pulled != "gemma4:e2b" {
		t.Fatalf("expected gemma4:e2b pull, got %q", pulled)
	}

	// Conflicting flags
	pullGGUF = true
	pullOllama = true
	err := pullCmd.RunE(pullCmd, []string{"hf://Qwen/Test"})
	if err == nil || !strings.Contains(err.Error(), "cannot specify both") {
		t.Fatalf("expected conflicting flags error, got %v", err)
	}
}

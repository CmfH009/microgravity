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
	"github.com/CmfH009/microgravity/internal/models"
	"github.com/CmfH009/microgravity/internal/ollama"
)

func setupCmdModelsTestEnv(t *testing.T) string {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("USERPROFILE", tmp)
	t.Setenv("MG_MODELS_DIR", filepath.Join(tmp, "models"))

	origScanner := modelsScannerFactory
	origHF := hfClientFactory
	origOllama := ollamaClientFactory
	origEnsure := ensureOllamaFn
	origRAM := availableRAMGBFn
	origBaseURL := baseURL

	origListLocal := modelsListLocal
	origListDownloaded := modelsListDownloaded
	origListCategory := modelsListCategory
	origListJSON := modelsListJSON
	origSearchLimit := modelsSearchLimit
	origSearchGGUF := modelsSearchGGUF
	origSearchSort := modelsSearchSort
	origSearchJSON := modelsSearchJSON
	origHFQuant := modelsHFQuant
	origHFDir := modelsHFDir
	origHFOllama := modelsHFOllama
	origHFSelect := modelsHFSelect
	origHFURL := modelsHFURL
	origAutoQuant := modelsAutoQuant
	origAutoDir := modelsAutoDir
	origAutoOllama := modelsAutoOllama
	origAutoSelect := modelsAutoSelect
	origAutoURL := modelsAutoURL

	t.Cleanup(func() {
		modelsScannerFactory = origScanner
		hfClientFactory = origHF
		ollamaClientFactory = origOllama
		ensureOllamaFn = origEnsure
		availableRAMGBFn = origRAM
		baseURL = origBaseURL

		modelsListLocal = origListLocal
		modelsListDownloaded = origListDownloaded
		modelsListCategory = origListCategory
		modelsListJSON = origListJSON
		modelsSearchLimit = origSearchLimit
		modelsSearchGGUF = origSearchGGUF
		modelsSearchSort = origSearchSort
		modelsSearchJSON = origSearchJSON
		modelsHFQuant = origHFQuant
		modelsHFDir = origHFDir
		modelsHFOllama = origHFOllama
		modelsHFSelect = origHFSelect
		modelsHFURL = origHFURL
		modelsAutoQuant = origAutoQuant
		modelsAutoDir = origAutoDir
		modelsAutoOllama = origAutoOllama
		modelsAutoSelect = origAutoSelect
		modelsAutoURL = origAutoURL
	})

	ensureOllamaFn = func() {}
	return tmp
}

func TestModelSwitcherValidation(t *testing.T) {
	scanner := models.NewScanner()
	catalog, err := scanner.Scan(context.Background())
	if err != nil {
		t.Fatalf("Failed to scan models catalog: %v", err)
	}

	validModels := make(map[string]bool)
	for _, m := range catalog {
		validModels[m.ID] = true
	}

	testCases := []struct {
		target   string
		expected bool
	}{
		{"gemini-3.1-pro", true},
		{"gemma:2b", true},
		{"gemini-3.6-flash", true},
		{"qwen2.5-coder:7b", true},
		{"deepseek-r1:7b", true},
		{"phi4:14b", true},
		{"invalid-model-name", false},
	}

	for _, tc := range testCases {
		if validModels[tc.target] != tc.expected {
			t.Errorf("Model %s validation expected %v, got %v", tc.target, tc.expected, validModels[tc.target])
		}
	}
}

func TestModelsSearchCmd(t *testing.T) {
	setupCmdModelsTestEnv(t)

	var gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/models" {
			gotQuery = r.URL.RawQuery
			_, _ = w.Write([]byte(`[
				{"id":"Qwen/Qwen2.5-Coder-7B-Instruct-GGUF","downloads":75000,"likes":600,"tags":["gguf"]}
			]`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	hfClientFactory = func() *huggingface.Client { return huggingface.NewClient(srv.URL) }

	var buf bytes.Buffer
	modelsSearchCmd.SetOut(&buf)
	modelsSearchLimit = 5
	modelsSearchGGUF = true
	modelsSearchSort = "likes"
	modelsSearchJSON = true

	if err := modelsSearchCmd.RunE(modelsSearchCmd, []string{"qwen", "coder"}); err != nil {
		t.Fatalf("modelsSearchCmd failed: %v", err)
	}

	if !strings.Contains(gotQuery, "limit=5") || !strings.Contains(gotQuery, "sort=likes") || !strings.Contains(gotQuery, "filter=gguf") {
		t.Fatalf("unexpected query params sent: %q", gotQuery)
	}

	var decoded []huggingface.ModelSummary
	if err := json.Unmarshal(buf.Bytes(), &decoded); err != nil {
		t.Fatalf("failed to decode JSON output: %v\noutput:\n%s", err, buf.String())
	}
	if len(decoded) != 1 || decoded[0].ID != "Qwen/Qwen2.5-Coder-7B-Instruct-GGUF" {
		t.Fatalf("unexpected decoded results: %+v", decoded)
	}

	// Verify human-readable table mode as well
	buf.Reset()
	modelsSearchJSON = false
	if err := modelsSearchCmd.RunE(modelsSearchCmd, []string{"qwen"}); err != nil {
		t.Fatalf("modelsSearchCmd text mode failed: %v", err)
	}
	if !strings.Contains(buf.String(), "Qwen/Qwen2.5-Coder-7B-Instruct-GGUF") {
		t.Fatalf("expected repo ID in text output, got:\n%s", buf.String())
	}
}

func TestModelsHFDownloadCmd(t *testing.T) {
	t.Run("Direct GGUF download with --quant and --select", func(t *testing.T) {
		tmp := setupCmdModelsTestEnv(t)
		const payload = "MOCK_GGUF_Q4_WEIGHTS"

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
		modelsHFDownloadCmd.SetOut(&buf)
		modelsHFQuant = "Q4_K_M"
		modelsHFDir = destDir
		modelsHFOllama = false
		modelsHFSelect = true

		if err := modelsHFDownloadCmd.RunE(modelsHFDownloadCmd, []string{"Qwen/Qwen2.5-Coder-7B-Instruct-GGUF"}); err != nil {
			t.Fatalf("hf-download failed: %v", err)
		}

		gotData, err := os.ReadFile(filepath.Join(destDir, "qwen-q4_k_m.gguf"))
		if err != nil || string(gotData) != payload {
			t.Fatalf("expected downloaded GGUF on disk, err=%v data=%q", err, string(gotData))
		}
		if gotActive := config.GetActiveModel(); gotActive != "qwen-q4_k_m.gguf" {
			t.Fatalf("expected active model qwen-q4_k_m.gguf, got %q", gotActive)
		}
	})

	t.Run("Ollama pull mode with --ollama and --select", func(t *testing.T) {
		setupCmdModelsTestEnv(t)

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

		ollamaClientFactory = ollama.NewClient

		var buf bytes.Buffer
		modelsHFDownloadCmd.SetOut(&buf)
		modelsHFQuant = "Q5_K_M"
		modelsHFOllama = true
		modelsHFSelect = true
		modelsHFURL = ollamaSrv.URL

		if err := modelsHFDownloadCmd.RunE(modelsHFDownloadCmd, []string{"Qwen/Qwen2.5-Coder-7B-Instruct-GGUF"}); err != nil {
			t.Fatalf("hf-download --ollama failed: %v", err)
		}

		wantRef := "hf.co/Qwen/Qwen2.5-Coder-7B-Instruct-GGUF:Q5_K_M"
		if pulledModel != wantRef {
			t.Fatalf("expected Ollama pull for %q, got %q", wantRef, pulledModel)
		}
		if gotActive := config.GetActiveModel(); gotActive != wantRef {
			t.Fatalf("expected active model %q, got %q", wantRef, gotActive)
		}
	})
}

func TestModelsAutoCmd(t *testing.T) {
	tmp := setupCmdModelsTestEnv(t)
	const payload = "AUTO_SELECTED_7B_Q4_GGUF"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/models":
			_, _ = w.Write([]byte(`[
				{"id":"big/Llama-70B-GGUF","downloads":99999},
				{"id":"Qwen/Qwen2.5-Coder-7B-Instruct-GGUF","downloads":50000}
			]`))
		case "/api/models/big/Llama-70B-GGUF":
			_, _ = w.Write([]byte(`{"id":"big/Llama-70B-GGUF","siblings":[
				{"rfilename":"llama-70b-q4_k_m.gguf","size":42000000000}
			]}`))
		case "/api/models/Qwen/Qwen2.5-Coder-7B-Instruct-GGUF":
			_, _ = w.Write([]byte(`{"id":"Qwen/Qwen2.5-Coder-7B-Instruct-GGUF","siblings":[
				{"rfilename":"qwen-7b-q4_k_m.gguf","size":4200000000},
				{"rfilename":"qwen-7b-q8_0.gguf","size":8000000000}
			]}`))
		case "/Qwen/Qwen2.5-Coder-7B-Instruct-GGUF/resolve/main/qwen-7b-q4_k_m.gguf":
			_, _ = w.Write([]byte(payload))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	hfClientFactory = func() *huggingface.Client { return huggingface.NewClient(srv.URL) }
	// 8 GB RAM skips the 70B model and chooses Q4_K_M on the 7B model
	availableRAMGBFn = func(ctx context.Context) float64 { return 8.0 }

	var buf bytes.Buffer
	modelsAutoCmd.SetOut(&buf)
	modelsAutoQuant = ""
	modelsAutoDir = filepath.Join(tmp, "models")
	modelsAutoOllama = false
	modelsAutoSelect = true

	if err := modelsAutoCmd.RunE(modelsAutoCmd, []string{"qwen", "coder"}); err != nil {
		t.Fatalf("modelsAutoCmd failed: %v", err)
	}

	gotBytes, err := os.ReadFile(filepath.Join(tmp, "models", "qwen-7b-q4_k_m.gguf"))
	if err != nil || string(gotBytes) != payload {
		t.Fatalf("expected auto-downloaded GGUF, err=%v data=%q", err, string(gotBytes))
	}
	if got := config.GetActiveModel(); got != "qwen-7b-q4_k_m.gguf" {
		t.Fatalf("expected active model qwen-7b-q4_k_m.gguf, got %q", got)
	}
}

func TestModelsListAndSelectCmd(t *testing.T) {
	tmp := setupCmdModelsTestEnv(t)
	modelsDir := filepath.Join(tmp, "models")
	if err := os.MkdirAll(modelsDir, 0755); err != nil {
		t.Fatalf("mkdir models: %v", err)
	}
	ggufFile := filepath.Join(modelsDir, "local-devstral-Q4_K_M.gguf")
	if err := os.WriteFile(ggufFile, []byte("local-gguf-content"), 0644); err != nil {
		t.Fatalf("write gguf: %v", err)
	}

	ollamaSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/tags" {
			_, _ = w.Write([]byte(`{"models":[{"name":"gemma4:e2b"},{"name":"custom-local:latest"}]}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer ollamaSrv.Close()

	baseURL = ollamaSrv.URL
	ollamaClientFactory = ollama.NewClient
	modelsScannerFactory = func() *models.Scanner {
		s := models.NewScannerWithClient(ollama.NewClient(ollamaSrv.URL))
		s.SetLocalModelsDir(modelsDir)
		return s
	}

	// 1. list --downloaded --json
	var buf bytes.Buffer
	modelsListCmd.SetOut(&buf)
	modelsListLocal = true
	modelsListDownloaded = true
	modelsListCategory = ""
	modelsListJSON = true

	if err := modelsListCmd.RunE(modelsListCmd, nil); err != nil {
		t.Fatalf("modelsListCmd failed: %v", err)
	}

	var listed []models.ModelInfo
	if err := json.Unmarshal(buf.Bytes(), &listed); err != nil {
		t.Fatalf("unmarshal models list JSON failed: %v\n%s", err, buf.String())
	}
	foundCustom := false
	foundGGUF := false
	for _, m := range listed {
		if m.ID == "custom-local:latest" {
			foundCustom = true
		}
		if m.ID == "local-devstral-Q4_K_M.gguf" {
			foundGGUF = true
		}
	}
	if !foundCustom || !foundGGUF {
		t.Fatalf("expected custom Ollama and local GGUF in --downloaded --json list, got %+v", listed)
	}

	// 2. select custom hf.co/... ID
	buf.Reset()
	modelsSelectCmd.SetOut(&buf)
	if err := modelsSelectCmd.RunE(modelsSelectCmd, []string{"hf.co/Qwen/Qwen2.5-Coder-7B-Instruct-GGUF:Q4_K_M"}); err != nil {
		t.Fatalf("select hf.co ref failed: %v", err)
	}
	if got := config.GetActiveModel(); got != "hf.co/Qwen/Qwen2.5-Coder-7B-Instruct-GGUF:Q4_K_M" {
		t.Fatalf("expected hf.co active model, got %q", got)
	}

	// 3. select custom Ollama model
	buf.Reset()
	if err := modelsSelectCmd.RunE(modelsSelectCmd, []string{"custom-local:latest"}); err != nil {
		t.Fatalf("select custom Ollama model failed: %v", err)
	}
	if got := config.GetActiveModel(); got != "custom-local:latest" {
		t.Fatalf("expected custom-local:latest active model, got %q", got)
	}

	// 4. select downloaded .gguf by path
	buf.Reset()
	if err := modelsSelectCmd.RunE(modelsSelectCmd, []string{ggufFile}); err != nil {
		t.Fatalf("select local gguf path failed: %v", err)
	}
	if got := config.GetActiveModel(); got != "local-devstral-Q4_K_M.gguf" {
		t.Fatalf("expected local-devstral-Q4_K_M.gguf active model, got %q", got)
	}

	// 5. reject unknown ID
	if err := modelsSelectCmd.RunE(modelsSelectCmd, []string{"nonexistent-xyz-model"}); err == nil {
		t.Fatalf("expected error for unknown model ID")
	}
}

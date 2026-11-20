package huggingface

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestSearchModels_ParamsAndAuth(t *testing.T) {
	t.Run("GGUF search with default public request sends no auth header", func(t *testing.T) {
		t.Setenv("HF_TOKEN", "")
		t.Setenv("HUGGING_FACE_HUB_TOKEN", "")

		var gotQuery string
		var gotAuth string
		var gotPath string

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotPath = r.URL.Path
			gotQuery = r.URL.RawQuery
			gotAuth = r.Header.Get("Authorization")
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[
				{"id":"Qwen/Qwen2.5-Coder-7B-Instruct-GGUF","downloads":50000,"likes":420,"tags":["gguf"],"gated":false},
				{"modelId":"bartowski/Llama-3.2-3B-Instruct-GGUF","downloads":25000,"likes":180,"tags":["gguf"],"gated":"auto"}
			]`))
		}))
		defer srv.Close()

		client := NewClient(srv.URL)
		results, err := client.SearchModels(context.Background(), SearchOptions{
			Query:    "qwen coder",
			GGUFOnly: true,
			Sort:     "downloads",
			Limit:    5,
		})
		if err != nil {
			t.Fatalf("SearchModels failed: %v", err)
		}
		if gotPath != "/api/models" {
			t.Fatalf("expected path /api/models, got %q", gotPath)
		}
		if gotAuth != "" {
			t.Fatalf("expected empty Authorization header for public search, got %q", gotAuth)
		}
		if !strings.Contains(gotQuery, "filter=gguf") || !strings.Contains(gotQuery, "sort=downloads") || !strings.Contains(gotQuery, "limit=5") || !strings.Contains(gotQuery, "search=qwen+coder") {
			t.Fatalf("unexpected query params: %q", gotQuery)
		}
		if len(results) != 2 {
			t.Fatalf("expected 2 results, got %d", len(results))
		}
		if results[0].ID != "Qwen/Qwen2.5-Coder-7B-Instruct-GGUF" || results[0].Author != "Qwen" {
			t.Fatalf("unexpected first result: %+v", results[0])
		}
		if results[1].ID != "bartowski/Llama-3.2-3B-Instruct-GGUF" || results[1].Author != "bartowski" {
			t.Fatalf("expected modelId fallback for second result, got %+v", results[1])
		}
	})

	t.Run("Non-GGUF search omits gguf filter and sends HF_TOKEN bearer", func(t *testing.T) {
		t.Setenv("HF_TOKEN", "hf_secret_test_token")
		t.Setenv("HUGGING_FACE_HUB_TOKEN", "")

		var gotQuery string
		var gotAuth string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotQuery = r.URL.RawQuery
			gotAuth = r.Header.Get("Authorization")
			_, _ = w.Write([]byte(`[{"id":"meta-llama/Llama-3.3-70B-Instruct","downloads":1000,"likes":99}]`))
		}))
		defer srv.Close()

		client := NewClient(srv.URL)
		results, err := client.SearchModels(context.Background(), SearchOptions{
			Query:    "llama",
			GGUFOnly: false,
			Sort:     "likes",
			Limit:    15,
		})
		if err != nil {
			t.Fatalf("SearchModels failed: %v", err)
		}
		if gotAuth != "Bearer hf_secret_test_token" {
			t.Fatalf("expected Bearer token from HF_TOKEN, got %q", gotAuth)
		}
		if strings.Contains(gotQuery, "filter=gguf") {
			t.Fatalf("did not expect filter=gguf when GGUFOnly=false, got %q", gotQuery)
		}
		if !strings.Contains(gotQuery, "sort=likes") || !strings.Contains(gotQuery, "limit=15") {
			t.Fatalf("unexpected query params: %q", gotQuery)
		}
		if len(results) != 1 || results[0].Likes != 99 {
			t.Fatalf("unexpected results: %+v", results)
		}
	})

	t.Run("HUGGING_FACE_HUB_TOKEN fallback and HTTP error mapping", func(t *testing.T) {
		t.Setenv("HF_TOKEN", "")
		t.Setenv("HUGGING_FACE_HUB_TOKEN", "hub_token_123")

		var gotAuth string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotAuth = r.Header.Get("Authorization")
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":"Invalid credentials"}`))
		}))
		defer srv.Close()

		client := NewClient(srv.URL)
		_, err := client.SearchModels(context.Background(), SearchOptions{Query: "private"})
		if !errors.Is(err, ErrUnauthorized) {
			t.Fatalf("expected ErrUnauthorized, got: %v", err)
		}
		if gotAuth != "Bearer hub_token_123" {
			t.Fatalf("expected HUGGING_FACE_HUB_TOKEN bearer, got %q", gotAuth)
		}
	})
}

func TestListGGUFFiles(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/models/Qwen/Qwen2.5-Coder-7B-Instruct-GGUF" {
			t.Errorf("unexpected path: %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if r.URL.Query().Get("blobs") != "true" {
			t.Errorf("expected blobs=true query parameter, got %q", r.URL.RawQuery)
		}
		_, _ = w.Write([]byte(`{
			"id": "Qwen/Qwen2.5-Coder-7B-Instruct-GGUF",
			"siblings": [
				{"rfilename": "README.md", "size": 2048},
				{"rfilename": "config.json", "size": 512},
				{"rfilename": "qwen2.5-coder-7b-instruct-q4_k_m.gguf", "size": 4683072000},
				{"rfilename": "qwen2.5-coder-7b-instruct-q5_k_m.gguf", "lfs": {"size": 5444000000}},
				{"rfilename": "qwen2.5-coder-7b-instruct-q8_0.gguf", "size": 8096000000},
				{"rfilename": "qwen2.5-coder-7b-instruct-iq4_xs.gguf", "size": 4200000000},
				{"rfilename": "qwen2.5-coder-7b-instruct-fp16.gguf", "size": 15230000000},
				{"rfilename": "mmproj-model-f16.gguf", "size": 600000000}
			]
		}`))
	}))
	defer srv.Close()

	client := NewClient(srv.URL)
	files, err := client.ListGGUFFiles(context.Background(), "Qwen/Qwen2.5-Coder-7B-Instruct-GGUF")
	if err != nil {
		t.Fatalf("ListGGUFFiles failed: %v", err)
	}
	if len(files) != 6 {
		t.Fatalf("expected 6 gguf files (filtering non-gguf), got %d: %+v", len(files), files)
	}

	wantQuants := map[string]string{
		"qwen2.5-coder-7b-instruct-q4_k_m.gguf": "Q4_K_M",
		"qwen2.5-coder-7b-instruct-q5_k_m.gguf": "Q5_K_M",
		"qwen2.5-coder-7b-instruct-q8_0.gguf":   "Q8_0",
		"qwen2.5-coder-7b-instruct-iq4_xs.gguf": "IQ4_XS",
		"qwen2.5-coder-7b-instruct-fp16.gguf":   "F16",
		"mmproj-model-f16.gguf":                 "F16",
	}

	for _, f := range files {
		if wantQ, ok := wantQuants[f.Filename]; !ok || f.Quantization != wantQ {
			t.Errorf("file %s quant = %q, want %q", f.Filename, f.Quantization, wantQ)
		}
		if f.SizeBytes <= 0 {
			t.Errorf("file %s expected positive SizeBytes (including lfs.size fallback), got %d", f.Filename, f.SizeBytes)
		}
		if f.EstimatedRAMGB <= 0 {
			t.Errorf("file %s expected positive EstimatedRAMGB, got %v", f.Filename, f.EstimatedRAMGB)
		}
		if f.Filename == "mmproj-model-f16.gguf" && !f.IsMMProj {
			t.Errorf("expected IsMMProj=true for %s", f.Filename)
		}
	}
}

func TestListGGUFFiles_InvalidRepoIDAndErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "missing") {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error":"Repository not found"}`))
			return
		}
		_, _ = w.Write([]byte(`{"id":"owner/no-gguf","siblings":[{"rfilename":"model.safetensors","size":100}]}`))
	}))
	defer srv.Close()

	client := NewClient(srv.URL)

	badRepos := []string{"", "../etc/passwd", "owner/../evil", "a/b/c", `owner\repo`, "owner/repo?foo=bar", "owner/repo:Q4_K_M"}
	for _, bad := range badRepos {
		if _, err := client.ListGGUFFiles(context.Background(), bad); !errors.Is(err, ErrInvalidRepoID) {
			t.Errorf("expected ErrInvalidRepoID for %q, got %v", bad, err)
		}
	}

	if _, err := client.ListGGUFFiles(context.Background(), "owner/missing"); !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}

	if _, err := client.ListGGUFFiles(context.Background(), "owner/no-gguf"); !errors.Is(err, ErrNoGGUFFiles) {
		t.Errorf("expected ErrNoGGUFFiles, got %v", err)
	}
}

func TestSelectBestGGUF(t *testing.T) {
	const giB = int64(1024 * 1024 * 1024)
	files := []GGUFFile{
		{Filename: "mmproj-model-f16.gguf", SizeBytes: 1 * giB, Quantization: "F16", IsMMProj: true},
		{Filename: "model-Q2_K.gguf", SizeBytes: 2 * giB},
		{Filename: "model-IQ4_XS.gguf", SizeBytes: 4 * giB},
		{Filename: "model-Q4_K_M.gguf", SizeBytes: 4500 * 1024 * 1024},
		{Filename: "model-Q6_K.gguf", SizeBytes: 6 * giB},
		{Filename: "model-Q8_0.gguf", SizeBytes: 8 * giB},
		{Filename: "model-F16.gguf", SizeBytes: 14 * giB},
	}

	// 1. 8 GB available RAM -> Q4_K_M (~5.77 GB est) fits, Q6_K (~7.7 GB est) fits! Wait: 6*1.2+0.5 = 7.7 GB <= 8.0 GB
	// Let's test 6.5 GB available RAM -> Q4_K_M (~5.77 GB) fits, Q6_K (7.7 GB) does not.
	got65, err := SelectBestGGUF(files, 6.5, "")
	if err != nil {
		t.Fatalf("SelectBestGGUF(6.5GB) failed: %v", err)
	}
	if got65.Filename != "model-Q4_K_M.gguf" {
		t.Fatalf("expected Q4_K_M for 6.5GB RAM, got %s", got65.Filename)
	}

	// 2. 12 GB available RAM -> Q8_0 (~10.1 GB) fits, F16 (~17.3 GB) does not.
	got12, err := SelectBestGGUF(files, 12.0, "")
	if err != nil {
		t.Fatalf("SelectBestGGUF(12GB) failed: %v", err)
	}
	if got12.Filename != "model-Q8_0.gguf" {
		t.Fatalf("expected Q8_0 for 12GB RAM, got %s", got12.Filename)
	}

	// 3. Preferred quant overrides auto selection (and skips mmproj F16).
	gotPref, err := SelectBestGGUF(files, 12.0, "iq4_xs")
	if err != nil {
		t.Fatalf("SelectBestGGUF(preferred IQ4_XS) failed: %v", err)
	}
	if gotPref.Filename != "model-IQ4_XS.gguf" {
		t.Fatalf("expected model-IQ4_XS.gguf, got %s", gotPref.Filename)
	}

	// 4. Preferred quant F16 matches model-F16.gguf (not mmproj).
	gotF16, err := SelectBestGGUF(files, 32.0, "f16")
	if err != nil {
		t.Fatalf("SelectBestGGUF(preferred F16) failed: %v", err)
	}
	if gotF16.Filename != "model-F16.gguf" {
		t.Fatalf("expected model-F16.gguf, got %s", gotF16.Filename)
	}

	// 5. Unavailable preferred quant returns ErrQuantNotFound.
	if _, err := SelectBestGGUF(files, 32.0, "Q5_K_M"); !errors.Is(err, ErrQuantNotFound) {
		t.Fatalf("expected ErrQuantNotFound, got %v", err)
	}

	// 6. Insufficient RAM returns ErrInsufficientRAM.
	if _, err := SelectBestGGUF(files, 1.5, ""); !errors.Is(err, ErrInsufficientRAM) {
		t.Fatalf("expected ErrInsufficientRAM, got %v", err)
	}

	// 7. Empty files returns ErrNoGGUFFiles.
	if _, err := SelectBestGGUF(nil, 16.0, ""); !errors.Is(err, ErrNoGGUFFiles) {
		t.Fatalf("expected ErrNoGGUFFiles, got %v", err)
	}
}

func TestDownloadGGUF_ResumeProgressAndSecurity(t *testing.T) {
	payload := bytes.Repeat([]byte("GGUF_MODEL_BINARY_CHUNK_0123456789ABCDEF"), 4096) // ~160 KB

	t.Run("Resumable Range download merges .part and renames atomically", func(t *testing.T) {
		destDir := t.TempDir()
		filename := "model-q4_k_m.gguf"
		partPath := filepath.Join(destDir, filename+".part")

		initialBytes := 50000
		if err := os.WriteFile(partPath, payload[:initialBytes], 0644); err != nil {
			t.Fatalf("seed .part file: %v", err)
		}

		var gotRange string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/Qwen/TestRepo/resolve/main/model-q4_k_m.gguf" {
				t.Errorf("unexpected download path: %s", r.URL.Path)
				w.WriteHeader(http.StatusNotFound)
				return
			}
			gotRange = r.Header.Get("Range")
			wantRange := fmt.Sprintf("bytes=%d-", initialBytes)
			if gotRange != wantRange {
				t.Errorf("expected Range %q, got %q", wantRange, gotRange)
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			remaining := payload[initialBytes:]
			w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", initialBytes, len(payload)-1, len(payload)))
			w.Header().Set("Content-Length", fmt.Sprintf("%d", len(remaining)))
			w.WriteHeader(http.StatusPartialContent)
			_, _ = w.Write(remaining)
		}))
		defer srv.Close()

		var lastCompleted, lastTotal int64
		var updates int
		client := NewClient(srv.URL)
		finalPath, err := client.DownloadGGUF(context.Background(), "Qwen/TestRepo", filename, destDir, func(completed, total int64) {
			updates++
			lastCompleted = completed
			lastTotal = total
		})
		if err != nil {
			t.Fatalf("DownloadGGUF failed: %v", err)
		}

		expectedPath := filepath.Join(destDir, filename)
		if finalPath != expectedPath {
			t.Fatalf("expected path %q, got %q", expectedPath, finalPath)
		}
		if _, err := os.Stat(partPath); !os.IsNotExist(err) {
			t.Fatalf("expected .part file to be removed/renamed after completion")
		}
		gotData, err := os.ReadFile(finalPath)
		if err != nil {
			t.Fatalf("read final file: %v", err)
		}
		if !bytes.Equal(gotData, payload) {
			t.Fatalf("downloaded payload mismatch: got %d bytes, want %d", len(gotData), len(payload))
		}
		if updates == 0 || lastCompleted != int64(len(payload)) || lastTotal != int64(len(payload)) {
			t.Fatalf("unexpected progress updates=%d completed=%d total=%d (want %d)", updates, lastCompleted, lastTotal, len(payload))
		}
	})

	t.Run("416 when .part is already complete finalizes atomically", func(t *testing.T) {
		destDir := t.TempDir()
		filename := "complete.gguf"
		partPath := filepath.Join(destDir, filename+".part")
		if err := os.WriteFile(partPath, payload, 0644); err != nil {
			t.Fatalf("seed .part file: %v", err)
		}

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Range", fmt.Sprintf("bytes */%d", len(payload)))
			w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
		}))
		defer srv.Close()

		client := NewClient(srv.URL)
		finalPath, err := client.DownloadGGUF(context.Background(), "Qwen/TestRepo", filename, destDir, nil)
		if err != nil {
			t.Fatalf("DownloadGGUF 416 finalize failed: %v", err)
		}
		gotData, err := os.ReadFile(finalPath)
		if err != nil || !bytes.Equal(gotData, payload) {
			t.Fatalf("final file verification failed: err=%v len=%d", err, len(gotData))
		}
	})

	t.Run("Truncated download returns ErrUnexpectedEOF and preserves .part", func(t *testing.T) {
		destDir := t.TempDir()
		filename := "truncated.gguf"

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Length", fmt.Sprintf("%d", len(payload)))
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(payload[:1024]) // Server closes early
		}))
		defer srv.Close()

		client := NewClient(srv.URL)
		_, err := client.DownloadGGUF(context.Background(), "Qwen/TestRepo", filename, destDir, nil)
		if !errors.Is(err, io.ErrUnexpectedEOF) {
			t.Fatalf("expected io.ErrUnexpectedEOF, got: %v", err)
		}
		if _, err := os.Stat(filepath.Join(destDir, filename)); !os.IsNotExist(err) {
			t.Fatalf("final file must not exist after truncated download")
		}
		info, err := os.Stat(filepath.Join(destDir, filename+".part"))
		if err != nil || info.Size() != 1024 {
			t.Fatalf("expected 1024-byte .part file preserved for resume, got err=%v info=%v", err, info)
		}
	})

	t.Run("Path traversal and unsafe filenames are rejected without HTTP dial", func(t *testing.T) {
		destDir := t.TempDir()
		var dialed int32
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			atomic.AddInt32(&dialed, 1)
			w.WriteHeader(http.StatusOK)
		}))
		defer srv.Close()

		client := NewClient(srv.URL)
		badNames := []string{
			"",
			"../escape.gguf",
			"../../etc/passwd",
			"sub/../../escape.gguf",
			`..\escape.gguf`,
			"/abs/path.gguf",
			`C:\abs\path.gguf`,
			"C:drive.gguf",
			"model.gguf?token=1",
			"model.gguf#frag",
			"./dot.gguf",
		}
		for _, bad := range badNames {
			_, err := client.DownloadGGUF(context.Background(), "Qwen/TestRepo", bad, destDir, nil)
			if !errors.Is(err, ErrInvalidFilename) {
				t.Errorf("expected ErrInvalidFilename for %q, got %v", bad, err)
			}
		}
		if atomic.LoadInt32(&dialed) != 0 {
			t.Fatalf("expected 0 HTTP dials for invalid filenames, got %d", dialed)
		}
	})
}

func TestDownloadGGUF_ContextCancellation(t *testing.T) {
	destDir := t.TempDir()
	started := make(chan struct{})

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "10485760")
		w.WriteHeader(http.StatusOK)
		flusher, _ := w.(http.Flusher)
		_, _ = w.Write(bytes.Repeat([]byte("A"), 4096))
		if flusher != nil {
			flusher.Flush()
		}
		select {
		case <-started:
		default:
			close(started)
		}
		<-r.Context().Done()
	}))
	defer srv.Close()

	client := NewClient(srv.URL)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		<-started
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()

	_, err := client.DownloadGGUF(ctx, "Qwen/TestRepo", "slow.gguf", destDir, nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got: %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(destDir, "slow.gguf")); !os.IsNotExist(statErr) {
		t.Fatalf("cancelled download must not produce final file")
	}
}

func TestOllamaRefAndParseRef(t *testing.T) {
	if got := OllamaRef("Qwen/Qwen2.5-Coder-7B-Instruct-GGUF", "Q4_K_M"); got != "hf.co/Qwen/Qwen2.5-Coder-7B-Instruct-GGUF:Q4_K_M" {
		t.Fatalf("unexpected OllamaRef with quant: %q", got)
	}
	if got := OllamaRef("Qwen/Qwen2.5-Coder-7B-Instruct-GGUF", ""); got != "hf.co/Qwen/Qwen2.5-Coder-7B-Instruct-GGUF" {
		t.Fatalf("unexpected OllamaRef without quant: %q", got)
	}
	if got := OllamaRef("hf://bartowski/Llama-3.2-3B-Instruct-GGUF:Q6_K", ""); got != "hf.co/bartowski/Llama-3.2-3B-Instruct-GGUF:Q6_K" {
		t.Fatalf("unexpected OllamaRef from hf://: %q", got)
	}

	repo, quant, isHF := ParseRef("hf://Qwen/Qwen2.5-7B-GGUF:Q5_K_M")
	if !isHF || repo != "Qwen/Qwen2.5-7B-GGUF" || quant != "Q5_K_M" {
		t.Fatalf("ParseRef mismatch: repo=%q quant=%q isHF=%v", repo, quant, isHF)
	}
	repo2, quant2, isHF2 := ParseRef("hf.co/unsloth/Phi-4-GGUF")
	if !isHF2 || repo2 != "unsloth/Phi-4-GGUF" || quant2 != "" {
		t.Fatalf("ParseRef mismatch: repo=%q quant=%q isHF=%v", repo2, quant2, isHF2)
	}
	_, _, isHF3 := ParseRef("gemma4:e2b")
	if isHF3 {
		t.Fatalf("expected gemma4:e2b isHF=false")
	}
}

func TestClient_ConcurrentStress_Race(t *testing.T) {
	payload := bytes.Repeat([]byte("RACE_SAFE_GGUF_PAYLOAD"), 512)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/models":
			_, _ = w.Write([]byte(`[{"id":"Qwen/Qwen2.5-Coder-7B-Instruct-GGUF","downloads":1000}]`))
		case r.URL.Path == "/api/models/Qwen/Qwen2.5-Coder-7B-Instruct-GGUF":
			_, _ = w.Write([]byte(`{"id":"Qwen/Qwen2.5-Coder-7B-Instruct-GGUF","siblings":[{"rfilename":"model-Q4_K_M.gguf","size":1024}]}`))
		case strings.HasSuffix(r.URL.Path, "/resolve/main/model-Q4_K_M.gguf"):
			w.Header().Set("Content-Length", fmt.Sprintf("%d", len(payload)))
			_, _ = w.Write(payload)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	client := NewClient(srv.URL)
	baseDir := t.TempDir()

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			ctx := context.Background()
			res, err := client.SearchModels(ctx, SearchOptions{Query: "qwen", GGUFOnly: true})
			if err != nil || len(res) != 1 {
				t.Errorf("worker %d search failed: len=%d err=%v", idx, len(res), err)
				return
			}
			files, err := client.ListGGUFFiles(ctx, res[0].ID)
			if err != nil || len(files) != 1 {
				t.Errorf("worker %d list failed: len=%d err=%v", idx, len(files), err)
				return
			}
			subDir := filepath.Join(baseDir, fmt.Sprintf("w-%d", idx))
			outPath, err := client.DownloadGGUF(ctx, res[0].ID, files[0].Filename, subDir, nil)
			if err != nil {
				t.Errorf("worker %d download failed: %v", idx, err)
				return
			}
			data, err := os.ReadFile(outPath)
			if err != nil || !bytes.Equal(data, payload) {
				t.Errorf("worker %d payload mismatch: err=%v len=%d", idx, err, len(data))
			}
		}(i)
	}
	wg.Wait()
}

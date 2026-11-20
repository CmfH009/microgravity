package models

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/CmfH009/microgravity/internal/huggingface"
	"github.com/CmfH009/microgravity/internal/ollama"
	"github.com/charmbracelet/lipgloss"
	"github.com/shirou/gopsutil/v3/mem"
)

const customModelsCategory = "Custom & Downloaded Local Models"

type ModelInfo struct {
	ID            string  `json:"id"`
	Name          string  `json:"name"`
	Provider      string  `json:"provider"` // "ollama", "gguf", "cloud", or "huggingface"
	Tier          string  `json:"tier"`     // "PRO", "FLASH", "FLASH_LITE", "LOCAL", "HF"
	Category      string  `json:"category"` // Category grouping header
	Description   string  `json:"description"`
	Offline       bool    `json:"offline"`
	Status        string  `json:"status"` // "Downloaded", "Available Offline", "Available Cloud"
	MinRAMGB      float64 `json:"min_ram_gb"`
	IsActive      bool    `json:"is_active"`
	ContextWindow int     `json:"context_window"`
	SizeBytes     int64   `json:"size_bytes,omitempty"`
	Quantization  string  `json:"quantization,omitempty"`
	LocalPath     string  `json:"local_path,omitempty"`
	Downloads     int64   `json:"downloads,omitempty"`
	Likes         int64   `json:"likes,omitempty"`
}

type Scanner struct {
	ollamaClient   *ollama.Client
	LocalModelsDir string
}

// DefaultLocalModelsDir returns the default directory for downloaded GGUF model files.
func DefaultLocalModelsDir() string {
	if envDir := strings.TrimSpace(os.Getenv("MG_MODELS_DIR")); envDir != "" {
		return envDir
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	return filepath.Join(home, ".microgravity", "models")
}

func NewScanner() *Scanner {
	return NewScannerWithClient(ollama.NewClient("http://localhost:11434"))
}

// NewScannerWithClient creates a Scanner backed by a custom Ollama client.
func NewScannerWithClient(client *ollama.Client) *Scanner {
	if client == nil {
		client = ollama.NewClient("http://localhost:11434")
	}
	return &Scanner{
		ollamaClient:   client,
		LocalModelsDir: DefaultLocalModelsDir(),
	}
}

// SetLocalModelsDir overrides the directory scanned for downloaded .gguf files.
func (s *Scanner) SetLocalModelsDir(dir string) {
	if s == nil {
		return
	}
	s.LocalModelsDir = dir
}

func (s *Scanner) resolveLocalModelsDir() string {
	if s != nil && strings.TrimSpace(s.LocalModelsDir) != "" {
		return s.LocalModelsDir
	}
	return DefaultLocalModelsDir()
}

func (s *Scanner) Scan(ctx context.Context) ([]ModelInfo, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	v, err := mem.VirtualMemoryWithContext(ctx)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("read virtual memory: %w", err)
	}
	if v == nil {
		return nil, fmt.Errorf("read virtual memory: empty stats")
	}
	totalRAMGB := float64(v.Total) / (1024 * 1024 * 1024)

	knownModels := []ModelInfo{
		// Gemini 3 Series Flagship Models (AGY Tier Parity)
		{
			ID:            "gemini-3.8-flash",
			Name:          "Gemini 3.8 Flash Cloud",
			Provider:      "cloud",
			Tier:          "FLASH",
			Category:      "Gemini 3 Series (Flagship)",
			Description:   "Cutting-edge Gemini 3.8 Flash model with high reasoning effort and rapid agent tool loop execution.",
			Offline:       false,
			MinRAMGB:      0.0,
			ContextWindow: 1048576,
		},
		{
			ID:            "gemini-3.7-flash",
			Name:          "Gemini 3.7 Flash Cloud",
			Provider:      "cloud",
			Tier:          "FLASH",
			Category:      "Gemini 3 Series (Flagship)",
			Description:   "High-throughput Gemini 3.7 Flash model with hybrid thinking budget controls.",
			Offline:       false,
			MinRAMGB:      0.0,
			ContextWindow: 1048576,
		},
		{
			ID:            "gemini-3.1-pro",
			Name:          "Gemini 3.1 Pro Cloud",
			Provider:      "cloud",
			Tier:          "PRO",
			Category:      "Gemini 3 Series (Flagship)",
			Description:   "Flagship Gemini 3 series reasoning & architecture model. High-capacity reasoning.",
			Offline:       false,
			MinRAMGB:      0.0,
			ContextWindow: 2097152,
		},
		{
			ID:            "gemini-3.6-flash",
			Name:          "Gemini 3.6 Flash Cloud",
			Provider:      "cloud",
			Tier:          "FLASH",
			Category:      "Gemini 3 Series (Flagship)",
			Description:   "High-speed Gemini 3 series Flash model for rapid code generation & multi-modal tasks.",
			Offline:       false,
			MinRAMGB:      0.0,
			ContextWindow: 1048576,
		},
		{
			ID:            "gemini-3.5-flash-lite",
			Name:          "Gemini 3.5 Flash Lite Cloud",
			Provider:      "cloud",
			Tier:          "FLASH_LITE",
			Category:      "Gemini 3 Series (Flagship)",
			Description:   "Ultra-lightweight low-latency Flash Lite model for instant lookups & subagent queries.",
			Offline:       false,
			MinRAMGB:      0.0,
			ContextWindow: 1048576,
		},

		// Gemini 2.5 & 2.0 Series Models
		{
			ID:            "gemini-2.5-pro",
			Name:          "Gemini 2.5 Pro Cloud",
			Provider:      "cloud",
			Tier:          "PRO",
			Category:      "Gemini 2.5 & 2.0 Series",
			Description:   "Next-gen flagship Gemini 2.5 model with deep reasoning and 2M token context.",
			Offline:       false,
			MinRAMGB:      0.0,
			ContextWindow: 2097152,
		},
		{
			ID:            "gemini-2.5-flash",
			Name:          "Gemini 2.5 Flash Cloud",
			Provider:      "cloud",
			Tier:          "FLASH",
			Category:      "Gemini 2.5 & 2.0 Series",
			Description:   "High-speed Gemini 2.5 Flash model for agent loops, tool orchestration & multi-modal tasks.",
			Offline:       false,
			MinRAMGB:      0.0,
			ContextWindow: 1048576,
		},
		{
			ID:            "gemini-2.0-flash",
			Name:          "Gemini 2.0 Flash Cloud",
			Provider:      "cloud",
			Tier:          "FLASH",
			Category:      "Gemini 2.5 & 2.0 Series",
			Description:   "Production Gemini 2.0 Flash model.",
			Offline:       false,
			MinRAMGB:      0.0,
			ContextWindow: 1048576,
		},
		{
			ID:            "gemini-2.0-pro-exp",
			Name:          "Gemini 2.0 Pro Cloud",
			Provider:      "cloud",
			Tier:          "PRO",
			Category:      "Gemini 2.5 & 2.0 Series",
			Description:   "Gemini 2.0 Pro experimental reasoning model.",
			Offline:       false,
			MinRAMGB:      0.0,
			ContextWindow: 2097152,
		},
		{
			ID:            "gemini-2.0-flash-exp",
			Name:          "Gemini 2.0 Flash Cloud",
			Provider:      "cloud",
			Tier:          "FLASH",
			Category:      "Gemini 2.5 & 2.0 Series",
			Description:   "Gemini 2.0 Flash experimental model.",
			Offline:       false,
			MinRAMGB:      0.0,
			ContextWindow: 1048576,
		},
		{
			ID:            "gemini-1.5-pro",
			Name:          "Gemini 1.5 Pro Cloud (2M Context)",
			Provider:      "cloud",
			Tier:          "PRO",
			Category:      "Gemini 2.0 & 1.5 Series",
			Description:   "Massive 2 Million token context for full codebase architecture review.",
			Offline:       false,
			MinRAMGB:      0.0,
			ContextWindow: 2097152,
		},
		{
			ID:            "gemini-1.5-flash",
			Name:          "Gemini 1.5 Flash Cloud",
			Provider:      "cloud",
			Tier:          "FLASH",
			Category:      "Gemini 2.0 & 1.5 Series",
			Description:   "Low-latency cloud model ideal for multi-turn assistant chat.",
			Offline:       false,
			MinRAMGB:      0.0,
			ContextWindow: 1048576,
		},

		// Gemma Open Models (Cloud & Local)
		{
			ID:            "gemma-2-27b-it",
			Name:          "Gemma 2 27B Cloud",
			Provider:      "cloud",
			Tier:          "PRO",
			Category:      "Gemma Open Models",
			Description:   "Google's flagship 27B Gemma model running on Cloud TPUs.",
			Offline:       false,
			MinRAMGB:      0.0,
			ContextWindow: 8192,
		},
		{
			ID:            "gemma-2-9b-it",
			Name:          "Gemma 2 9B Cloud",
			Provider:      "cloud",
			Tier:          "FLASH",
			Category:      "Gemma Open Models",
			Description:   "High-performance instruction-tuned Cloud Gemma model.",
			Offline:       false,
			MinRAMGB:      0.0,
			ContextWindow: 8192,
		},
		{
			ID:            "gemma:2b",
			Name:          "Gemma 2.6B Local",
			Provider:      "ollama",
			Tier:          "LOCAL",
			Category:      "Gemma Open Models",
			Description:   "Ultra-fast local model for quick edits & lightweight CPU/GPU offline execution.",
			Offline:       true,
			MinRAMGB:      4.0,
			ContextWindow: 8192,
		},
		{
			ID:            "gemma:7b",
			Name:          "Gemma 7B Local",
			Provider:      "ollama",
			Tier:          "LOCAL",
			Category:      "Gemma Open Models",
			Description:   "Balanced local model for code generation, logic reasoning, and analysis.",
			Offline:       true,
			MinRAMGB:      8.0,
			ContextWindow: 8192,
		},
		{
			ID:            "gemma4:e4b",
			Name:          "Gemma 4 e4B Local",
			Provider:      "ollama",
			Tier:          "LOCAL",
			Category:      "Gemma 4 Series",
			Description:   "High-efficiency 4B Gemma 4 local runtime with rapid inference speed.",
			Offline:       true,
			MinRAMGB:      6.0,
			ContextWindow: 8192,
		},
		{
			ID:            "gemma4:e2b",
			Name:          "Gemma 4 e2B Local",
			Provider:      "ollama",
			Tier:          "LOCAL",
			Category:      "Gemma 4 Series",
			Description:   "Ultra-compact 2B Gemma 4 local model for background classification and routing.",
			Offline:       true,
			MinRAMGB:      4.0,
			ContextWindow: 8192,
		},
		{
			ID:            "gemma4:12b",
			Name:          "Gemma 4 12B Local",
			Provider:      "ollama",
			Tier:          "LOCAL",
			Category:      "Gemma 4 Series",
			Description:   "Heavy local model for advanced offline reasoning (requires ~10GB RAM).",
			Offline:       true,
			MinRAMGB:      12.0,
			ContextWindow: 8192,
		},

		// Orbit Deterministic Gemma Personas
		{
			ID:            "orbit-coder:latest",
			Name:          "Orbit Coder (Gemma 4)",
			Provider:      "ollama",
			Tier:          "LOCAL",
			Category:      "Orbit Deterministic Personas",
			Description:   "Deterministic code synthesis, refactoring, and AST verification persona.",
			Offline:       true,
			MinRAMGB:      8.0,
			ContextWindow: 8192,
		},
		{
			ID:            "orbit-thinker:latest",
			Name:          "Orbit Thinker (Gemma 4)",
			Provider:      "ollama",
			Tier:          "LOCAL",
			Category:      "Orbit Deterministic Personas",
			Description:   "Deterministic deep-thinking architecture, GraphRAG reasoning, and spec modeling.",
			Offline:       true,
			MinRAMGB:      8.0,
			ContextWindow: 8192,
		},
		{
			ID:            "orbit-sentinel:latest",
			Name:          "Orbit Sentinel (Gemma 4)",
			Provider:      "ollama",
			Tier:          "LOCAL",
			Category:      "Orbit Deterministic Personas",
			Description:   "Deterministic red/blue team security, credential hardening, and vulnerability sentry.",
			Offline:       true,
			MinRAMGB:      6.0,
			ContextWindow: 8192,
		},
		{
			ID:            "orbit-critic:latest",
			Name:          "Orbit Critic (Gemma 4)",
			Provider:      "ollama",
			Tier:          "LOCAL",
			Category:      "Orbit Deterministic Personas",
			Description:   "Adversarial code review, doubt-driven verification, and invariant validation.",
			Offline:       true,
			MinRAMGB:      8.0,
			ContextWindow: 8192,
		},
		{
			ID:            "orbit-brainstormer:latest",
			Name:          "Orbit Brainstormer (Gemma 4)",
			Provider:      "ollama",
			Tier:          "LOCAL",
			Category:      "Orbit Deterministic Personas",
			Description:   "Divergent ideation, multi-axis concept refinement, and memory consolidation.",
			Offline:       true,
			MinRAMGB:      6.0,
			ContextWindow: 8192,
		},

		// Specialized Code, Recurrent & Multi-modal Open Gemma Models
		{
			ID:            "codegemma:7b",
			Name:          "CodeGemma 7B Local",
			Provider:      "ollama",
			Tier:          "LOCAL",
			Category:      "Specialized Gemma & Vision Models",
			Description:   "Google's specialized model fine-tuned for code generation & completion.",
			Offline:       true,
			MinRAMGB:      8.0,
			ContextWindow: 8192,
		},
		{
			ID:            "codegemma:2b",
			Name:          "CodeGemma 2B Local",
			Provider:      "ollama",
			Tier:          "LOCAL",
			Category:      "Specialized Gemma & Vision Models",
			Description:   "Ultra-fast local code completion & refactoring model.",
			Offline:       true,
			MinRAMGB:      4.0,
			ContextWindow: 8192,
		},
		{
			ID:            "recurrentgemma:2b",
			Name:          "RecurrentGemma 2B Local",
			Provider:      "ollama",
			Tier:          "LOCAL",
			Category:      "Specialized Gemma & Vision Models",
			Description:   "Google DeepMind RNN architecture for infinite context stream processing.",
			Offline:       true,
			MinRAMGB:      4.0,
			ContextWindow: 8192,
		},
		{
			ID:            "paligemma:3b",
			Name:          "PaliGemma 3B Vision Local",
			Provider:      "ollama",
			Tier:          "LOCAL",
			Category:      "Specialized Gemma & Vision Models",
			Description:   "Open vision-language model for local image analysis & UI inspection.",
			Offline:       true,
			MinRAMGB:      6.0,
			ContextWindow: 8192,
		},
		{
			ID:            "gemini-2.0-flash-thinking-exp",
			Name:          "Gemini 2.0 Flash Thinking Cloud",
			Provider:      "cloud",
			Tier:          "PRO",
			Category:      "Gemini 2.0 & 1.5 Series",
			Description:   "Google reasoning model with explicit chain-of-thought step evaluation.",
			Offline:       false,
			MinRAMGB:      0.0,
			ContextWindow: 1048576,
		},

		// Qwen 2.5 Coder & Qwen 3 Open Models
		{
			ID:            "qwen2.5-coder:3b",
			Name:          "Qwen 2.5 Coder 3B Local",
			Provider:      "ollama",
			Tier:          "LOCAL",
			Category:      "Qwen Open Coding & Reasoning Models",
			Description:   "Compact Alibaba Qwen 2.5 Coder model for rapid local code completion & edits.",
			Offline:       true,
			MinRAMGB:      4.0,
			ContextWindow: 32768,
		},
		{
			ID:            "qwen2.5-coder:7b",
			Name:          "Qwen 2.5 Coder 7B Local",
			Provider:      "ollama",
			Tier:          "LOCAL",
			Category:      "Qwen Open Coding & Reasoning Models",
			Description:   "High-accuracy 7B Qwen 2.5 Coder model for multi-file code synthesis & debugging.",
			Offline:       true,
			MinRAMGB:      6.0,
			ContextWindow: 32768,
		},
		{
			ID:            "qwen2.5-coder:14b",
			Name:          "Qwen 2.5 Coder 14B Local",
			Provider:      "ollama",
			Tier:          "LOCAL",
			Category:      "Qwen Open Coding & Reasoning Models",
			Description:   "Strong 14B Qwen 2.5 Coder model for complex refactoring and test engineering.",
			Offline:       true,
			MinRAMGB:      12.0,
			ContextWindow: 32768,
		},
		{
			ID:            "qwen2.5-coder:32b",
			Name:          "Qwen 2.5 Coder 32B Local",
			Provider:      "ollama",
			Tier:          "LOCAL",
			Category:      "Qwen Open Coding & Reasoning Models",
			Description:   "Flagship 32B Qwen 2.5 Coder local model rivaling frontier coding assistants.",
			Offline:       true,
			MinRAMGB:      24.0,
			ContextWindow: 32768,
		},
		{
			ID:            "qwen3:8b",
			Name:          "Qwen 3 8B Local",
			Provider:      "ollama",
			Tier:          "LOCAL",
			Category:      "Qwen Open Coding & Reasoning Models",
			Description:   "Next-generation Qwen 3 8B hybrid thinking & tool-use local model.",
			Offline:       true,
			MinRAMGB:      8.0,
			ContextWindow: 32768,
		},
		{
			ID:            "qwen3:14b",
			Name:          "Qwen 3 14B Local",
			Provider:      "ollama",
			Tier:          "LOCAL",
			Category:      "Qwen Open Coding & Reasoning Models",
			Description:   "Qwen 3 14B local model with deep step-by-step reasoning and tool calling.",
			Offline:       true,
			MinRAMGB:      12.0,
			ContextWindow: 32768,
		},

		// DeepSeek Reasoning & Code Models
		{
			ID:            "deepseek-r1:7b",
			Name:          "DeepSeek-R1 7B Local",
			Provider:      "ollama",
			Tier:          "LOCAL",
			Category:      "DeepSeek Reasoning & Code Models",
			Description:   "Distilled DeepSeek-R1 7B chain-of-thought reasoning model for local inference.",
			Offline:       true,
			MinRAMGB:      6.0,
			ContextWindow: 65536,
		},
		{
			ID:            "deepseek-r1:14b",
			Name:          "DeepSeek-R1 14B Local",
			Provider:      "ollama",
			Tier:          "LOCAL",
			Category:      "DeepSeek Reasoning & Code Models",
			Description:   "Distilled DeepSeek-R1 14B reasoning model for architecture, math, and debugging.",
			Offline:       true,
			MinRAMGB:      12.0,
			ContextWindow: 65536,
		},
		{
			ID:            "deepseek-r1:32b",
			Name:          "DeepSeek-R1 32B Local",
			Provider:      "ollama",
			Tier:          "LOCAL",
			Category:      "DeepSeek Reasoning & Code Models",
			Description:   "High-capacity DeepSeek-R1 32B local reasoning powerhouse.",
			Offline:       true,
			MinRAMGB:      24.0,
			ContextWindow: 65536,
		},
		{
			ID:            "deepseek-coder-v2:16b",
			Name:          "DeepSeek-Coder-V2 16B Local",
			Provider:      "ollama",
			Tier:          "LOCAL",
			Category:      "DeepSeek Reasoning & Code Models",
			Description:   "MoE 16B DeepSeek-Coder-V2 model supporting 300+ programming languages offline.",
			Offline:       true,
			MinRAMGB:      12.0,
			ContextWindow: 65536,
		},

		// Meta Llama Open Models
		{
			ID:            "llama3.2:3b",
			Name:          "Llama 3.2 3B Local",
			Provider:      "ollama",
			Tier:          "LOCAL",
			Category:      "Llama Open Models",
			Description:   "Lightweight Meta Llama 3.2 3B instruct model with 128K context window.",
			Offline:       true,
			MinRAMGB:      4.0,
			ContextWindow: 131072,
		},
		{
			ID:            "llama3.3:70b",
			Name:          "Llama 3.3 70B Local",
			Provider:      "ollama",
			Tier:          "LOCAL",
			Category:      "Llama Open Models",
			Description:   "Flagship Meta Llama 3.3 70B instruction & tool-calling model for high-RAM workstations.",
			Offline:       true,
			MinRAMGB:      48.0,
			ContextWindow: 131072,
		},

		// Mistral & Devstral Models
		{
			ID:            "mistral:7b",
			Name:          "Mistral 7B Local",
			Provider:      "ollama",
			Tier:          "LOCAL",
			Category:      "Mistral & Devstral Models",
			Description:   "Classic fast Mistral 7B local instruct model for general engineering tasks.",
			Offline:       true,
			MinRAMGB:      6.0,
			ContextWindow: 32768,
		},
		{
			ID:            "devstral:24b",
			Name:          "Devstral 24B Local",
			Provider:      "ollama",
			Tier:          "LOCAL",
			Category:      "Mistral & Devstral Models",
			Description:   "Mistral's agentic software engineering 24B model built for repository-scale code tasks.",
			Offline:       true,
			MinRAMGB:      18.0,
			ContextWindow: 131072,
		},

		// Phi & Open Frontier Models
		{
			ID:            "phi4:14b",
			Name:          "Phi-4 14B Local",
			Provider:      "ollama",
			Tier:          "LOCAL",
			Category:      "Phi & Open Frontier Models",
			Description:   "Microsoft Phi-4 14B high-density reasoning and STEM synthesis local model.",
			Offline:       true,
			MinRAMGB:      12.0,
			ContextWindow: 16384,
		},
		{
			ID:            "gpt-oss:20b",
			Name:          "GPT-OSS 20B Local",
			Provider:      "ollama",
			Tier:          "LOCAL",
			Category:      "Phi & Open Frontier Models",
			Description:   "Open-weight 20B reasoning & agentic tool-use model for local workstations.",
			Offline:       true,
			MinRAMGB:      16.0,
			ContextWindow: 131072,
		},
	}

	var installedOllama []string
	var ollamaErr error
	if s != nil && s.ollamaClient != nil {
		installedOllama, ollamaErr = s.ollamaClient.ListModels(ctx)
		if ollamaErr != nil && ctx.Err() != nil {
			return nil, ctx.Err()
		}
	}

	knownIDs := make(map[string]bool, len(knownModels)*2)
	matchedOllama := make(map[string]bool)

	for i := range knownModels {
		knownIDs[knownModels[i].ID] = true
		if knownModels[i].Provider == "ollama" {
			if ollamaErr == nil && ollama.MatchModelName(installedOllama, knownModels[i].ID) {
				knownModels[i].Status = "Downloaded (Ready Offline)"
				markMatchedOllama(installedOllama, knownModels[i].ID, matchedOllama)
			} else if totalRAMGB >= knownModels[i].MinRAMGB {
				knownModels[i].Status = "Available Offline (Can Download)"
			} else {
				knownModels[i].Status = fmt.Sprintf("Requires %.0fGB RAM (PC has %.1fGB)", knownModels[i].MinRAMGB, totalRAMGB)
			}
		} else {
			knownModels[i].Status = "Available Cloud (0 MB Local RAM)"
		}
	}

	// Dynamically append extra installed Ollama models not already in knownModels.
	if ollamaErr == nil && len(installedOllama) > 0 {
		seenExtra := make(map[string]bool)
		for _, name := range installedOllama {
			if name == "" || matchedOllama[name] || knownIDs[name] || seenExtra[name] {
				continue
			}
			seenExtra[name] = true
			knownIDs[name] = true

			desc := "Custom locally installed Ollama model ready for offline execution."
			if strings.HasPrefix(strings.ToLower(name), "hf.co/") {
				desc = "Hugging Face model installed locally in Ollama."
			}
			knownModels = append(knownModels, ModelInfo{
				ID:            name,
				Name:          name,
				Provider:      "ollama",
				Tier:          "LOCAL",
				Category:      customModelsCategory,
				Description:   desc,
				Offline:       true,
				Status:        "Downloaded (Ready Offline)",
				MinRAMGB:      0.0,
				ContextWindow: safeContextWindow(name),
			})
		}
	}

	// Dynamically scan LocalModelsDir for downloaded .gguf files.
	if localDir := s.resolveLocalModelsDir(); localDir != "" {
		ggufModels := scanLocalGGUFDir(localDir, knownIDs)
		knownModels = append(knownModels, ggufModels...)
	}

	return knownModels, nil
}

func markMatchedOllama(installed []string, modelID string, matched map[string]bool) {
	hasTag := strings.Contains(modelID, ":")
	for _, name := range installed {
		if name == modelID || (!hasTag && name == modelID+":latest") {
			matched[name] = true
		}
	}
}

func safeContextWindow(modelID string) int {
	m := strings.ToLower(modelID)
	if strings.Contains(m, "gemini-1.5-pro") || strings.Contains(m, "gemini-2.5-pro") || strings.Contains(m, "gemini-3.1-pro") {
		return 262144
	}
	if strings.Contains(m, "gemini") || strings.Contains(m, "flash") || strings.Contains(m, "pro") || strings.Contains(m, "llama") {
		return 131072
	}
	if strings.Contains(m, "qwen") {
		return 32768
	}
	return 16384
}

func scanLocalGGUFDir(localDir string, seenIDs map[string]bool) []ModelInfo {
	info, err := os.Stat(localDir)
	if err != nil || !info.IsDir() {
		return nil
	}

	var found []ModelInfo
	_ = filepath.WalkDir(localDir, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil || d == nil || d.IsDir() {
			return nil
		}
		name := d.Name()
		lower := strings.ToLower(name)
		if !strings.HasSuffix(lower, ".gguf") || strings.HasSuffix(lower, ".part") {
			return nil
		}
		rel, err := filepath.Rel(localDir, path)
		if err != nil {
			return nil
		}
		relID := filepath.ToSlash(rel)
		if seenIDs[relID] {
			return nil
		}
		seenIDs[relID] = true

		var sizeBytes int64
		if fi, err := d.Info(); err == nil {
			sizeBytes = fi.Size()
		}
		quant := huggingface.ExtractQuantization(name)
		estRAM := huggingface.EstimateRAMGB(sizeBytes)
		desc := fmt.Sprintf("Downloaded local GGUF model (%s)", formatBytes(sizeBytes))
		if quant != "" {
			desc = fmt.Sprintf("Downloaded local GGUF (%s, %s)", quant, formatBytes(sizeBytes))
		}

		found = append(found, ModelInfo{
			ID:            relID,
			Name:          name,
			Provider:      "gguf",
			Tier:          "LOCAL",
			Category:      customModelsCategory,
			Description:   desc,
			Offline:       true,
			Status:        fmt.Sprintf("Downloaded GGUF (%s)", formatBytes(sizeBytes)),
			MinRAMGB:      estRAM,
			ContextWindow: safeContextWindow(name),
			SizeBytes:     sizeBytes,
			Quantization:  quant,
			LocalPath:     path,
		})
		return nil
	})

	sort.Slice(found, func(i, j int) bool {
		return found[i].ID < found[j].ID
	})
	return found
}

func formatBytes(b int64) string {
	const (
		KB = 1024
		MB = 1024 * KB
		GB = 1024 * MB
	)
	switch {
	case b >= GB:
		return fmt.Sprintf("%.1f GB", float64(b)/float64(GB))
	case b >= MB:
		return fmt.Sprintf("%.1f MB", float64(b)/float64(MB))
	case b >= KB:
		return fmt.Sprintf("%.1f KB", float64(b)/float64(KB))
	default:
		return fmt.Sprintf("%d B", b)
	}
}

func GetModelByID(catalog []ModelInfo, id string) (ModelInfo, bool) {
	for _, m := range catalog {
		if m.ID == id {
			return m, true
		}
	}
	return ModelInfo{}, false
}

var (
	TierProStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("15")).
			Background(lipgloss.Color("213")). // Hot Pink / Magenta
			Bold(true).
			Padding(0, 1)

	TierFlashStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("15")).
			Background(lipgloss.Color("39")). // Cyan
			Bold(true).
			Padding(0, 1)

	TierLiteStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("15")).
			Background(lipgloss.Color("214")). // Gold
			Bold(true).
			Padding(0, 1)

	TierLocalStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("15")).
			Background(lipgloss.Color("48")). // Green
			Bold(true).
			Padding(0, 1)

	TierHFStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("15")).
			Background(lipgloss.Color("99")). // Purple
			Bold(true).
			Padding(0, 1)
)

func (m ModelInfo) RenderBadge() string {
	switch m.Tier {
	case "PRO":
		return TierProStyle.Render("PRO TIER")
	case "FLASH":
		return TierFlashStyle.Render("FLASH")
	case "FLASH_LITE":
		return TierLiteStyle.Render("FLASH LITE")
	case "HF":
		return TierHFStyle.Render("HUGGING FACE")
	default:
		if m.Provider == "gguf" {
			return TierLocalStyle.Render("LOCAL GGUF")
		}
		lower := strings.ToLower(m.ID)
		if strings.Contains(lower, "gemma") || strings.Contains(lower, "orbit") {
			return TierLocalStyle.Render("LOCAL GEMMA")
		}
		return TierLocalStyle.Render("LOCAL MODEL")
	}
}

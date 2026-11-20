package models

import (
	"context"
	"fmt"

	"github.com/charmbracelet/lipgloss"
	"github.com/CmfH009/microgravity/internal/ollama"
	"github.com/shirou/gopsutil/v3/mem"
)

type ModelInfo struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	Provider    string  `json:"provider"` // "ollama" or "cloud"
	Tier        string  `json:"tier"`     // "PRO", "FLASH", "FLASH_LITE", "LOCAL"
	Category    string  `json:"category"` // "Gemini 3 Series", "Gemini 2.0 & 1.5", "Gemma Open Models"
	Description string  `json:"description"`
	Offline     bool    `json:"offline"`
	Status      string  `json:"status"` // "Downloaded", "Available Offline", "Available Cloud"
	MinRAMGB      float64 `json:"min_ram_gb"`
	IsActive      bool    `json:"is_active"`
	ContextWindow int     `json:"context_window"`
}

type Scanner struct {
	ollamaClient *ollama.Client
}

func NewScanner() *Scanner {
	return &Scanner{
		ollamaClient: ollama.NewClient("http://localhost:11434"),
	}
}

func (s *Scanner) Scan(ctx context.Context) ([]ModelInfo, error) {
	v, _ := mem.VirtualMemory()
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
	}

	for i := range knownModels {
		if knownModels[i].Provider == "ollama" {
			hasModel, err := s.ollamaClient.HasModel(ctx, knownModels[i].ID)
			if err == nil && hasModel {
				knownModels[i].Status = "Downloaded (Ready Offline)"
			} else if totalRAMGB >= knownModels[i].MinRAMGB {
				knownModels[i].Status = "Available Offline (Can Download)"
			} else {
				knownModels[i].Status = fmt.Sprintf("Requires %.0fGB RAM (PC has %.1fGB)", knownModels[i].MinRAMGB, totalRAMGB)
			}
		} else {
			knownModels[i].Status = "Available Cloud (0 MB Local RAM)"
		}
	}

	return knownModels, nil
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
)

func (m ModelInfo) RenderBadge() string {
	switch m.Tier {
	case "PRO":
		return TierProStyle.Render("PRO TIER")
	case "FLASH":
		return TierFlashStyle.Render("FLASH")
	case "FLASH_LITE":
		return TierLiteStyle.Render("FLASH LITE")
	default:
		return TierLocalStyle.Render("LOCAL GEMMA")
	}
}


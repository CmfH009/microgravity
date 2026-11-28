# Model Catalog & Availability Scanner Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a comprehensive model scanner and catalog (`mg models`) for all Gemma and Gemini models, auditing local PC hardware (RAM, Ollama status, downloaded weights) vs Cloud status, with use-case descriptions and continuous availability tracking.

**Architecture:** Create an `internal/models` package containing a structured `Catalog` of Gemma & Gemini models with metadata (offline support, hardware requirements, use cases). Provide hardware diagnostic scanning and integrate CLI commands in `cmd/models.go`.

**Tech Stack:** Go, LipGloss, gopsutil, Ollama API, Cobra CLI.

---

### Task 1: Create Model Catalog & Hardware Scanner (`internal/models`)

**Files:**
- Create: `internal/models/models.go`
- Create: `internal/models/models_test.go`

- [ ] **Step 1: Write the failing unit test for Model Catalog & Hardware Scanner**

```go
package models

import (
	"context"
	"testing"
)

func TestGetCatalog(t *testing.T) {
	scanner := NewScanner()
	catalog, err := scanner.Scan(context.Background())
	if err != nil {
		t.Fatalf("Scan failed: %v", err)
	}

	if len(catalog) == 0 {
		t.Fatalf("Expected non-empty model catalog")
	}

	// Verify key Gemma & Gemini models exist in catalog
	foundGemma2b := false
	foundGeminiFlash := false
	for _, m := range catalog {
		if m.ID == "gemma:2b" {
			foundGemma2b = true
			if !m.Offline {
				t.Errorf("gemma:2b expected Offline=true")
			}
		}
		if m.ID == "gemini-1.5-flash" || m.ID == "gemma-2-9b-it" {
			foundGeminiFlash = true
		}
	}

	if !foundGemma2b || !foundGeminiFlash {
		t.Errorf("Catalog missing expected Gemma/Gemini models")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test -v ./internal/models/...`
Expected: FAIL with "package models not found"

- [ ] **Step 3: Write minimal implementation in `internal/models/models.go`**

```go
package models

import (
	"context"
	"fmt"

	"github.com/purav/microgravity/internal/ollama"
	"github.com/shirou/gopsutil/v3/mem"
)

type ModelInfo struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Provider    string `json:"provider"` // "ollama" or "cloud"
	Description string `json:"description"`
	Offline     bool   `json:"offline"`
	Status      string `json:"status"` // "Downloaded", "Available Offline", "Available Cloud", "Requires API Key"
	MinRAMGB    float64 `json:"min_ram_gb"`
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
		{
			ID:          "gemma:2b",
			Name:        "Gemma 2.6B Local",
			Provider:    "ollama",
			Description: "Ultra-fast local model for quick edits, low memory usage, runs offline.",
			Offline:     true,
			MinRAMGB:    4.0,
		},
		{
			ID:          "gemma:7b",
			Name:        "Gemma 7B Local",
			Provider:    "ollama",
			Description: "Balanced local model for code generation, logic reasoning, and analysis.",
			Offline:     true,
			MinRAMGB:    8.0,
		},
		{
			ID:          "gemma-2-9b-it",
			Name:        "Gemma 2 9B Cloud",
			Provider:    "cloud",
			Description: "High-performance instruction-tuned cloud model for multi-step reasoning.",
			Offline:     false,
			MinRAMGB:    0.0,
		},
		{
			ID:          "gemini-1.5-flash",
			Name:        "Gemini 1.5 Flash Cloud",
			Provider:    "cloud",
			Description: "Super-fast multimodal cloud model for low-latency reasoning and coding.",
			Offline:     false,
			MinRAMGB:    0.0,
		},
		{
			ID:          "gemini-1.5-pro",
			Name:        "Gemini 1.5 Pro Cloud",
			Provider:    "cloud",
			Description: "Advanced reasoning model with 2M token context for massive codebases.",
			Offline:     false,
			MinRAMGB:    0.0,
		},
	}

	for i := range knownModels {
		if knownModels[i].Provider == "ollama" {
			hasModel, err := s.ollamaClient.HasModel(ctx, knownModels[i].ID)
			if err == nil && hasModel {
				knownModels[i].Status = "Downloaded (Ready)"
			} else if totalRAMGB >= knownModels[i].MinRAMGB {
				knownModels[i].Status = "Available Offline (Can Download)"
			} else {
				knownModels[i].Status = fmt.Sprintf("Requires %.0fGB RAM (PC has %.1fGB)", knownModels[i].MinRAMGB, totalRAMGB)
			}
		} else {
			knownModels[i].Status = "Available Cloud"
		}
	}

	return knownModels, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test -v ./internal/models/...`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/models/models.go internal/models/models_test.go
git commit -m "feat(models): add model catalog and hardware availability scanner"
```

---

### Task 2: Create Interactive `mg models` TUI Commands (`cmd/models.go`)

**Files:**
- Create: `cmd/models.go`

- [ ] **Step 1: Write `cmd/models.go` with LipGloss TUI Table & Status Badges**

```go
package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/purav/microgravity/internal/models"
	"github.com/purav/microgravity/internal/ui"
	"github.com/spf13/cobra"
)

var modelsCmd = &cobra.Command{
	Use:   "models",
	Short: "Scan and inspect available Gemma & Gemini models",
	Run: func(cmd *cobra.Command, args []string) {
		cmd.Help()
	},
}

var modelsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all local (offline) and cloud Gemma/Gemini models",
	Run: func(cmd *cobra.Command, args []string) {
		scanner := models.NewScanner()
		catalog, err := scanner.Scan(context.Background())
		if err != nil {
			fmt.Printf("%s\n", ui.PrintError(fmt.Errorf("Failed to scan models: %v", err)))
			os.Exit(1)
		}

		cardStyle := lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("39")).
			Padding(1, 2)

		var sb strings.Builder
		sb.WriteString("🤖 MODEL AVAILABILITY & HARDWARE AUDIT\n\n")

		for _, m := range catalog {
			modeBadge := "☁️ CLOUD"
			if m.Offline {
				modeBadge = "💻 OFFLINE"
			}
			sb.WriteString(fmt.Sprintf("• %s [%s] (%s)\n  Status: %s\n  Use Case: %s\n\n",
				m.ID, m.Name, modeBadge, m.Status, m.Description))
		}

		sb.WriteString("To switch active model: mg chat --model <name>")
		fmt.Println(cardStyle.Render(sb.String()))
	},
}

func init() {
	modelsCmd.AddCommand(modelsListCmd)
	rootCmd.AddCommand(modelsCmd)
}
```

- [ ] **Step 2: Run test suite to verify compilation**

Run: `go test ./...`
Expected: PASS

- [ ] **Step 3: Build & Install updated `mg.exe`**

Run: `go build -o <USER_HOME>\go\bin\mg.exe main.go`
Expected: Build succeeds

- [ ] **Step 4: Commit**

```bash
git add cmd/models.go
git commit -m "feat(models): add mg models CLI command for model hardware audit"
```

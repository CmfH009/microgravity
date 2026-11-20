package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/CmfH009/microgravity/internal/config"
	"github.com/CmfH009/microgravity/internal/huggingface"
	"github.com/CmfH009/microgravity/internal/models"
	"github.com/CmfH009/microgravity/internal/ollama"
	"github.com/CmfH009/microgravity/internal/ui"
	"github.com/charmbracelet/lipgloss"
	"github.com/shirou/gopsutil/v3/mem"
	"github.com/spf13/cobra"
)

var (
	modelsListLocal      bool
	modelsListDownloaded bool
	modelsListCategory   string
	modelsListJSON       bool

	modelsSearchLimit int
	modelsSearchGGUF  bool
	modelsSearchSort  string
	modelsSearchJSON  bool

	modelsHFQuant  string
	modelsHFDir    string
	modelsHFOllama bool
	modelsHFSelect bool
	modelsHFURL    string

	modelsAutoQuant  string
	modelsAutoDir    string
	modelsAutoOllama bool
	modelsAutoSelect bool
	modelsAutoURL    string

	// Injectable dependencies for hermetic unit tests.
	modelsScannerFactory = models.NewScanner
	hfClientFactory      = func() *huggingface.Client { return huggingface.NewClient("") }
	ollamaClientFactory  = ollama.NewClient
	ensureOllamaFn       = ollama.EnsureOllama
	availableRAMGBFn     = detectAvailableRAMGB
)

func detectAvailableRAMGB(ctx context.Context) float64 {
	if ctx == nil {
		ctx = context.Background()
	}
	v, err := mem.VirtualMemoryWithContext(ctx)
	if err != nil || v == nil {
		return 0
	}
	if v.Available > 0 {
		return float64(v.Available) / (1024 * 1024 * 1024)
	}
	if v.Total > 0 {
		return float64(v.Total) / (1024 * 1024 * 1024) * 0.75
	}
	return 0
}

var modelsCmd = &cobra.Command{
	Use:   "models",
	Short: "Scan, search, download, and select local and cloud models",
	Run: func(cmd *cobra.Command, args []string) {
		selected, ok := models.RunTUIWithDeps(context.Background(), modelsScannerFactory(), hfClientFactory())
		if ok {
			ui.PrintStepSuccess(fmt.Sprintf("Active model set to: %s (%s)", selected.ID, selected.Name))
		}
	},
}

var modelsListCmd = &cobra.Command{
	Use:          "list",
	Short:        "List all local (offline), downloaded GGUF, and cloud models",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		if ctx == nil {
			ctx = context.Background()
		}
		scanner := modelsScannerFactory()
		catalog, err := scanner.Scan(ctx)
		if err != nil {
			return fmt.Errorf("failed to scan models: %w", err)
		}

		activeID := config.GetActiveModel()
		filtered := make([]models.ModelInfo, 0, len(catalog))
		catFilter := strings.ToLower(strings.TrimSpace(modelsListCategory))
		for _, m := range catalog {
			if m.ID == activeID {
				m.IsActive = true
			}
			if modelsListLocal && !m.Offline {
				continue
			}
			if modelsListDownloaded && !strings.Contains(strings.ToLower(m.Status), "downloaded") {
				continue
			}
			if catFilter != "" && !strings.Contains(strings.ToLower(m.Category), catFilter) {
				continue
			}
			filtered = append(filtered, m)
		}

		out := cmd.OutOrStdout()
		if modelsListJSON {
			data, err := json.MarshalIndent(filtered, "", "  ")
			if err != nil {
				return fmt.Errorf("encode models JSON: %w", err)
			}
			fmt.Fprintln(out, string(data))
			return nil
		}

		boxStyle := lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("39")).
			Padding(1, 2)

		var sb strings.Builder
		sb.WriteString("🤖 MICROGRAVITY MODEL CATALOG & HARDWARE AUDIT\n\n")

		if len(filtered) == 0 {
			sb.WriteString("No models matched the specified filters.\n")
		} else {
			var currentCategory string
			for i, m := range filtered {
				if m.Category != currentCategory {
					currentCategory = m.Category
					sb.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("39")).Bold(true).Render("── "+currentCategory+" ──") + "\n\n")
				}

				activeTag := ""
				if m.ID == activeID {
					activeTag = "  [ACTIVE MODEL]"
				}

				sb.WriteString(fmt.Sprintf("%d. %s [%s]%s\n   Status: %s\n   Use Case: %s\n\n",
					i+1, m.ID, m.RenderBadge(), activeTag, m.Status, m.Description))
			}
		}

		sb.WriteString("\nSwitch model: mg models select <id> | Search HF: mg models search <query> | TUI: mg models")
		fmt.Fprintln(out, boxStyle.Render(sb.String()))
		return nil
	},
}

var modelsSelectCmd = &cobra.Command{
	Use:          "select [model-id]",
	Short:        "Select active model by catalog ID, hf.co/... reference, Ollama model, or local .gguf file",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		if ctx == nil {
			ctx = context.Background()
		}
		targetID := strings.TrimSpace(args[0])
		if targetID == "" {
			return fmt.Errorf("model ID cannot be empty")
		}

		scanner := modelsScannerFactory()
		catalog, err := scanner.Scan(ctx)
		if err != nil {
			return fmt.Errorf("failed to scan models: %w", err)
		}

		resolvedID, resolvedName, ok := resolveSelectableModel(ctx, catalog, targetID)
		if !ok {
			return fmt.Errorf("unknown model ID %q. Run 'mg models list' or provide an hf.co/<owner>/<repo> ref or .gguf file path", targetID)
		}

		if err := config.SetActiveModel(resolvedID); err != nil {
			return fmt.Errorf("failed to save active model preference: %w", err)
		}

		fmt.Fprintf(cmd.OutOrStdout(), "✔ Successfully selected active model: %s (%s)\n", resolvedID, resolvedName)
		return nil
	},
}

func resolveSelectableModel(ctx context.Context, catalog []models.ModelInfo, targetID string) (string, string, bool) {
	if m, found := models.GetModelByID(catalog, targetID); found {
		return m.ID, m.Name, true
	}

	// Match local .gguf entries in catalog by filename or full LocalPath
	for _, m := range catalog {
		if m.Provider == "gguf" && (m.Name == targetID || (m.LocalPath != "" && filepath.Clean(m.LocalPath) == filepath.Clean(targetID))) {
			return m.ID, m.Name, true
		}
	}

	// Accept custom Hugging Face refs (hf.co/<owner>/<repo>[:quant] or hf://<owner>/<repo>[:quant])
	if repoID, quant, isHF := huggingface.ParseRef(targetID); isHF {
		if cleanRepo, err := huggingface.ValidateRepoID(repoID); err == nil {
			ref := huggingface.OllamaRef(cleanRepo, quant)
			return ref, "Hugging Face Model", true
		}
	}

	// Accept existing .gguf file path on disk
	if strings.HasSuffix(strings.ToLower(targetID), ".gguf") {
		if fi, err := os.Stat(targetID); err == nil && !fi.IsDir() {
			absPath, absErr := filepath.Abs(targetID)
			if absErr != nil {
				absPath = targetID
			}
			return absPath, filepath.Base(absPath), true
		}
	}

	// Accept locally installed Ollama model (e.g., custom tag match)
	ollamaURL := baseURL
	if strings.TrimSpace(ollamaURL) == "" {
		ollamaURL = "http://localhost:11434"
	}
	if has, err := ollamaClientFactory(ollamaURL).HasModel(ctx, targetID); err == nil && has {
		return targetID, targetID, true
	}

	return "", "", false
}

var modelsCurrentCmd = &cobra.Command{
	Use:          "current",
	Short:        "Show currently active model",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		if ctx == nil {
			ctx = context.Background()
		}
		activeID := config.GetActiveModel()
		scanner := modelsScannerFactory()
		catalog, _ := scanner.Scan(ctx)
		m, found := models.GetModelByID(catalog, activeID)

		if found {
			fmt.Fprintf(cmd.OutOrStdout(), "ℹ Active Model: %s [%s] (%s)\n", m.ID, m.RenderBadge(), m.Description)
		} else {
			fmt.Fprintf(cmd.OutOrStdout(), "ℹ Active Model: %s\n", activeID)
		}
		return nil
	},
}

var modelsSearchCmd = &cobra.Command{
	Use:          "search <query>",
	Short:        "Search Hugging Face Hub for models and GGUF weights",
	Args:         cobra.MinimumNArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		if ctx == nil {
			ctx = context.Background()
		}
		query := strings.TrimSpace(strings.Join(args, " "))
		client := hfClientFactory()

		results, err := client.SearchModels(ctx, huggingface.SearchOptions{
			Query:    query,
			GGUFOnly: modelsSearchGGUF,
			Sort:     modelsSearchSort,
			Limit:    modelsSearchLimit,
		})
		if err != nil {
			return fmt.Errorf("huggingface search failed: %w", err)
		}

		out := cmd.OutOrStdout()
		if modelsSearchJSON {
			data, err := json.MarshalIndent(results, "", "  ")
			if err != nil {
				return fmt.Errorf("encode search results JSON: %w", err)
			}
			fmt.Fprintln(out, string(data))
			return nil
		}

		if len(results) == 0 {
			fmt.Fprintf(out, "No models found on Hugging Face for %q.\n", query)
			return nil
		}

		fmt.Fprintf(out, "Hugging Face Search Results for %q (%d):\n\n", query, len(results))
		for i, r := range results {
			fmt.Fprintf(out, "%2d. %-52s  ↓ %-8d  ♥ %d\n", i+1, r.ID, r.Downloads, r.Likes)
		}
		fmt.Fprintf(out, "\nDownload best GGUF: mg models hf-download <repo-id>  |  Auto: mg models auto %q\n", query)
		return nil
	},
}

var modelsHFDownloadCmd = &cobra.Command{
	Use:          "hf-download <repo-id>",
	Short:        "Download a GGUF model from Hugging Face (auto-selecting best quant for RAM or via --quant)",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		if ctx == nil {
			ctx = context.Background()
		}
		repoID, embeddedQuant, _ := huggingface.ParseRef(args[0])
		cleanRepo, err := huggingface.ValidateRepoID(repoID)
		if err != nil {
			return err
		}
		quant := strings.TrimSpace(modelsHFQuant)
		if quant == "" {
			quant = embeddedQuant
		}

		return executeHFInstall(ctx, cmd.OutOrStdout(), cleanRepo, quant, modelsHFDir, modelsHFOllama, modelsHFSelect, modelsHFURL)
	},
}

var modelsAutoCmd = &cobra.Command{
	Use:          "auto <query>",
	Short:        "Search Hugging Face, pick the best RAM-fitting GGUF, download/pull, and optionally select it",
	Args:         cobra.MinimumNArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		if ctx == nil {
			ctx = context.Background()
		}
		query := strings.TrimSpace(strings.Join(args, " "))
		hfClient := hfClientFactory()
		out := cmd.OutOrStdout()

		results, err := hfClient.SearchModels(ctx, huggingface.SearchOptions{
			Query:    query,
			GGUFOnly: true,
			Sort:     "downloads",
			Limit:    10,
		})
		if err != nil {
			return fmt.Errorf("huggingface search failed: %w", err)
		}
		if len(results) == 0 {
			return fmt.Errorf("no GGUF models found on Hugging Face for %q", query)
		}

		availRAM := availableRAMGBFn(ctx)
		quant := strings.TrimSpace(modelsAutoQuant)

		var chosenRepo string
		var chosenFile huggingface.GGUFFile
		var lastErr error
		maxProbe := len(results)
		if maxProbe > 5 {
			maxProbe = 5
		}

		for i := 0; i < maxProbe; i++ {
			candidate := results[i].ID
			files, listErr := hfClient.ListGGUFFiles(ctx, candidate)
			if listErr != nil {
				lastErr = listErr
				continue
			}
			best, selErr := huggingface.SelectBestGGUF(files, availRAM, quant)
			if selErr != nil {
				lastErr = selErr
				continue
			}
			chosenRepo = candidate
			chosenFile = best
			break
		}

		if chosenRepo == "" {
			if lastErr != nil {
				return fmt.Errorf("no suitable GGUF found for %q: %w", query, lastErr)
			}
			return fmt.Errorf("no suitable GGUF found for %q", query)
		}

		fmt.Fprintf(out, "Auto-selected %s -> %s (%s, ~%.1f GB RAM)\n",
			chosenRepo, chosenFile.Filename, chosenFile.Quantization, chosenFile.EstimatedRAMGB)

		if modelsAutoOllama {
			q := quant
			if q == "" {
				q = chosenFile.Quantization
			}
			ref := huggingface.OllamaRef(chosenRepo, q)
			if err := pullOllamaRef(ctx, out, modelsAutoURL, ref); err != nil {
				return err
			}
			if modelsAutoSelect {
				if err := config.SetActiveModel(ref); err != nil {
					return fmt.Errorf("failed to set active model: %w", err)
				}
				fmt.Fprintf(out, "✔ Active model set to: %s\n", ref)
			}
			return nil
		}

		destDir := resolveGGUFDestDir(modelsAutoDir)
		destPath, err := downloadGGUFWithProgress(ctx, hfClient, chosenRepo, chosenFile, destDir)
		if err != nil {
			return err
		}
		fmt.Fprintf(out, "✔ Downloaded %s -> %s\n", chosenFile.Filename, destPath)

		if modelsAutoSelect {
			activeID := activeIDForDownloadedGGUF(destDir, destPath)
			if err := config.SetActiveModel(activeID); err != nil {
				return fmt.Errorf("failed to set active model: %w", err)
			}
			fmt.Fprintf(out, "✔ Active model set to: %s\n", activeID)
		}
		return nil
	},
}

func executeHFInstall(ctx context.Context, out io.Writer, repoID, quant, dirOverride string, useOllama, selectActive bool, ollamaURL string) error {
	if useOllama {
		ref := huggingface.OllamaRef(repoID, quant)
		if err := pullOllamaRef(ctx, out, ollamaURL, ref); err != nil {
			return err
		}
		if selectActive {
			if err := config.SetActiveModel(ref); err != nil {
				return fmt.Errorf("failed to set active model: %w", err)
			}
			fmt.Fprintf(out, "✔ Active model set to: %s\n", ref)
		}
		return nil
	}

	hfClient := hfClientFactory()
	files, err := hfClient.ListGGUFFiles(ctx, repoID)
	if err != nil {
		return err
	}

	availRAM := availableRAMGBFn(ctx)
	chosen, err := huggingface.SelectBestGGUF(files, availRAM, quant)
	if err != nil {
		return err
	}

	destDir := resolveGGUFDestDir(dirOverride)
	if destDir == "" {
		return errors.New("could not resolve destination directory for GGUF download")
	}

	fmt.Fprintf(out, "Selected %s (%s, %s, est. %.1f GB RAM) from %s\n",
		chosen.Filename, chosen.Quantization, ui.HumanBytes(chosen.SizeBytes), chosen.EstimatedRAMGB, repoID)

	destPath, err := downloadGGUFWithProgress(ctx, hfClient, repoID, chosen, destDir)
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "✔ Downloaded %s -> %s\n", chosen.Filename, destPath)

	if selectActive {
		activeID := activeIDForDownloadedGGUF(destDir, destPath)
		if err := config.SetActiveModel(activeID); err != nil {
			return fmt.Errorf("failed to set active model: %w", err)
		}
		fmt.Fprintf(out, "✔ Active model set to: %s\n", activeID)
	}
	return nil
}

func resolveGGUFDestDir(override string) string {
	if strings.TrimSpace(override) != "" {
		return strings.TrimSpace(override)
	}
	return models.DefaultLocalModelsDir()
}

func activeIDForDownloadedGGUF(destDir, destPath string) string {
	defaultDir := models.DefaultLocalModelsDir()
	if defaultDir != "" {
		absDefault, err1 := filepath.Abs(defaultDir)
		absDest, err2 := filepath.Abs(destPath)
		if err1 == nil && err2 == nil {
			if rel, err := filepath.Rel(absDefault, absDest); err == nil && !strings.HasPrefix(rel, "..") {
				return filepath.ToSlash(rel)
			}
		}
	}
	if rel, err := filepath.Rel(destDir, destPath); err == nil && !strings.HasPrefix(rel, "..") && destDir == defaultDir {
		return filepath.ToSlash(rel)
	}
	return destPath
}

func downloadGGUFWithProgress(ctx context.Context, hfClient *huggingface.Client, repoID string, file huggingface.GGUFFile, destDir string) (string, error) {
	var bar *ui.ProgressBar
	destPath, err := hfClient.DownloadGGUF(ctx, repoID, file.Filename, destDir, func(completed, total int64) {
		if total <= 0 {
			return
		}
		if bar == nil {
			bar = ui.NewProgressBar("Downloading "+filepath.Base(file.Filename), total)
		}
		bar.Update(completed)
	})
	if bar != nil {
		bar.Finish()
	}
	return destPath, err
}

func pullOllamaRef(ctx context.Context, out io.Writer, ollamaURL, modelRef string) error {
	if strings.TrimSpace(ollamaURL) == "" {
		ollamaURL = "http://localhost:11434"
	}
	ensureOllamaFn()
	client := ollamaClientFactory(ollamaURL)

	hasModel, _ := client.HasModel(ctx, modelRef)
	if hasModel {
		fmt.Fprintf(out, "✔ Model %s is already downloaded and up to date.\n", modelRef)
		return nil
	}

	fmt.Fprintf(out, "Requesting %s from Ollama...\n", modelRef)
	var bar *ui.ProgressBar
	err := client.Pull(ctx, modelRef, func(p ollama.PullProgress) {
		switch {
		case p.Total > 0 && p.Completed > 0:
			label := p.Digest
			if len(label) > 12 {
				label = label[:12]
			}
			if label == "" {
				label = modelRef
			}
			if bar == nil || bar.Label() != "Downloading "+label {
				if bar != nil {
					bar.Finish()
				}
				bar = ui.NewProgressBar("Downloading "+label, p.Total)
			}
			bar.Update(p.Completed)
		case p.Status == "success":
			if bar != nil {
				bar.Finish()
				bar = nil
			}
		default:
			if bar != nil {
				bar.Finish()
				bar = nil
			}
			if p.Status != "" {
				fmt.Fprintf(out, "  ✔ %s\n", p.Status)
			}
		}
	})
	if bar != nil {
		bar.Finish()
	}
	if err != nil {
		return fmt.Errorf("pull failed: %w", err)
	}
	fmt.Fprintf(out, "✔ Model %s ready.\n", modelRef)
	return nil
}

func init() {
	modelsListCmd.Flags().BoolVar(&modelsListLocal, "local", false, "Show only local/offline models")
	modelsListCmd.Flags().BoolVar(&modelsListDownloaded, "downloaded", false, "Show only downloaded models")
	modelsListCmd.Flags().StringVar(&modelsListCategory, "category", "", "Filter models by category substring")
	modelsListCmd.Flags().BoolVar(&modelsListJSON, "json", false, "Output models catalog as JSON")

	modelsSearchCmd.Flags().IntVar(&modelsSearchLimit, "limit", 10, "Maximum number of results to return")
	modelsSearchCmd.Flags().BoolVar(&modelsSearchGGUF, "gguf", true, "Filter to repositories with GGUF weights")
	modelsSearchCmd.Flags().StringVar(&modelsSearchSort, "sort", "downloads", "Sort order: downloads, likes, or lastModified")
	modelsSearchCmd.Flags().BoolVar(&modelsSearchJSON, "json", false, "Output search results as JSON")

	modelsHFDownloadCmd.Flags().StringVar(&modelsHFQuant, "quant", "", "Preferred GGUF quantization (e.g. Q4_K_M, Q8_0)")
	modelsHFDownloadCmd.Flags().StringVar(&modelsHFDir, "dir", "", "Destination directory for downloaded GGUF (default ~/.microgravity/models)")
	modelsHFDownloadCmd.Flags().BoolVar(&modelsHFOllama, "ollama", false, "Pull via Ollama (hf.co/...) instead of direct GGUF download")
	modelsHFDownloadCmd.Flags().BoolVar(&modelsHFSelect, "select", false, "Set downloaded model as active model")
	modelsHFDownloadCmd.Flags().StringVar(&modelsHFURL, "url", "http://localhost:11434", "Ollama API base URL when --ollama is used")

	modelsAutoCmd.Flags().StringVar(&modelsAutoQuant, "quant", "", "Preferred GGUF quantization (e.g. Q4_K_M, Q8_0)")
	modelsAutoCmd.Flags().StringVar(&modelsAutoDir, "dir", "", "Destination directory for downloaded GGUF (default ~/.microgravity/models)")
	modelsAutoCmd.Flags().BoolVar(&modelsAutoOllama, "ollama", false, "Pull via Ollama (hf.co/...) instead of direct GGUF download")
	modelsAutoCmd.Flags().BoolVar(&modelsAutoSelect, "select", false, "Set downloaded model as active model")
	modelsAutoCmd.Flags().StringVar(&modelsAutoURL, "url", "http://localhost:11434", "Ollama API base URL when --ollama is used")

	modelsCmd.AddCommand(modelsListCmd)
	modelsCmd.AddCommand(modelsSelectCmd)
	modelsCmd.AddCommand(modelsCurrentCmd)
	modelsCmd.AddCommand(modelsSearchCmd)
	modelsCmd.AddCommand(modelsHFDownloadCmd)
	modelsCmd.AddCommand(modelsAutoCmd)
	rootCmd.AddCommand(modelsCmd)
}

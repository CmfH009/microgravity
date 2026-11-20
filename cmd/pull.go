package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/CmfH009/microgravity/internal/config"
	"github.com/CmfH009/microgravity/internal/huggingface"
	"github.com/CmfH009/microgravity/internal/ui"
	"github.com/spf13/cobra"
)

var (
	pullGGUF   bool
	pullOllama bool
	pullDir    string
	pullQuant  string
	pullSelect bool
)

var pullCmd = &cobra.Command{
	Use:   "pull [model]",
	Short: "Download a model via Ollama or directly from Hugging Face Hub",
	Long: `Download an Ollama model or a Hugging Face GGUF repository with live progress.

Examples:
  mg pull gemma4:e2b
  mg pull hf://Qwen/Qwen2.5-Coder-7B-Instruct-GGUF
  mg pull hf://Qwen/Qwen2.5-Coder-7B-Instruct-GGUF:Q4_K_M --select
  mg pull hf.co/Qwen/Qwen2.5-Coder-7B-Instruct-GGUF:Q4_K_M`,
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		if pullGGUF && pullOllama {
			return fmt.Errorf("cannot specify both --gguf and --ollama")
		}

		target := strings.TrimSpace(args[0])
		if target == "" {
			return fmt.Errorf("model argument cannot be empty")
		}

		ctx := cmd.Context()
		if ctx == nil {
			ctx = context.Background()
		}
		out := cmd.OutOrStdout()
		startTime := time.Now()

		repoID, embeddedQuant, isHF := huggingface.ParseRef(target)
		lowerTarget := strings.ToLower(target)
		isHFScheme := strings.HasPrefix(lowerTarget, "hf://")

		quant := strings.TrimSpace(pullQuant)
		if quant == "" {
			quant = embeddedQuant
		}

		// Standard Ollama registry model (neither hf://, hf.co/, nor --gguf)
		if !isHF && !pullGGUF {
			if err := pullOllamaRef(ctx, out, baseURL, target); err != nil {
				return err
			}
			if pullSelect {
				if err := config.SetActiveModel(target); err != nil {
					return fmt.Errorf("failed to set active model: %w", err)
				}
				fmt.Fprintf(out, "✔ Active model set to: %s\n", target)
			}
			elapsed := time.Since(startTime).Round(time.Millisecond * 100)
			fmt.Fprintf(out, "✔ Model %s ready (%s)\n", target, elapsed)
			return nil
		}

		cleanRepo, err := huggingface.ValidateRepoID(repoID)
		if err != nil {
			return err
		}

		// Force Ollama pull for hf.co/... (without --gguf) or explicit --ollama
		if pullOllama || (!isHFScheme && !pullGGUF) {
			ref := huggingface.OllamaRef(cleanRepo, quant)
			if err := pullOllamaRef(ctx, out, baseURL, ref); err != nil {
				return err
			}
			if pullSelect {
				if err := config.SetActiveModel(ref); err != nil {
					return fmt.Errorf("failed to set active model: %w", err)
				}
				fmt.Fprintf(out, "✔ Active model set to: %s\n", ref)
			}
			elapsed := time.Since(startTime).Round(time.Millisecond * 100)
			fmt.Fprintf(out, "✔ Model %s ready (%s)\n", ref, elapsed)
			return nil
		}

		// Direct GGUF download when hf:// or --gguf
		hfClient := hfClientFactory()
		files, err := hfClient.ListGGUFFiles(ctx, cleanRepo)
		if err != nil {
			// If hf:// repo has no GGUF files and --gguf wasn't forced, fall back to Ollama hf.co/... pull
			if errors.Is(err, huggingface.ErrNoGGUFFiles) && !pullGGUF {
				ref := huggingface.OllamaRef(cleanRepo, quant)
				fmt.Fprintf(out, "ℹ No GGUF files found in %s; falling back to Ollama pull (%s)\n", cleanRepo, ref)
				if pullErr := pullOllamaRef(ctx, out, baseURL, ref); pullErr != nil {
					return pullErr
				}
				if pullSelect {
					if setErr := config.SetActiveModel(ref); setErr != nil {
						return fmt.Errorf("failed to set active model: %w", setErr)
					}
					fmt.Fprintf(out, "✔ Active model set to: %s\n", ref)
				}
				elapsed := time.Since(startTime).Round(time.Millisecond * 100)
				fmt.Fprintf(out, "✔ Model %s ready (%s)\n", ref, elapsed)
				return nil
			}
			return err
		}

		availRAM := availableRAMGBFn(ctx)
		chosen, err := huggingface.SelectBestGGUF(files, availRAM, quant)
		if err != nil {
			return err
		}

		destDir := resolveGGUFDestDir(pullDir)
		if destDir == "" {
			return errors.New("could not resolve destination directory for GGUF download")
		}

		if out == os.Stdout {
			fmt.Println()
			ui.PrintPhaseHeader("Pulling Hugging Face GGUF", "📦")
		}
		fmt.Fprintf(out, "Downloading %s (%s, %s, est. %.1f GB RAM) from %s...\n",
			chosen.Filename, chosen.Quantization, ui.HumanBytes(chosen.SizeBytes), chosen.EstimatedRAMGB, cleanRepo)

		destPath, err := downloadGGUFWithProgress(ctx, hfClient, cleanRepo, chosen, destDir)
		if err != nil {
			return err
		}

		if pullSelect {
			activeID := activeIDForDownloadedGGUF(destDir, destPath)
			if err := config.SetActiveModel(activeID); err != nil {
				return fmt.Errorf("failed to set active model: %w", err)
			}
			fmt.Fprintf(out, "✔ Active model set to: %s\n", activeID)
		}

		elapsed := time.Since(startTime).Round(time.Millisecond * 100)
		fmt.Fprintf(out, "✔ Downloaded %s -> %s (%s)\n", chosen.Filename, destPath, elapsed)
		return nil
	},
}

func init() {
	pullCmd.Flags().StringVarP(&baseURL, "url", "u", "http://localhost:11434", "Ollama API base URL")
	pullCmd.Flags().BoolVar(&pullGGUF, "gguf", false, "Force direct GGUF file download from Hugging Face")
	pullCmd.Flags().BoolVar(&pullOllama, "ollama", false, "Pull Hugging Face model via Ollama (hf.co/...)")
	pullCmd.Flags().StringVar(&pullDir, "dir", "", "Destination directory for GGUF downloads (default ~/.microgravity/models)")
	pullCmd.Flags().StringVar(&pullQuant, "quant", "", "Preferred GGUF quantization (e.g. Q4_K_M, Q8_0)")
	pullCmd.Flags().BoolVar(&pullSelect, "select", false, "Set pulled/downloaded model as the active model")
	rootCmd.AddCommand(pullCmd)
}

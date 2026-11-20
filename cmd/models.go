package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/purav/microgravity/internal/config"
	"github.com/purav/microgravity/internal/models"
	"github.com/purav/microgravity/internal/ui"
	"github.com/spf13/cobra"
)

var modelsCmd = &cobra.Command{
	Use:   "models",
	Short: "Scan, inspect, and select Gemma & Gemini models",
	Run: func(cmd *cobra.Command, args []string) {
		selected, ok := models.RunTUI(context.Background())
		if ok {
			ui.PrintStepSuccess(fmt.Sprintf("Active model set to: %s (%s)", selected.ID, selected.Name))
		}
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

		activeID := config.GetActiveModel()

		boxStyle := lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("39")).
			Padding(1, 2)

		var sb strings.Builder
		sb.WriteString("🤖 GEMMA & GEMINI MODEL CATALOG & HARDWARE AUDIT\n\n")

		var currentCategory string
		for i, m := range catalog {
			if m.Category != currentCategory {
				currentCategory = m.Category
				sb.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("39")).Bold(true).Render("── "+currentCategory+" ──") + "\n\n")
			}

			activeTag := ""
			if m.ID == activeID {
				activeTag = "  [ACTIVE MODEL]"
			}

			sb.WriteString(fmt.Sprintf("%d. %s [%s]%s  (%s)\n   Status: %s\n   Use Case: %s\n\n",
				i+1, m.ID, m.RenderBadge(), activeTag, m.Status, m.Status, m.Description))
		}

		sb.WriteString("\nSwitch model: mg models select <id> or launch interactive menu: mg models")
		fmt.Println(boxStyle.Render(sb.String()))
	},
}

var modelsSelectCmd = &cobra.Command{
	Use:   "select [model-id]",
	Short: "Select active model by ID",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		targetID := args[0]
		scanner := models.NewScanner()
		catalog, err := scanner.Scan(context.Background())
		if err != nil {
			fmt.Printf("%s\n", ui.PrintError(fmt.Errorf("Failed to scan models: %v", err)))
			os.Exit(1)
		}

		m, found := models.GetModelByID(catalog, targetID)
		if !found {
			fmt.Printf("%s\n", ui.PrintError(fmt.Errorf("Unknown model ID '%s'. Run 'mg models list' to view valid IDs.", targetID)))
			os.Exit(1)
		}

		if err := config.SetActiveModel(targetID); err != nil {
			fmt.Printf("%s\n", ui.PrintError(fmt.Errorf("Failed to save active model preference: %v", err)))
			os.Exit(1)
		}

		ui.PrintStepSuccess(fmt.Sprintf("Successfully selected active model: %s (%s)", m.ID, m.Name))
	},
}

var modelsCurrentCmd = &cobra.Command{
	Use:   "current",
	Short: "Show currently active model",
	Run: func(cmd *cobra.Command, args []string) {
		activeID := config.GetActiveModel()
		scanner := models.NewScanner()
		catalog, _ := scanner.Scan(context.Background())
		m, found := models.GetModelByID(catalog, activeID)

		if found {
			ui.PrintStepInfo(fmt.Sprintf("Active Model: %s [%s] (%s)", m.ID, m.RenderBadge(), m.Description))
		} else {
			ui.PrintStepInfo(fmt.Sprintf("Active Model: %s", activeID))
		}
	},
}

func init() {
	modelsCmd.AddCommand(modelsListCmd)
	modelsCmd.AddCommand(modelsSelectCmd)
	modelsCmd.AddCommand(modelsCurrentCmd)
	rootCmd.AddCommand(modelsCmd)
}

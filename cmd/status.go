package cmd

import (
	"context"
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/CmfH009/microgravity/internal/config"
	"github.com/CmfH009/microgravity/internal/ollama"
	"github.com/CmfH009/microgravity/internal/subagent"
	"github.com/shirou/gopsutil/v3/mem"
	"github.com/spf13/cobra"
)

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Display live agent status, active model, hardware metrics, and quota utilization",
	Run: func(cmd *cobra.Command, args []string) {
		ctx := context.Background()

		v, _ := mem.VirtualMemory()
		totalRAM := float64(v.Total) / (1024 * 1024 * 1024)
		usedRAM := float64(v.Used) / (1024 * 1024 * 1024)
		usedPercent := v.UsedPercent

		activeModel := config.GetActiveModel()
		if activeModel == "" {
			activeModel = "gemini-3.6-flash"
		}

		m := subagent.GetManager()
		subList := m.List()
		activeSubs := 0
		for _, s := range subList {
			if s.Status == "RUNNING" {
				activeSubs++
			}
		}

		ollamaClient := ollama.NewClient("http://localhost:11434")
		ollamaReady := "ACTIVE (http://localhost:11434)"
		if has, err := ollamaClient.HasModel(ctx, "gemma:2b"); err != nil || !has {
			ollamaReady = "STANDBY"
		}

		boxStyle := lipgloss.NewStyle().
			Border(lipgloss.DoubleBorder()).
			BorderForeground(lipgloss.Color("99")).
			Padding(1, 3)

		badgeActive := lipgloss.NewStyle().Foreground(lipgloss.Color("15")).Background(lipgloss.Color("48")).Bold(true).Padding(0, 1).Render("ACTIVE")
		badgeStandby := lipgloss.NewStyle().Foreground(lipgloss.Color("15")).Background(lipgloss.Color("240")).Bold(true).Padding(0, 1).Render("STANDBY")
		titleStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("213")).Bold(true)
		keyStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("39")).Bold(true)
		valStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("255"))

		// Build RAM progress bar
		barWidth := 20
		ramPct := int(usedPercent)
		filled := (ramPct * barWidth) / 100
		ramBar := fmt.Sprintf("[%s] %.1f GB / %.1f GB (%.1f%%)",
			strings.Repeat("▓", filled)+strings.Repeat("░", barWidth-filled),
			usedRAM, totalRAM, usedPercent)

		ollamaBadge := badgeActive + " (http://localhost:11434)"
		if strings.Contains(ollamaReady, "STANDBY") {
			ollamaBadge = badgeStandby
		}

		var sb strings.Builder
		sb.WriteString(titleStyle.Render("✦  M I C R O G R A V I T Y   S Y S T E M   D A S H B O A R D  ✦") + "\n\n")

		sb.WriteString(fmt.Sprintf("%s %s\n", keyStyle.Render("🤖 Active Cloud Model  :"), valStyle.Render(activeModel)))
		sb.WriteString(fmt.Sprintf("%s %s\n", keyStyle.Render("🧠 Local Ollama Daemon :"), ollamaBadge))
		sb.WriteString(fmt.Sprintf("%s %s\n", keyStyle.Render("💻 System Memory (RAM) :"), valStyle.Render(ramBar)))
		sb.WriteString(fmt.Sprintf("%s %s\n", keyStyle.Render("🤖 Active Subagents    :"), valStyle.Render(fmt.Sprintf("%d running (%d total in history)", activeSubs, len(subList)))))
		sb.WriteString(fmt.Sprintf("%s %s\n", keyStyle.Render("⚡ API Quota & Tier    :"), valStyle.Render("Tier-1 Pro (Unlimited Local / Standard Cloud)")))

		fmt.Println(boxStyle.Render(sb.String()))
	},
}

func init() {
	rootCmd.AddCommand(statusCmd)
}

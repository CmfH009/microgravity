package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// Colors based on project guidelines
var (
	ColorPrimary   = lipgloss.Color("39")  // cyan
	ColorSuccess   = lipgloss.Color("48")  // green/mint
	ColorAccent    = lipgloss.Color("99")  // purple/violet
	ColorBrand     = lipgloss.Color("213") // pink/magenta
	ColorHighlight = lipgloss.Color("214") // orange/gold
	ColorSeparator = lipgloss.Color("63")  // indigo
	ColorSystem    = lipgloss.Color("240") // gray
	ColorError     = lipgloss.Color("196") // red
)

// PrintBootBanner prints a space/gravity themed ASCII art banner for Microgravity
func PrintBootBanner() {
	banner := `
   __  ____                                        _ __       
  /  |/  (_)_____________  ____ __________ __   __(_) /___  __
 / /|_/ / / ___/ ___/ __ \/ __ ` + "`" + `/ ___/ __ ` + "`" + `/ | / / / __/ / / /
/ /  / / / /__/ /  / /_/ / /_/ / /  / /_/ /| |/ / / /_/ /_/ / 
/_/  /_/_/\___/_/   \____/\__, /_/   \__,_/ |___/_/\__/\__, /  
                          /____/                       /____/  `

	lines := strings.Split(banner, "\n")
	
	colors := []lipgloss.Color{
		lipgloss.Color("39"),  // cyan
		lipgloss.Color("45"),
		lipgloss.Color("63"),  // purple
		lipgloss.Color("99"),
		lipgloss.Color("141"),
		lipgloss.Color("207"),
		lipgloss.Color("213"), // pink
		lipgloss.Color("39"),
	}

	for i, line := range lines {
		if line == "" {
			continue
		}
		colorIdx := i % len(colors)
		fmt.Println(lipgloss.NewStyle().Foreground(colors[colorIdx]).Bold(true).Render(line))
	}

	// Subtitle
	subtitle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("15")).
		Background(ColorAccent).
		Bold(true).
		Padding(0, 2).
		Render("🌌  M I C R O G R A V I T Y   A I   E N V I R O N M E N T  ✦  v2.5")

	tagline := lipgloss.NewStyle().
		Foreground(ColorPrimary).
		Italic(true).
		Render("  Autonomous Copilot Engine • Multimodal Gemma & Gemini Matrix")

	fmt.Println()
	fmt.Printf("  %s\n%s\n", subtitle, tagline)
	fmt.Println()
}

// PrintStatusSummary renders Option 1 clean dashboard summary
func PrintStatusSummary(activeModel string, mcpCount int, skillCount int, ollamaModel string) {
	bullet := lipgloss.NewStyle().Foreground(ColorPrimary).Render("●")
	check := lipgloss.NewStyle().Foreground(ColorSuccess).Render("✔")
	labelStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("252")).Bold(true)
	valStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("255"))
	mutedStyle := lipgloss.NewStyle().Foreground(ColorSystem).Italic(true)

	extText := "No MCP servers configured"
	if mcpCount > 0 {
		extText = fmt.Sprintf("%d MCP server(s) connected", mcpCount)
	}

	localText := "Ollama daemon active"
	if ollamaModel != "" {
		localText = fmt.Sprintf("Ollama daemon active (%s ready)", ollamaModel)
	}

	fmt.Printf("  %s %s : %s\n", bullet, labelStyle.Render("Active Model"), valStyle.Render(activeModel))
	fmt.Printf("  %s %s : %s\n", bullet, labelStyle.Render("Extensions  "), valStyle.Render(extText))
	fmt.Printf("  %s %s : %s\n", bullet, labelStyle.Render("Skills      "), valStyle.Render(fmt.Sprintf("%d loaded", skillCount)))
	fmt.Printf("  %s %s : %s\n", bullet, labelStyle.Render("Local Engine"), valStyle.Render(localText))
	fmt.Printf("  %s %s\n\n", check, mutedStyle.Render("Background model scanner active"))

	welcome := lipgloss.NewStyle().Foreground(ColorBrand).Bold(true).Render("Welcome to Microgravity CLI Chat!")
	help := mutedStyle.Render("Type your prompt or /help to start.")
	fmt.Printf("  %s %s\n\n", welcome, help)
}

// PrintPhaseHeader renders a phase transition header with an animated-feel box
func PrintPhaseHeader(phase string, icon string) {
	style := lipgloss.NewStyle().
		Border(lipgloss.DoubleBorder()).
		BorderForeground(ColorAccent).
		Foreground(ColorPink).
		Padding(0, 2).
		Bold(true)

	content := fmt.Sprintf("%s  PHASE: %s", icon, strings.ToUpper(phase))
	fmt.Println(style.Render(content))
}

// PrintStepComplete prints a completed step with a checkmark badge
func PrintStepComplete(step string) {
	icon := lipgloss.NewStyle().Foreground(ColorSuccess).Bold(true).Render("✔")
	tag := lipgloss.NewStyle().Foreground(ColorSuccess).Render(step)
	fmt.Printf("  %s %s\n", icon, tag)
}

// PrintStepSuccess is an alias for PrintStepComplete
func PrintStepSuccess(step string) {
	PrintStepComplete(step)
}

// PrintStepInfo prints an informational step
func PrintStepInfo(step string) {
	icon := lipgloss.NewStyle().Foreground(ColorPrimary).Bold(true).Render("ℹ")
	tag := lipgloss.NewStyle().Foreground(ColorPrimary).Render(step)
	fmt.Printf("  %s %s\n", icon, tag)
}

// PrintStepSkipped prints a skipped step
func PrintStepSkipped(step string) {
	icon := lipgloss.NewStyle().Foreground(ColorSystem).Render("○")
	text := lipgloss.NewStyle().Foreground(ColorSystem).Italic(true).Render(step)
	fmt.Printf("  %s %s\n", icon, text)
}

// PrintStepFailed prints a failed step
func PrintStepFailed(step string) {
	icon := lipgloss.NewStyle().Foreground(ColorError).Bold(true).Render("✖")
	text := lipgloss.NewStyle().Foreground(ColorError).Bold(true).Render(step)
	fmt.Printf("  %s %s\n", icon, text)
}

// PrintSkillsLoaded prints how many skills were loaded
func PrintSkillsLoaded(count int) {
	badge := lipgloss.NewStyle().
		Foreground(lipgloss.Color("15")).
		Background(ColorHighlight).
		Bold(true).
		Padding(0, 1).
		Render(fmt.Sprintf("⚡ SKILLS LOADED: %d", count))
	fmt.Printf("  %s\n", badge)
}

// PrintModelInfo prints model info card
func PrintModelInfo(model string, size string) {
	boxStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(ColorAccent).
		Padding(0, 2).
		MarginTop(0).
		MarginBottom(1)

	modelTitle := lipgloss.NewStyle().Foreground(ColorBrand).Bold(true).Render(model)
	sizeTag := lipgloss.NewStyle().Foreground(ColorSystem).Render(fmt.Sprintf("(%s)", size))

	content := fmt.Sprintf("🤖 Active LLM Endpoint: %s %s", modelTitle, sizeTag)
	fmt.Println(boxStyle.Render(content))
}

// PrintGoodbye prints a farewell message
func PrintGoodbye() {
	style := lipgloss.NewStyle().
		Foreground(ColorPrimary).
		Bold(true).
		Italic(true).
		MarginTop(1)
		
	fmt.Println(style.Render("✦ End of line. Safe travels in Microgravity 🌌"))
}


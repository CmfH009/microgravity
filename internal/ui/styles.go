package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

var (
	// Palettes & Brand Colors — sourced from active theme
	ColorCyan    = RoleColor(RolePrimary)   // Electric Cyan
	ColorMint    = RoleColor(RoleSuccess)   // Emerald Mint / Green
	ColorPurple  = RoleColor(RoleAccent)    // Deep Violet / Purple
	ColorPink    = RoleColor(RoleBrand)     // Hot Pink / Magenta
	ColorGold    = RoleColor(RoleHighlight) // Glowing Gold / Amber
	ColorCrimson = RoleColor(RoleError)     // Error Red
	ColorSlate   = RoleColor(RoleSystem)    // Dark Slate Gray
	ColorMuted   = RoleColor(RoleTextMuted) // Light Muted Gray

	// Chat pill badges
	AgentPrefixStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("15")).
				Background(lipgloss.Color("39")).
				Bold(true).
				Padding(0, 1)

	UserPrefixStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("15")).
				Background(lipgloss.Color("48")).
				Bold(true).
				Padding(0, 1)

	SystemInfoStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("147")).
				Italic(true)

	HeaderStyle = lipgloss.NewStyle().
			Foreground(ColorPink).
			Bold(true).
			Border(lipgloss.DoubleBorder()).
			BorderForeground(ColorPurple).
			Padding(0, 2)

	SeparatorStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("63"))

	// Tool styles
	ToolTitleStyle  = lipgloss.NewStyle().Foreground(ColorGold).Bold(true)
	ToolOutputStyle = lipgloss.NewStyle().Foreground(ColorMuted).Italic(true)

	// Error style
	ErrorStyle = lipgloss.NewStyle().
			Foreground(ColorCrimson).
			Bold(true).
			Border(lipgloss.RoundedBorder()).
			BorderForeground(ColorCrimson).
			Padding(0, 1)

	// Main Menu styles
	MenuBoxStyle = lipgloss.NewStyle().
			Border(lipgloss.DoubleBorder()).
			BorderForeground(ColorPurple).
			Padding(1, 4).
			MarginTop(1).
			MarginBottom(1)

	MenuTitleStyle = lipgloss.NewStyle().
			Foreground(ColorPink).
			Bold(true)

	MenuItemStyle = lipgloss.NewStyle().
			Foreground(RoleColor(RoleText))

	MenuPromptStyle = lipgloss.NewStyle().
			Foreground(ColorCyan).
			Bold(true)
)

// refreshThemeStyles synchronizes exported color and style variables with CurrentTheme.
func refreshThemeStyles() {
	ColorCyan = RoleColor(RolePrimary)
	ColorMint = RoleColor(RoleSuccess)
	ColorPurple = RoleColor(RoleAccent)
	ColorPink = RoleColor(RoleBrand)
	ColorGold = RoleColor(RoleHighlight)
	ColorCrimson = RoleColor(RoleError)
	ColorSlate = RoleColor(RoleSystem)
	ColorMuted = RoleColor(RoleTextMuted)

	AgentPrefixStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color("15")).
		Background(ColorCyan).
		Bold(true).
		Padding(0, 1)

	UserPrefixStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color("15")).
		Background(ColorMint).
		Bold(true).
		Padding(0, 1)

	SystemInfoStyle = lipgloss.NewStyle().
		Foreground(ColorMuted).
		Italic(true)

	HeaderStyle = lipgloss.NewStyle().
		Foreground(ColorPink).
		Bold(true).
		Border(lipgloss.DoubleBorder()).
		BorderForeground(ColorPurple).
		Padding(0, 2)

	SeparatorStyle = lipgloss.NewStyle().Foreground(RoleColor(RoleSeparator))

	ToolTitleStyle = lipgloss.NewStyle().Foreground(ColorGold).Bold(true)
	ToolOutputStyle = lipgloss.NewStyle().Foreground(ColorMuted).Italic(true)

	ErrorStyle = lipgloss.NewStyle().
		Foreground(ColorCrimson).
		Bold(true).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(ColorCrimson).
		Padding(0, 1)

	MenuBoxStyle = lipgloss.NewStyle().
		Border(lipgloss.DoubleBorder()).
		BorderForeground(ColorPurple).
		Padding(1, 4).
		MarginTop(1).
		MarginBottom(1)

	MenuTitleStyle = lipgloss.NewStyle().
		Foreground(ColorPink).
		Bold(true)

	MenuItemStyle = lipgloss.NewStyle().
		Foreground(RoleColor(RoleText))

	MenuPromptStyle = lipgloss.NewStyle().
		Foreground(ColorCyan).
		Bold(true)

	refreshToolCardStyles()
}

func PrintHeader(text string) string {
	style := lipgloss.NewStyle().
		Foreground(RoleColor(RoleBrand)).
		Bold(true).
		Border(lipgloss.DoubleBorder()).
		BorderForeground(RoleColor(RoleAccent)).
		Padding(0, 2)
	return style.Render(text)
}

func PrintSeparator() string {
	return lipgloss.NewStyle().
		Foreground(RoleColor(RoleSeparator)).
		Render("━━━━━━━ ✦ ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━ ✦ ━━━━━━━")
}

func PrintAgentPrefix() string {
	style := lipgloss.NewStyle().
		Foreground(lipgloss.Color("15")).
		Background(RoleColor(RolePrimary)).
		Bold(true).
		Padding(0, 1)
	return style.Render("🤖 MICROGRAVITY") + " "
}

func PrintUserPrefix() string {
	style := lipgloss.NewStyle().
		Foreground(lipgloss.Color("15")).
		Background(RoleColor(RoleSuccess)).
		Bold(true).
		Padding(0, 1)
	return style.Render("👤 YOU") + " "
}

func PrintSystemInfo(text string) string {
	badge := lipgloss.NewStyle().
		Foreground(lipgloss.Color("15")).
		Background(RoleColor(RoleSeparator)).
		Bold(true).
		Padding(0, 1).
		Render("ℹ SYS")
	infoStyle := lipgloss.NewStyle().
		Foreground(RoleColor(RoleTextMuted)).
		Italic(true)
	return fmt.Sprintf("%s %s", badge, infoStyle.Render(text))
}

func PrintToolTitle(text string) string {
	return lipgloss.NewStyle().Foreground(RoleColor(RoleHighlight)).Bold(true).Render("⚙️  " + text)
}

func PrintError(err error) string {
	errColor := RoleColor(RoleError)
	style := lipgloss.NewStyle().
		Foreground(errColor).
		Bold(true).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(errColor).
		Padding(0, 1)
	return style.Render("✖  ERROR: " + err.Error())
}

func RenderMainMenu() string {
	title := lipgloss.NewStyle().
		Foreground(RoleColor(RoleBrand)).
		Bold(true).
		Render("🌌  M I C R O G R A V I T Y   A I   E N V I R O N M E N T\n ✦  Autonomous Agentic Copilot & Execution Engine  ✦")

	items := []string{
		" 1 💬  Initialize Interactive Agent Chat Loop",
		" 2 🚀  Launch Copilot (IDE Code Completion Engine)",
		" 3 📦  Install Agent Skill (Download from Registry/GitHub)",
		" 4 🔑  Configure API Core Keys & Provider Credentials",
		" 5 ⚙️   Bootstrap Environment & Local Model Services",
		" 6 ❌  Disconnect Session",
	}

	itemStyle := lipgloss.NewStyle().Foreground(RoleColor(RoleText))
	var menuText string
	for i, item := range items {
		menuText += itemStyle.Render(item)
		if i < len(items)-1 {
			menuText += "\n"
		}
	}

	boxStyle := lipgloss.NewStyle().
		Border(lipgloss.DoubleBorder()).
		BorderForeground(RoleColor(RoleAccent)).
		Padding(1, 4).
		MarginTop(1).
		MarginBottom(1)

	content := lipgloss.JoinVertical(lipgloss.Center, title, strings.Repeat("─", 50), menuText)
	return boxStyle.Render(content)
}

func PrintMenuPrompt() string {
	return lipgloss.NewStyle().
		Foreground(RoleColor(RolePrimary)).
		Bold(true).
		Render("\n❯ Awaiting directive (1-6): ")
}

// RenderStatusLine formats a rich status bar line displaying active model, workspace, skills, mcp, and date/time.
func RenderStatusLine(modelName string, activeSkills int, mcpCount int, cwd string) string {
	modelPill := lipgloss.NewStyle().Foreground(lipgloss.Color("15")).Background(RoleColor(RolePrimary)).Bold(true).Padding(0, 1).Render(fmt.Sprintf("MODEL: %s", modelName))
	skillsPill := lipgloss.NewStyle().Foreground(lipgloss.Color("15")).Background(RoleColor(RoleAccent)).Bold(true).Padding(0, 1).Render(fmt.Sprintf("SKILLS: %d", activeSkills))
	mcpPill := lipgloss.NewStyle().Foreground(lipgloss.Color("15")).Background(RoleColor(RoleSuccess)).Bold(true).Padding(0, 1).Render(fmt.Sprintf("MCP: %d", mcpCount))
	dirPill := lipgloss.NewStyle().Foreground(RoleColor(RoleTextMuted)).Italic(true).Render(fmt.Sprintf("DIR: %s", cwd))

	return fmt.Sprintf("%s %s %s | %s", modelPill, skillsPill, mcpPill, dirPill)
}



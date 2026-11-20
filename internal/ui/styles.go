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
			Foreground(lipgloss.Color("255"))

	MenuPromptStyle = lipgloss.NewStyle().
			Foreground(ColorCyan).
			Bold(true)
)

func PrintHeader(text string) string {
	return HeaderStyle.Render(text)
}

func PrintSeparator() string {
	return SeparatorStyle.Render("━━━━━━━ ✦ ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━ ✦ ━━━━━━━")
}

func PrintAgentPrefix() string {
	return AgentPrefixStyle.Render("🤖 MICROGRAVITY") + " "
}

func PrintUserPrefix() string {
	return UserPrefixStyle.Render("👤 YOU") + " "
}

func PrintSystemInfo(text string) string {
	badge := lipgloss.NewStyle().
		Foreground(lipgloss.Color("15")).
		Background(lipgloss.Color("63")).
		Bold(true).
		Padding(0, 1).
		Render("ℹ SYS")
	return fmt.Sprintf("%s %s", badge, SystemInfoStyle.Render(text))
}

func PrintToolTitle(text string) string {
	return ToolTitleStyle.Render("⚙️  " + text)
}

func PrintError(err error) string {
	return ErrorStyle.Render("✖  ERROR: " + err.Error())
}

func RenderMainMenu() string {
	title := MenuTitleStyle.Render("🌌  M I C R O G R A V I T Y   A I   E N V I R O N M E N T\n ✦  Autonomous Agentic Copilot & Execution Engine  ✦")

	items := []string{
		" 1 💬  Initialize Interactive Agent Chat Loop",
		" 2 🚀  Launch Copilot (IDE Code Completion Engine)",
		" 3 📦  Install Agent Skill (Download from Registry/GitHub)",
		" 4 🔑  Configure API Core Keys & Provider Credentials",
		" 5 ⚙️   Bootstrap Environment & Local Model Services",
		" 6 ❌  Disconnect Session",
	}

	var menuText string
	for i, item := range items {
		menuText += MenuItemStyle.Render(item)
		if i < len(items)-1 {
			menuText += "\n"
		}
	}

	content := lipgloss.JoinVertical(lipgloss.Center, title, strings.Repeat("─", 50), menuText)
	return MenuBoxStyle.Render(content)
}

func PrintMenuPrompt() string {
	return MenuPromptStyle.Render("\n❯ Awaiting directive (1-6): ")
}

// RenderStatusLine formats a rich status bar line displaying active model, workspace, skills, mcp, and date/time.
func RenderStatusLine(modelName string, activeSkills int, mcpCount int, cwd string) string {
	modelPill := lipgloss.NewStyle().Foreground(lipgloss.Color("15")).Background(ColorCyan).Bold(true).Padding(0, 1).Render(fmt.Sprintf("MODEL: %s", modelName))
	skillsPill := lipgloss.NewStyle().Foreground(lipgloss.Color("15")).Background(ColorPurple).Bold(true).Padding(0, 1).Render(fmt.Sprintf("SKILLS: %d", activeSkills))
	mcpPill := lipgloss.NewStyle().Foreground(lipgloss.Color("15")).Background(ColorMint).Bold(true).Padding(0, 1).Render(fmt.Sprintf("MCP: %d", mcpCount))
	dirPill := lipgloss.NewStyle().Foreground(ColorMuted).Italic(true).Render(fmt.Sprintf("DIR: %s", cwd))

	return fmt.Sprintf("%s %s %s | %s", modelPill, skillsPill, mcpPill, dirPill)
}



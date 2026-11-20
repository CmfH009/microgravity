package ui

import (
	"github.com/charmbracelet/lipgloss"
)

// ThemeRole is a named color role in the Microgravity theme.
type ThemeRole string

const (
	RolePrimary   ThemeRole = "primary"   // cyan - main actions, primary text
	RoleSuccess   ThemeRole = "success"   // mint/green - success states
	RoleAccent    ThemeRole = "accent"    // purple/violet - secondary emphasis
	RoleBrand     ThemeRole = "brand"     // pink/magenta - brand identity
	RoleHighlight ThemeRole = "highlight" // orange/gold - warnings, highlights
	RoleSeparator ThemeRole = "separator" // indigo - dividers
	RoleSystem    ThemeRole = "system"    // gray - muted/system text
	RoleError     ThemeRole = "error"     // red - errors, failures
	RoleText      ThemeRole = "text"      // white - primary text
	RoleTextMuted ThemeRole = "text_muted" // light gray - secondary text
)

// Theme defines the complete Microgravity visual color system.
// Both dark and light variants share the same 256-color ANSI indices for
// terminal compatibility, but light theme swaps foreground/background roles.
type Theme struct {
	Name        string
	Primary     lipgloss.Color
	Success     lipgloss.Color
	Accent      lipgloss.Color
	Brand       lipgloss.Color
	Highlight   lipgloss.Color
	Separator   lipgloss.Color
	System      lipgloss.Color
	Error       lipgloss.Color
	Text        lipgloss.Color
	TextMuted   lipgloss.Color
	Background  lipgloss.Color
	Surface     lipgloss.Color
	Border      lipgloss.Color
}

// DarkTheme is the default space-themed palette: deep backgrounds with
// electric cyan, emerald, violet, and pink accents.
var DarkTheme = Theme{
	Name: "dark",
	Primary:     lipgloss.Color("39"),  // electric cyan
	Success:     lipgloss.Color("48"),  // emerald mint
	Accent:      lipgloss.Color("99"),  // deep violet
	Brand:       lipgloss.Color("213"), // hot pink
	Highlight:   lipgloss.Color("214"), // glowing gold
	Separator:   lipgloss.Color("63"),  // indigo
	System:      lipgloss.Color("240"), // dark slate
	Error:       lipgloss.Color("196"), // error red
	Text:        lipgloss.Color("15"),  // white
	TextMuted:   lipgloss.Color("246"), // light muted gray
	Background:  lipgloss.Color("235"), // near-black
	Surface:     lipgloss.Color("237"), // slightly lighter than bg
	Border:      lipgloss.Color("239"), // subtle border
}

// LightTheme is a high-contrast daylight variant for bright terminals.
var LightTheme = Theme{
	Name: "light",
	Primary:     lipgloss.Color("39"),  // electric cyan
	Success:     lipgloss.Color("28"),  // forest green
	Accent:      lipgloss.Color("93"),  // medium purple
	Brand:       lipgloss.Color("161"), // deep pink
	Highlight:   lipgloss.Color("166"), // orange
	Separator:   lipgloss.Color("60"),  // indigo
	System:      lipgloss.Color("244"), // light gray
	Error:       lipgloss.Color("124"), // brick red
	Text:        lipgloss.Color("234"), // near-black text
	TextMuted:   lipgloss.Color("245"), // light muted
	Background:  lipgloss.Color("255"), // white
	Surface:     lipgloss.Color("252"), // off-white
	Border:      lipgloss.Color("247"), // subtle border
}

// CurrentTheme is the active theme. Defaults to DarkTheme.
var CurrentTheme = DarkTheme

// SetTheme switches the active theme at runtime.
func SetTheme(name string) {
	switch name {
	case "light":
		CurrentTheme = LightTheme
	case "dark":
		CurrentTheme = DarkTheme
	}
}

// RoleColor returns the lipgloss.Color for a given theme role in the active theme.
func RoleColor(role ThemeRole) lipgloss.Color {
	switch role {
	case RolePrimary:
		return CurrentTheme.Primary
	case RoleSuccess:
		return CurrentTheme.Success
	case RoleAccent:
		return CurrentTheme.Accent
	case RoleBrand:
		return CurrentTheme.Brand
	case RoleHighlight:
		return CurrentTheme.Highlight
	case RoleSeparator:
		return CurrentTheme.Separator
	case RoleSystem:
		return CurrentTheme.System
	case RoleError:
		return CurrentTheme.Error
	case RoleText:
		return CurrentTheme.Text
	case RoleTextMuted:
		return CurrentTheme.TextMuted
	default:
		return CurrentTheme.Primary
	}
}

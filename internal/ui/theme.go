package ui

import (
	"strings"
	"sync"

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

// TokyoNightTheme is a moody indigo-and-neon palette inspired by Tokyo Night.
var TokyoNightTheme = Theme{
	Name:       "tokyo-night",
	Primary:    lipgloss.Color("#7aa2f7"), // Tokyo Night blue
	Success:    lipgloss.Color("#9ece6a"), // Tokyo Night green
	Accent:     lipgloss.Color("#bb9af7"), // Tokyo Night purple
	Brand:      lipgloss.Color("#7dcfff"), // Tokyo Night cyan
	Highlight:  lipgloss.Color("#e0af68"), // Tokyo Night amber
	Separator:  lipgloss.Color("#565f89"), // Tokyo Night slate
	System:     lipgloss.Color("#414868"), // Tokyo Night dark slate
	Error:      lipgloss.Color("#f7768e"), // Tokyo Night rose red
	Text:       lipgloss.Color("#c0caf5"), // Tokyo Night foreground
	TextMuted:  lipgloss.Color("#a9b1d6"), // Tokyo Night secondary text
	Background: lipgloss.Color("#1a1b26"), // Tokyo Night background
	Surface:    lipgloss.Color("#24283b"), // Tokyo Night surface
	Border:     lipgloss.Color("#3b4261"), // Tokyo Night border
}

// CatppuccinTheme is a warm pastel dark palette inspired by Catppuccin Mocha.
var CatppuccinTheme = Theme{
	Name:       "catppuccin",
	Primary:    lipgloss.Color("#89b4fa"), // Mocha blue
	Success:    lipgloss.Color("#a6e3a1"), // Mocha green
	Accent:     lipgloss.Color("#cba6f7"), // Mocha mauve
	Brand:      lipgloss.Color("#f5c2e7"), // Mocha pink
	Highlight:  lipgloss.Color("#f9e2af"), // Mocha yellow
	Separator:  lipgloss.Color("#6c7086"), // Mocha overlay0
	System:     lipgloss.Color("#585b70"), // Mocha surface2
	Error:      lipgloss.Color("#f38ba8"), // Mocha red
	Text:       lipgloss.Color("#cdd6f4"), // Mocha text
	TextMuted:  lipgloss.Color("#a6adc8"), // Mocha subtext0
	Background: lipgloss.Color("#1e1e2e"), // Mocha base
	Surface:    lipgloss.Color("#313244"), // Mocha surface0
	Border:     lipgloss.Color("#45475a"), // Mocha surface1
}

// HighContrastTheme provides maximum luminance contrast for accessibility.
var HighContrastTheme = Theme{
	Name:       "high-contrast",
	Primary:    lipgloss.Color("51"),  // vivid cyan
	Success:    lipgloss.Color("46"),  // vivid green
	Accent:     lipgloss.Color("201"), // vivid magenta
	Brand:      lipgloss.Color("213"), // bright pink
	Highlight:  lipgloss.Color("226"), // vivid yellow
	Separator:  lipgloss.Color("15"),  // crisp white
	System:     lipgloss.Color("250"), // bright gray
	Error:      lipgloss.Color("196"), // vivid red
	Text:       lipgloss.Color("15"),  // pure white
	TextMuted:  lipgloss.Color("252"), // bright muted gray
	Background: lipgloss.Color("0"),   // pure black
	Surface:    lipgloss.Color("234"), // near-black surface
	Border:     lipgloss.Color("15"),  // crisp white border
}

var themeMu sync.RWMutex

// CurrentTheme is the active theme. Defaults to DarkTheme.
var CurrentTheme = DarkTheme

// ThemeNames returns all built-in concrete theme names in display order.
func ThemeNames() []string {
	return []string{"dark", "light", "tokyo-night", "catppuccin", "high-contrast"}
}

// AutoDetectTheme selects "dark" or "light" based on terminal background luminance.
func AutoDetectTheme() string {
	if lipgloss.HasDarkBackground() {
		return "dark"
	}
	return "light"
}

// ActiveThemeName returns the active theme name thread-safely.
func ActiveThemeName() string {
	themeMu.RLock()
	defer themeMu.RUnlock()
	if CurrentTheme.Name == "" {
		return "dark"
	}
	return CurrentTheme.Name
}

// SetTheme switches the active theme at runtime.
func SetTheme(name string) {
	normalized := strings.ToLower(strings.TrimSpace(name))
	if normalized == "auto" {
		normalized = AutoDetectTheme()
	}

	var next Theme
	switch normalized {
	case "light":
		next = LightTheme
	case "dark":
		next = DarkTheme
	case "tokyo-night", "tokyonight":
		next = TokyoNightTheme
	case "catppuccin", "catppuccin-mocha":
		next = CatppuccinTheme
	case "high-contrast", "highcontrast":
		next = HighContrastTheme
	default:
		return
	}

	themeMu.Lock()
	CurrentTheme = next
	themeMu.Unlock()

	refreshThemeStyles()
}

// RoleColor returns the lipgloss.Color for a given theme role in the active theme.
func RoleColor(role ThemeRole) lipgloss.Color {
	themeMu.RLock()
	t := CurrentTheme
	themeMu.RUnlock()

	switch role {
	case RolePrimary:
		return t.Primary
	case RoleSuccess:
		return t.Success
	case RoleAccent:
		return t.Accent
	case RoleBrand:
		return t.Brand
	case RoleHighlight:
		return t.Highlight
	case RoleSeparator:
		return t.Separator
	case RoleSystem:
		return t.System
	case RoleError:
		return t.Error
	case RoleText:
		return t.Text
	case RoleTextMuted:
		return t.TextMuted
	default:
		return t.Primary
	}
}

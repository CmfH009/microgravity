package ui

import (
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"
	"golang.org/x/term"
)

type ArtifactRenderer struct {
	renderer *glamour.TermRenderer
	isTTY    bool
}

func NewArtifactRenderer() (*ArtifactRenderer, error) {
	fd := int(os.Stdout.Fd())
	isTTY := term.IsTerminal(fd)

	width := 80
	if isTTY {
		w, _, err := term.GetSize(fd)
		if err == nil && w > 0 {
			width = w
		}
	}

	renderer, err := glamour.NewTermRenderer(
		glamour.WithAutoStyle(),
		glamour.WithWordWrap(width),
	)
	if err != nil {
		return nil, err
	}
	return &ArtifactRenderer{
		renderer: renderer,
		isTTY:    isTTY,
	}, nil
}

func (r *ArtifactRenderer) Render(markdown string) (string, error) {
	// First pass: LaTeX conversion
	markdown = RenderLaTeX(markdown)

	// Second pass: Alert Callouts conversion
	markdown = RenderAlerts(markdown)

	if !r.isTTY {
		return markdown + "\n", nil
	}
	return r.renderer.Render(markdown)
}

var alertRegex = regexp.MustCompile(`(?m)^>\s*\[\!(NOTE|TIP|IMPORTANT|WARNING|CAUTION)\]\s*\n((?:^>.*$\n?)*)`)

// RenderAlerts transforms markdown alert blocks into styled visual callouts.
func RenderAlerts(text string) string {
	return alertRegex.ReplaceAllStringFunc(text, func(match string) string {
		submatches := alertRegex.FindStringSubmatch(match)
		if len(submatches) < 3 {
			return match
		}
		alertType := strings.ToUpper(submatches[1])
		rawBody := submatches[2]

		var bodyLines []string
		for _, line := range strings.Split(rawBody, "\n") {
			trimmed := strings.TrimPrefix(strings.TrimSpace(line), ">")
			if trimmed != "" {
				bodyLines = append(bodyLines, strings.TrimSpace(trimmed))
			}
		}
		body := strings.Join(bodyLines, " ")

		var color lipgloss.Color
		var icon string
		switch alertType {
		case "NOTE":
			color = ColorCyan
			icon = "ℹ NOTE"
		case "TIP":
			color = ColorMint
			icon = "💡 TIP"
		case "IMPORTANT":
			color = ColorPurple
			icon = "❗ IMPORTANT"
		case "WARNING":
			color = ColorGold
			icon = "⚠️ WARNING"
		case "CAUTION":
			color = ColorCrimson
			icon = "🛑 CAUTION"
		default:
			color = ColorSlate
			icon = alertType
		}

		style := lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(color).
			Padding(0, 1).
			Foreground(lipgloss.Color("255"))

		titleStyle := lipgloss.NewStyle().Foreground(color).Bold(true)
		return style.Render(fmt.Sprintf("%s: %s", titleStyle.Render(icon), body)) + "\n"
	})
}

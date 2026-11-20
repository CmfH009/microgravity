package ui

import (
	"fmt"
	"math"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// Sparkline renders a mini chart from a series of integer values.
// It uses braille patterns for smooth sub-column resolution.
type Sparkline struct {
	Values  []int
	Width   int
	Color   lipgloss.Color
	ShowMin bool
	ShowMax bool
}

// NewSparkline creates a sparkline from a value series.
func NewSparkline(values []int) *Sparkline {
	return &Sparkline{
		Values: values,
		Width:  40,
		Color:  RoleColor(RolePrimary),
	}
}

// braillePatterns are the 8-dot braille patterns arranged by column.
// Each pattern has 2 dots vertically and 4 horizontally (2x4 grid).
var braillePatterns = []string{
	"⠀", "⠈", "⠐", "⠠", "⢀", "⢈", "⢐", "⢠",
	"⠄", "⠌", "⠔", "⠜", "⢄", "⢌", "⢔", "⢜",
	"⠂", "⠊", "⠘", "⠰", "⢂", "⢊", "⢘", "⢰",
	"⠆", "⠚", "⠞", "⠯", "⢆", "⢚", "⢞", "⢯",
	"⠁", "⠉", "⠑", "⠡", "⢁", "⢉", "⢑", "⢡",
	"⠅", "⠍", "⠙", "⠻", "⢅", "⢍", "⢙", "⢳",
	"⠃", "⠋", "⠍", "⠿", "⢃", "⢋", "⢗", "⢻",
	"⠇", "⠏", "⠛", "⠟", "⢇", "⢏", "⢛", "⢫",
}

// colorRender wraps text in the sparkline's color.
func colorRender(color lipgloss.Color, text string) string {
	return lipgloss.NewStyle().Foreground(color).Render(text)
}

// Render renders the sparkline as a string.
func (s *Sparkline) Render() string {
	if len(s.Values) == 0 {
		return colorRender(s.Color, "⠀⠀⠀")
	}

	values := s.Values
	if s.Width > 0 && len(values) > s.Width {
		// Downsample to fit width
		downsampled := make([]int, s.Width)
		for i := 0; i < s.Width; i++ {
			srcIdx := (i * len(values)) / s.Width
			downsampled[i] = values[srcIdx]
		}
		values = downsampled
	}

	minVal, maxVal := minMax(values)
	if maxVal == minVal {
		maxVal = minVal + 1
	}

	// Map values to braille rows (0-7)
	rows := make([]int, len(values))
	for i, v := range values {
		// Normalize to 0-7 range
		normalized := int(math.Round((float64(v-minVal) / float64(maxVal-minVal)) * 7.0))
		if normalized < 0 {
			normalized = 0
		}
		if normalized > 7 {
			normalized = 7
		}
		rows[i] = normalized
	}

	// Build braille characters: each char uses 2 rows from adjacent columns
	var sb strings.Builder
	for i := 0; i < len(rows); i += 2 {
		row1 := rows[i]
		row2 := 0
		if i+1 < len(rows) {
			row2 = rows[i+1]
		}
		patternIdx := row1 + (row2 * 8)
		if patternIdx >= len(braillePatterns) {
			patternIdx = len(braillePatterns) - 1
		}
		sb.WriteString(colorRender(s.Color, braillePatterns[patternIdx]))
	}

	result := sb.String()

	if s.ShowMin || s.ShowMax {
		result += "  "
		if s.ShowMin {
			result += fmt.Sprintf("min:%d ", minVal)
		}
		if s.ShowMax {
			result += fmt.Sprintf("max:%d", maxVal)
		}
	}

	return result
}

// minMax returns the minimum and maximum values in a slice.
func minMax(values []int) (int, int) {
	if len(values) == 0 {
		return 0, 0
	}
	minVal := values[0]
	maxVal := values[0]
	for _, v := range values[1:] {
		if v < minVal {
			minVal = v
		}
		if v > maxVal {
			maxVal = v
		}
	}
	return minVal, maxVal
}

// SparklineStat renders a sparkline with a label above it.
func SparklineStat(label string, values []int, role ThemeRole) string {
	labelStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(RoleColor(role)).
		PaddingBottom(1)

	sl := NewSparkline(values)
	sl.Color = RoleColor(role)

	return labelStyle.Render(label) + "\n" + sl.Render()
}

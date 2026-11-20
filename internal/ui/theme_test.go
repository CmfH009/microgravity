package ui

import (
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/stretchr/testify/assert"
)

func TestDarkThemeRoles(t *testing.T) {
	assert.Equal(t, lipgloss.Color("39"), RoleColor(RolePrimary))
	assert.Equal(t, lipgloss.Color("48"), RoleColor(RoleSuccess))
	assert.Equal(t, lipgloss.Color("99"), RoleColor(RoleAccent))
	assert.Equal(t, lipgloss.Color("213"), RoleColor(RoleBrand))
	assert.Equal(t, lipgloss.Color("214"), RoleColor(RoleHighlight))
	assert.Equal(t, lipgloss.Color("196"), RoleColor(RoleError))
	assert.Equal(t, lipgloss.Color("15"), RoleColor(RoleText))
	assert.Equal(t, lipgloss.Color("246"), RoleColor(RoleTextMuted))
}

func TestSetThemeLight(t *testing.T) {
	SetTheme("light")
	assert.Equal(t, lipgloss.Color("39"), RoleColor(RolePrimary))
	assert.Equal(t, lipgloss.Color("28"), RoleColor(RoleSuccess))
	assert.Equal(t, lipgloss.Color("234"), RoleColor(RoleText))

	// Reset
	SetTheme("dark")
	assert.Equal(t, lipgloss.Color("48"), RoleColor(RoleSuccess))
	assert.Equal(t, lipgloss.Color("15"), RoleColor(RoleText))
}

func TestSetThemeUnknown(t *testing.T) {
	original := CurrentTheme
	SetTheme("nonexistent")
	assert.Equal(t, original, CurrentTheme)
}

func TestPanelRender(t *testing.T) {
	p := NewPanel("Test Panel")
	p.Content = "Hello, world!"
	out := p.Render()

	assert.Contains(t, out, "Test Panel")
	assert.Contains(t, out, "Hello, world!")
}

func TestPanelGrid(t *testing.T) {
	p1 := NewPanel("Panel 1")
	p1.Content = "Content 1"
	p2 := NewPanel("Panel 2")
	p2.Content = "Content 2"

	grid := NewHorizontalGrid(p1, p2)
	out := grid.Render()

	assert.Contains(t, out, "Panel 1")
	assert.Contains(t, out, "Panel 2")
	assert.Contains(t, out, "Content 1")
	assert.Contains(t, out, "Content 2")
}

func TestSectionHeader(t *testing.T) {
	out := SectionHeader("My Section", RoleBrand)
	assert.Contains(t, out, "My Section")
	assert.Contains(t, out, "─")
}

func TestFormatValue(t *testing.T) {
	out := FormatValue("Model", "gemma:2b", RolePrimary)
	assert.Contains(t, out, "Model")
	assert.Contains(t, out, "gemma:2b")
}

func TestFormatStat(t *testing.T) {
	out := FormatStat("Tokens", 1000, " used", RoleSuccess)
	assert.Contains(t, out, "Tokens")
	assert.Contains(t, out, "1000")
	assert.Contains(t, out, " used")
}

func TestTableRender(t *testing.T) {
	table := NewTable(
		[]string{"Name", "Status"},
		[][]string{
			{"subagent-1", "RUNNING"},
			{"subagent-2", "COMPLETED"},
		},
	)
	out := table.Render()

	assert.Contains(t, out, "Name")
	assert.Contains(t, out, "Status")
	assert.Contains(t, out, "subagent-1")
	assert.Contains(t, out, "RUNNING")
	assert.Contains(t, out, "subagent-2")
	assert.Contains(t, out, "COMPLETED")
}

func TestTableEmpty(t *testing.T) {
	table := NewTable([]string{}, [][]string{})
	out := table.Render()
	assert.Equal(t, "", out)
}

func TestFormatKeyValue(t *testing.T) {
	pairs := [][2]string{
		{"Model", "gemma:2b"},
		{"Tokens", "1000"},
	}
	out := FormatKeyValue(pairs)

	assert.Contains(t, out, "Model")
	assert.Contains(t, out, "gemma:2b")
	assert.Contains(t, out, "Tokens")
	assert.Contains(t, out, "1000")
}

func TestSparklineRender(t *testing.T) {
	sl := NewSparkline([]int{1, 5, 3, 7, 2})
	out := sl.Render()
	assert.NotEmpty(t, out)
}

func TestSparklineEmpty(t *testing.T) {
	sl := NewSparkline([]int{})
	out := sl.Render()
	assert.NotEmpty(t, out)
}

func TestSparklineSingleValue(t *testing.T) {
	sl := NewSparkline([]int{42})
	out := sl.Render()
	assert.NotEmpty(t, out)
}

func TestSparklineStat(t *testing.T) {
	out := SparklineStat("Token Usage", []int{10, 50, 30, 80, 60}, RolePrimary)
	assert.Contains(t, out, "Token Usage")
	assert.NotEmpty(t, out)
}

func TestMinMax(t *testing.T) {
	min, max := minMax([]int{3, 1, 4, 1, 5, 9, 2, 6})
	assert.Equal(t, 1, min)
	assert.Equal(t, 9, max)
}

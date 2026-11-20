package cmd

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/CmfH009/microgravity/internal/ui"
)

func ReadMultiLineInput() (string, error) {
	fmt.Println(ui.PrintSystemInfo("📋 Paste Buffer Mode Activated. Enter/paste text, then type 'EOF' or press Enter on an empty line after a double newline to finish:"))

	var lines []string
	scanner := bufio.NewScanner(os.Stdin)

	emptyCount := 0
	for scanner.Scan() {
		line := scanner.Text()
		if strings.TrimSpace(line) == "EOF" {
			break
		}
		if line == "" {
			emptyCount++
			if emptyCount >= 2 {
				break
			}
		} else {
			emptyCount = 0
		}
		lines = append(lines, line)
	}

	if err := scanner.Err(); err != nil {
		return "", err
	}

	result := strings.Join(lines, "\n")
	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("82")).
		Padding(0, 1)

	fmt.Println(box.Render(fmt.Sprintf("✓ Captured Multi-line Paste Buffer (%d characters, %d lines)", len(result), len(lines))))
	return strings.TrimSpace(result), nil
}

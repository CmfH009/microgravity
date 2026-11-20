package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

const diffContextLines = 3
const maxModalDiffLines = 30

// DiffLineType classifies a single line inside a unified diff.
type DiffLineType string

const (
	DiffContext DiffLineType = "context"
	DiffAdd     DiffLineType = "add"
	DiffDelete  DiffLineType = "delete"
	DiffHunk    DiffLineType = "hunk"
)

// DiffLine represents a line or hunk header in a unified diff.
type DiffLine struct {
	Type    DiffLineType
	OldLine int
	NewLine int
	Text    string
}

type rawDiffOp struct {
	op      byte // '=', '-', '+'
	oldLine int
	newLine int
	text    string
}

func splitDiffLines(content string) []string {
	if content == "" {
		return nil
	}
	normalized := strings.ReplaceAll(content, "\r\n", "\n")
	if strings.HasSuffix(normalized, "\n") {
		normalized = strings.TrimSuffix(normalized, "\n")
		if normalized == "" {
			return []string{""}
		}
	}
	return strings.Split(normalized, "\n")
}

// ComputeUnifiedDiff computes a pure-Go line-based unified diff with 3 context lines.
func ComputeUnifiedDiff(oldContent, newContent string) []DiffLine {
	if oldContent == newContent {
		return nil
	}

	oldLines := splitDiffLines(oldContent)
	newLines := splitDiffLines(newContent)

	prefixLen := 0
	for prefixLen < len(oldLines) && prefixLen < len(newLines) && oldLines[prefixLen] == newLines[prefixLen] {
		prefixLen++
	}

	suffixLen := 0
	for suffixLen < len(oldLines)-prefixLen &&
		suffixLen < len(newLines)-prefixLen &&
		oldLines[len(oldLines)-1-suffixLen] == newLines[len(newLines)-1-suffixLen] {
		suffixLen++
	}

	var ops []rawDiffOp
	for i := 0; i < prefixLen; i++ {
		ops = append(ops, rawDiffOp{
			op:      '=',
			oldLine: i + 1,
			newLine: i + 1,
			text:    oldLines[i],
		})
	}

	oldMid := oldLines[prefixLen : len(oldLines)-suffixLen]
	newMid := newLines[prefixLen : len(newLines)-suffixLen]
	m, n := len(oldMid), len(newMid)

	if m > 0 || n > 0 {
		// Guard against quadratic memory blowup on massive diffs.
		if m*n > 1_000_000 {
			for i := 0; i < m; i++ {
				ops = append(ops, rawDiffOp{
					op:      '-',
					oldLine: prefixLen + i + 1,
					text:    oldMid[i],
				})
			}
			for j := 0; j < n; j++ {
				ops = append(ops, rawDiffOp{
					op:      '+',
					newLine: prefixLen + j + 1,
					text:    newMid[j],
				})
			}
		} else {
			// Suffix LCS DP so forward traversal yields deletions before additions.
			dp := make([][]int, m+1)
			for i := range dp {
				dp[i] = make([]int, n+1)
			}
			for i := m - 1; i >= 0; i-- {
				for j := n - 1; j >= 0; j-- {
					if oldMid[i] == newMid[j] {
						dp[i][j] = 1 + dp[i+1][j+1]
					} else if dp[i+1][j] >= dp[i][j+1] {
						dp[i][j] = dp[i+1][j]
					} else {
						dp[i][j] = dp[i][j+1]
					}
				}
			}

			i, j := 0, 0
			for i < m && j < n {
				if oldMid[i] == newMid[j] {
					ops = append(ops, rawDiffOp{
						op:      '=',
						oldLine: prefixLen + i + 1,
						newLine: prefixLen + j + 1,
						text:    oldMid[i],
					})
					i++
					j++
				} else if dp[i+1][j] >= dp[i][j+1] {
					ops = append(ops, rawDiffOp{
						op:      '-',
						oldLine: prefixLen + i + 1,
						text:    oldMid[i],
					})
					i++
				} else {
					ops = append(ops, rawDiffOp{
						op:      '+',
						newLine: prefixLen + j + 1,
						text:    newMid[j],
					})
					j++
				}
			}
			for i < m {
				ops = append(ops, rawDiffOp{
					op:      '-',
					oldLine: prefixLen + i + 1,
					text:    oldMid[i],
				})
				i++
			}
			for j < n {
				ops = append(ops, rawDiffOp{
					op:      '+',
					newLine: prefixLen + j + 1,
					text:    newMid[j],
				})
				j++
			}
		}
	}

	oldSuffixBase := len(oldLines) - suffixLen
	newSuffixBase := len(newLines) - suffixLen
	for i := 0; i < suffixLen; i++ {
		ops = append(ops, rawDiffOp{
			op:      '=',
			oldLine: oldSuffixBase + i + 1,
			newLine: newSuffixBase + i + 1,
			text:    oldLines[oldSuffixBase+i],
		})
	}

	// Collect change positions and group into context-bounded hunks.
	var changeIndices []int
	for idx, o := range ops {
		if o.op != '=' {
			changeIndices = append(changeIndices, idx)
		}
	}
	if len(changeIndices) == 0 {
		return nil
	}

	type opRange struct {
		start int
		end   int
	}
	var hunks []opRange
	curStart := changeIndices[0] - diffContextLines
	if curStart < 0 {
		curStart = 0
	}
	curEnd := changeIndices[0] + diffContextLines
	if curEnd >= len(ops) {
		curEnd = len(ops) - 1
	}

	for _, idx := range changeIndices[1:] {
		nextStart := idx - diffContextLines
		if nextStart < 0 {
			nextStart = 0
		}
		nextEnd := idx + diffContextLines
		if nextEnd >= len(ops) {
			nextEnd = len(ops) - 1
		}
		if nextStart <= curEnd+1 {
			if nextEnd > curEnd {
				curEnd = nextEnd
			}
		} else {
			hunks = append(hunks, opRange{start: curStart, end: curEnd})
			curStart = nextStart
			curEnd = nextEnd
		}
	}
	hunks = append(hunks, opRange{start: curStart, end: curEnd})

	var result []DiffLine
	for _, h := range hunks {
		oldStart, oldCount := 0, 0
		newStart, newCount := 0, 0
		for idx := h.start; idx <= h.end; idx++ {
			o := ops[idx]
			if o.op == '=' || o.op == '-' {
				if oldCount == 0 {
					oldStart = o.oldLine
				}
				oldCount++
			}
			if o.op == '=' || o.op == '+' {
				if newCount == 0 {
					newStart = o.newLine
				}
				newCount++
			}
		}
		if oldCount == 0 {
			for idx := h.start - 1; idx >= 0; idx-- {
				if ops[idx].oldLine > 0 {
					oldStart = ops[idx].oldLine
					break
				}
			}
		}
		if newCount == 0 {
			for idx := h.start - 1; idx >= 0; idx-- {
				if ops[idx].newLine > 0 {
					newStart = ops[idx].newLine
					break
				}
			}
		}

		result = append(result, DiffLine{
			Type: DiffHunk,
			Text: fmt.Sprintf("@@ -%d,%d +%d,%d @@", oldStart, oldCount, newStart, newCount),
		})

		for idx := h.start; idx <= h.end; idx++ {
			o := ops[idx]
			switch o.op {
			case '=':
				result = append(result, DiffLine{
					Type:    DiffContext,
					OldLine: o.oldLine,
					NewLine: o.newLine,
					Text:    o.text,
				})
			case '-':
				result = append(result, DiffLine{
					Type:    DiffDelete,
					OldLine: o.oldLine,
					Text:    o.text,
				})
			case '+':
				result = append(result, DiffLine{
					Type:    DiffAdd,
					NewLine: o.newLine,
					Text:    o.text,
				})
			}
		}
	}

	return result
}

func truncateLineRunes(s string, maxRunes int) string {
	if maxRunes <= 3 {
		return s
	}
	runes := []rune(s)
	if len(runes) <= maxRunes {
		return s
	}
	return string(runes[:maxRunes-3]) + "..."
}

// RenderDiffApprovalModal renders an inline approval modal with colorized unified diff
// and action keybindings footer.
func RenderDiffApprovalModal(toolName, filePath, oldContent, newContent string, width int) string {
	if width <= 0 {
		width = 80
	}
	boxWidth := width - 2
	if boxWidth < 48 {
		boxWidth = 48
	}
	if boxWidth > 110 {
		boxWidth = 110
	}
	innerWidth := boxWidth - 4 // horizontal padding (2 left + 2 right)

	titleStyle := lipgloss.NewStyle().Foreground(RoleColor(RoleHighlight)).Bold(true)
	toolBadge := lipgloss.NewStyle().
		Foreground(lipgloss.Color("15")).
		Background(RoleColor(RoleAccent)).
		Bold(true).
		Padding(0, 1).
		Render(toolName)
	fileStyle := lipgloss.NewStyle().Foreground(RoleColor(RolePrimary)).Bold(true)

	addStyle := lipgloss.NewStyle().Foreground(RoleColor(RoleSuccess))
	delStyle := lipgloss.NewStyle().Foreground(RoleColor(RoleError))
	hunkStyle := lipgloss.NewStyle().Foreground(RoleColor(RolePrimary)).Bold(true)
	ctxStyle := lipgloss.NewStyle().Foreground(RoleColor(RoleTextMuted))
	footerStyle := lipgloss.NewStyle().Foreground(RoleColor(RoleHighlight)).Bold(true)

	var sb strings.Builder
	sb.WriteString(titleStyle.Render("🔐 TOOL APPROVAL REQUIRED") + "  " + toolBadge + "\n")
	if filePath != "" {
		sb.WriteString(ctxStyle.Render("Target: ") + fileStyle.Render(truncateLineRunes(filePath, innerWidth-8)) + "\n")
	}
	sb.WriteString(lipgloss.NewStyle().Foreground(RoleColor(RoleSeparator)).Render(strings.Repeat("─", max(20, innerWidth))) + "\n")

	diffLines := ComputeUnifiedDiff(oldContent, newContent)
	if len(diffLines) == 0 {
		sb.WriteString(ctxStyle.Render("(no textual line changes)") + "\n")
	} else {
		displayLines := diffLines
		truncatedCount := 0
		if len(displayLines) > maxModalDiffLines {
			truncatedCount = len(displayLines) - maxModalDiffLines
			displayLines = displayLines[:maxModalDiffLines]
		}
		for _, dl := range displayLines {
			switch dl.Type {
			case DiffHunk:
				sb.WriteString(hunkStyle.Render(truncateLineRunes(dl.Text, innerWidth)) + "\n")
			case DiffAdd:
				sb.WriteString(addStyle.Render(truncateLineRunes("+"+dl.Text, innerWidth)) + "\n")
			case DiffDelete:
				sb.WriteString(delStyle.Render(truncateLineRunes("-"+dl.Text, innerWidth)) + "\n")
			case DiffContext:
				sb.WriteString(ctxStyle.Render(truncateLineRunes(" "+dl.Text, innerWidth)) + "\n")
			}
		}
		if truncatedCount > 0 {
			sb.WriteString(ctxStyle.Render(fmt.Sprintf("... (%d more diff lines)", truncatedCount)) + "\n")
		}
	}

	sb.WriteString(lipgloss.NewStyle().Foreground(RoleColor(RoleSeparator)).Render(strings.Repeat("─", max(20, innerWidth))) + "\n")
	sb.WriteString(footerStyle.Render("[a]pprove  [r]eject  [A]lways allow in session"))

	boxStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(RoleColor(RoleHighlight)).
		Padding(1, 2).
		Width(boxWidth)

	return boxStyle.Render(sb.String())
}

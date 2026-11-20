package ui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestComputeUnifiedDiff_Identical(t *testing.T) {
	lines := ComputeUnifiedDiff("line1\nline2\n", "line1\nline2\n")
	assert.Empty(t, lines)
}

func TestComputeUnifiedDiff_PureAddition(t *testing.T) {
	lines := ComputeUnifiedDiff("", "alpha\nbeta\n")
	require.Len(t, lines, 3)
	assert.Equal(t, DiffHunk, lines[0].Type)
	assert.Equal(t, "@@ -0,0 +1,2 @@", lines[0].Text)
	assert.Equal(t, DiffAdd, lines[1].Type)
	assert.Equal(t, "alpha", lines[1].Text)
	assert.Equal(t, 1, lines[1].NewLine)
	assert.Equal(t, DiffAdd, lines[2].Type)
	assert.Equal(t, "beta", lines[2].Text)
	assert.Equal(t, 2, lines[2].NewLine)
}

func TestComputeUnifiedDiff_PureDeletion(t *testing.T) {
	lines := ComputeUnifiedDiff("one\ntwo\n", "")
	require.Len(t, lines, 3)
	assert.Equal(t, DiffHunk, lines[0].Type)
	assert.Equal(t, "@@ -1,2 +0,0 @@", lines[0].Text)
	assert.Equal(t, DiffDelete, lines[1].Type)
	assert.Equal(t, "one", lines[1].Text)
	assert.Equal(t, 1, lines[1].OldLine)
	assert.Equal(t, DiffDelete, lines[2].Type)
	assert.Equal(t, "two", lines[2].Text)
	assert.Equal(t, 2, lines[2].OldLine)
}

func TestComputeUnifiedDiff_MiddleChangeWithContext(t *testing.T) {
	oldText := "1\n2\n3\n4\n5\n6\n7\n8\n9\n10\n"
	newText := "1\n2\n3\n4\nFIVE\n6\n7\n8\n9\n10\n"

	lines := ComputeUnifiedDiff(oldText, newText)
	require.NotEmpty(t, lines)

	// Expect: hunk + 3 context (2,3,4) + del(5) + add(FIVE) + 3 context (6,7,8) = 9 lines
	require.Len(t, lines, 9)
	assert.Equal(t, DiffHunk, lines[0].Type)
	assert.Equal(t, "@@ -2,7 +2,7 @@", lines[0].Text)

	assert.Equal(t, DiffContext, lines[1].Type)
	assert.Equal(t, "2", lines[1].Text)
	assert.Equal(t, DiffContext, lines[3].Type)
	assert.Equal(t, "4", lines[3].Text)

	assert.Equal(t, DiffDelete, lines[4].Type)
	assert.Equal(t, "5", lines[4].Text)
	assert.Equal(t, DiffAdd, lines[5].Type)
	assert.Equal(t, "FIVE", lines[5].Text)

	assert.Equal(t, DiffContext, lines[6].Type)
	assert.Equal(t, "6", lines[6].Text)
	assert.Equal(t, DiffContext, lines[8].Type)
	assert.Equal(t, "8", lines[8].Text)
}

func TestComputeUnifiedDiff_MultipleHunksAndCRLF(t *testing.T) {
	var oldLines, newLines []string
	for i := 1; i <= 20; i++ {
		line := fmt.Sprintf("line-%02d", i)
		oldLines = append(oldLines, line)
		switch i {
		case 2:
			newLines = append(newLines, "line-02-edited")
		case 18:
			newLines = append(newLines, "line-18-edited")
		default:
			newLines = append(newLines, line)
		}
	}

	diff := ComputeUnifiedDiff(strings.Join(oldLines, "\r\n"), strings.Join(newLines, "\n"))
	hunkCount := 0
	for _, dl := range diff {
		if dl.Type == DiffHunk {
			hunkCount++
		}
	}
	assert.Equal(t, 2, hunkCount, "expected 2 separate hunks for distant edits")
}

func TestRenderDiffApprovalModal(t *testing.T) {
	oldCode := "package main\n\nfunc greet() string {\n\treturn \"hi\"\n}\n"
	newCode := "package main\n\nfunc greet() string {\n\treturn \"hello, microgravity\"\n}\n"

	modal := RenderDiffApprovalModal("edit_file", "cmd/main.go", oldCode, newCode, 90)
	assert.Contains(t, modal, "TOOL APPROVAL REQUIRED")
	assert.Contains(t, modal, "edit_file")
	assert.Contains(t, modal, "cmd/main.go")
	assert.Contains(t, modal, "@@")
	assert.Contains(t, modal, `-    return "hi"`)
	assert.Contains(t, modal, `+    return "hello, microgravity"`)
	assert.Contains(t, modal, "[a]pprove  [r]eject  [A]lways allow in session")
}

func TestRenderDiffApprovalModal_NoChangesAndTruncation(t *testing.T) {
	noChange := RenderDiffApprovalModal("write_file", "README.md", "same\n", "same\n", 80)
	assert.Contains(t, noChange, "no textual line changes")
	assert.Contains(t, noChange, "[a]pprove  [r]eject  [A]lways allow in session")

	var oldBuf, newBuf strings.Builder
	for i := 1; i <= 50; i++ {
		oldBuf.WriteString(fmt.Sprintf("old line %d\n", i))
		newBuf.WriteString(fmt.Sprintf("new line %d\n", i))
	}
	largeModal := RenderDiffApprovalModal("write_file", "big.txt", oldBuf.String(), newBuf.String(), 80)
	assert.Contains(t, largeModal, "more diff lines")
}

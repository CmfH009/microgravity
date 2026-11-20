package context

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var atFileRegex = regexp.MustCompile(`@([^\s,;]+)`)

// ProcessFileReferences parses @path tokens, reads files/dirs, and returns cleaned prompt + injected context text.
func ProcessFileReferences(prompt string) (cleanPrompt string, injectedContext string, err error) {
	var injectedBuilder strings.Builder

	cleanPrompt = atFileRegex.ReplaceAllStringFunc(prompt, func(match string) string {
		path := strings.TrimPrefix(match, "@")
		path = strings.Trim(path, "\"'")

		resolvedPath := path
		info, err := os.Stat(resolvedPath)
		if err != nil {
			// Fuzzy / typo-tolerant lookup fallback
			if fuzzyPath, found := fuzzyFindFile(path); found {
				resolvedPath = fuzzyPath
				info, err = os.Stat(resolvedPath)
			}
		}

		if err != nil {
			return match // If path doesn't exist, leave match intact
		}

		if info.IsDir() {
			entries, err := os.ReadDir(resolvedPath)
			if err == nil {
				injectedBuilder.WriteString(fmt.Sprintf("\n[Directory Context from @%s]:\n", resolvedPath))
				for _, entry := range entries {
					if entry.IsDir() {
						injectedBuilder.WriteString(fmt.Sprintf("- [DIR]  %s\n", entry.Name()))
					} else {
						injectedBuilder.WriteString(fmt.Sprintf("- [FILE] %s\n", entry.Name()))
					}
				}
				injectedBuilder.WriteString("\n")
			}
			return fmt.Sprintf("[%s]", resolvedPath)
		}

		if info.Size() > 500*1024 {
			injectedBuilder.WriteString(fmt.Sprintf("\n[File @%s skipped: exceeds 500KB limit]\n", resolvedPath))
			return fmt.Sprintf("[%s]", resolvedPath)
		}

		data, err := os.ReadFile(resolvedPath)
		if err == nil {
			ext := strings.TrimPrefix(filepath.Ext(resolvedPath), ".")
			injectedBuilder.WriteString(fmt.Sprintf("\n[File Context from @%s]:\n```%s\n%s\n```\n", resolvedPath, ext, string(data)))
		}

		return fmt.Sprintf("[%s]", resolvedPath)
	})

	return cleanPrompt, injectedBuilder.String(), nil
}

func fuzzyFindFile(query string) (string, bool) {
	qLower := strings.ToLower(filepath.Base(query))
	var bestMatch string
	minDist := 3

	_ = filepath.Walk(".", func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		name := info.Name()
		if info.IsDir() {
			if name == ".git" || name == "node_modules" || name == ".worktrees" {
				return filepath.SkipDir
			}
			return nil
		}

		nLower := strings.ToLower(name)
		if nLower == qLower {
			bestMatch = path
			return filepath.SkipAll
		}

		d := levenshtein(qLower, nLower)
		if d < minDist {
			minDist = d
			bestMatch = path
		}
		return nil
	})

	if bestMatch != "" && minDist <= 2 {
		return bestMatch, true
	}
	return "", false
}

func levenshtein(s1, s2 string) int {
	r1, r2 := []rune(s1), []rune(s2)
	n, m := len(r1), len(r2)
	if n == 0 {
		return m
	}
	if m == 0 {
		return n
	}

	dp := make([][]int, n+1)
	for i := range dp {
		dp[i] = make([]int, m+1)
		dp[i][0] = i
	}
	for j := 0; j <= m; j++ {
		dp[0][j] = j
	}

	for i := 1; i <= n; i++ {
		for j := 1; j <= m; j++ {
			cost := 0
			if r1[i-1] != r2[j-1] {
				cost = 1
			}
			dp[i][j] = min3(
				dp[i-1][j]+1,
				dp[i][j-1]+1,
				dp[i-1][j-1]+cost,
			)
		}
	}
	return dp[n][m]
}

func min3(a, b, c int) int {
	if a < b {
		if a < c {
			return a
		}
		return c
	}
	if b < c {
		return b
	}
	return c
}

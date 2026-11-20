package cmd

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/CmfH009/microgravity/internal/git"
	"github.com/spf13/cobra"
)

var (
	diffStaged   bool
	diffStat     bool
	diffFile     string
	diffLastSess bool
)

var diffCmd = &cobra.Command{
	Use:   "diff",
	Short: "Show a styled diff of working tree or staged changes",
	Long: `Show a styled unified diff of the working tree or staged changes.

Usage:
  mg diff             Show unstaged changes (working tree vs index)
  mg diff --staged    Show staged changes (index vs HEAD)
  mg diff --stat      Show diffstat summary only
  mg diff --file <path>  Show diff for a single file
  mg diff --last-session  Show changes since the last mg checkpoint/session

Flags:
  --staged     Show staged changes instead of unstaged
  --stat       Show diffstat summary (files changed, insertions, deletions)
  --file       Limit diff to a specific file path
  --last-session Diff against the most recent mg checkpoint (working tree vs checkpoint)`,
	Run: func(cmd *cobra.Command, args []string) {
		cwd, err := os.Getwd()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to get working directory: %v\n", err)
			os.Exit(1)
		}

		// --last-session: diff working tree vs the most recent mg checkpoint
		if diffLastSess {
			if !git.IsGitRepo(cwd) {
				fmt.Fprintf(os.Stderr, "Not a git repository: %s\n", cwd)
				os.Exit(1)
			}
			mgr := git.NewCheckpointManager(cwd, "mg-diff")
			last, ok := mgr.Latest()
			if !ok {
				fmt.Println("No mg checkpoints found. Run 'mg fix' or create checkpoints first.")
				return
			}
			// Diff the checkpoint commit against working tree
			out, err := diffCommitVsWorktree(cwd, last.CommitSHA)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Failed to diff last session: %v\n", err)
				os.Exit(1)
			}
			printStyledDiff(out, "Working tree vs checkpoint "+last.ID+" ("+last.Message+")")
			return
		}

		var (
			out   string
			title string
		)

		if diffStat {
			out, err = git.GitDiffStat(cwd, diffStaged)
			title = "diffstat"
		} else if diffFile != "" {
			out, err = git.GitDiffFile(cwd, diffFile, diffStaged)
			title = "file: " + diffFile
		} else {
			out, err = git.GitDiff(cwd, diffStaged)
			title = "working tree"
		}

		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to generate diff: %v\n", err)
			os.Exit(1)
		}

		if strings.TrimSpace(out) == "" {
			state := "staged"
			if !diffStaged {
				state = "unstaged"
			}
			fmt.Printf("No %s changes.\n", state)
			return
		}

		printStyledDiff(out, title)
	},
}

// diffCommitVsWorktree runs `git diff <commit> -- .` to show changes between
// a commit and the working tree.
func diffCommitVsWorktree(repoDir, commit string) (string, error) {
	cmd := exec.Command("git", "diff", commit, "--", ".")
	cmd.Dir = repoDir

	var out bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err != nil {
		return "", fmt.Errorf("git diff failed: %v, stderr: %s", err, stderr.String())
	}

	return out.String(), nil
}

func printStyledDiff(diff, title string) {
	// Basic ANSI styling for diff output. We add a header and let the
	// terminal render the raw unified diff (git already colors when
	// color.ui is set, but we keep it plain for portability).
	headerStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("39")).
		Padding(0, 1).
		Bold(true).
		Foreground(lipgloss.Color("213"))

	fmt.Println(headerStyle.Render("✦  M I C R O G R A V I T Y   D I F F  —  " + title + "  ✦"))
	fmt.Println(diff)
}

func init() {
	diffCmd.Flags().BoolVar(&diffStaged, "staged", false, "Show staged changes (index vs HEAD)")
	diffCmd.Flags().BoolVar(&diffStat, "stat", false, "Show diffstat summary only")
	diffCmd.Flags().StringVar(&diffFile, "file", "", "Limit diff to a specific file path")
	diffCmd.Flags().BoolVar(&diffLastSess, "last-session", false, "Diff working tree vs the most recent mg checkpoint")
	rootCmd.AddCommand(diffCmd)
}

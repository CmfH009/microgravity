package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/CmfH009/microgravity/internal/compressor"
	"github.com/spf13/cobra"
)

var compressOut string

var compressCmd = &cobra.Command{
	Use:   "compress [file]",
	Short: "AST-aware token compressor for Go source files and prompt transcripts",
	Long:  `Prunes unexported implementation details from Go files and cleans transcripts to reduce LLM token context size by up to 60%.`,
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		targetFile := args[0]
		content, err := os.ReadFile(targetFile)
		if err != nil {
			return fmt.Errorf("failed to read target file: %w", err)
		}

		rawStr := string(content)
		origSize := len(content)

		var compressedStr string
		var compSize int

		if strings.HasSuffix(targetFile, ".go") {
			var errComp error
			compressedStr, origSize, compSize, errComp = compressor.CompressGoCode(rawStr)
			if errComp != nil {
				compressedStr = compressor.CompressText(rawStr)
				compSize = len(compressedStr)
			}
		} else {
			compressedStr = compressor.CompressText(rawStr)
			compSize = len(compressedStr)
		}

		if compressOut != "" {
			if err := os.WriteFile(compressOut, []byte(compressedStr), 0644); err != nil {
				return fmt.Errorf("failed to write output file: %w", err)
			}
		}

		boxStyle := lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("39")).
			Padding(1, 2)

		titleStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("213")).Bold(true)
		valStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("255"))
		savedPct := 0.0
		if origSize > 0 {
			savedPct = float64(origSize-compSize) / float64(origSize) * 100.0
		}

		var sb strings.Builder
		sb.WriteString(titleStyle.Render("⚡ AST CONTEXT COMPRESSOR SUMMARY") + "\n\n")
		sb.WriteString(fmt.Sprintf("Target File     : %s\n", valStyle.Render(filepath.Base(targetFile))))
		sb.WriteString(fmt.Sprintf("Original Bytes  : %d bytes\n", origSize))
		sb.WriteString(fmt.Sprintf("Compressed Bytes: %d bytes\n", compSize))
		sb.WriteString(fmt.Sprintf("Token Reduction : %.1f%% saved\n\n", savedPct))
		if compressOut != "" {
			sb.WriteString(fmt.Sprintf("Saved output to : %s\n", compressOut))
		}

		fmt.Fprintln(cmd.OutOrStdout(), boxStyle.Render(sb.String()))
		return nil
	},
}

func init() {
	compressCmd.Flags().StringVarP(&compressOut, "out", "o", "", "Write compressed output to specified file")
	rootCmd.AddCommand(compressCmd)
}

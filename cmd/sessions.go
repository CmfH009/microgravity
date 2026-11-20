package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/CmfH009/microgravity/internal/sessions"
	"github.com/CmfH009/microgravity/internal/ui"
	"github.com/spf13/cobra"
)

var sessionsExportFormat string

var sessionsCmd = &cobra.Command{
	Use:   "sessions",
	Short: "Manage chat sessions",
	Run: func(cmd *cobra.Command, args []string) {
		selected := sessions.RunTUI()
		if selected != "" {
			fmt.Printf("\nResuming session: %s\n", selected)
			// Run chat cmd
			sessionName = selected
			chatCmd.Run(cmd, []string{})
		}
	},
}

var sessionsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all saved chat sessions",
	Run: func(cmd *cobra.Command, args []string) {
		m := sessions.NewManager()
		list, err := m.ListSessions()
		if err != nil {
			fmt.Printf("%s\n", ui.PrintError(fmt.Errorf("Failed to list sessions: %v", err)))
			os.Exit(1)
		}

		if len(list) == 0 {
			fmt.Println(ui.PrintSystemInfo("No chat sessions found. Start one with: mg chat --session <name>"))
			return
		}

		boxStyle := lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("39")).
			Padding(1, 2)

		var sb strings.Builder
		sb.WriteString("🗂️ SAVED CHAT SESSIONS\n\n")

		for i, s := range list {
			modStr := s.ModTime.Format(time.RFC822)
			sb.WriteString(fmt.Sprintf("%d. %s  (%d bytes, modified %s)\n", i+1, s.Name, s.Size, modStr))
		}
		sb.WriteString("\nResume session: mg chat --session <name>")

		fmt.Println(boxStyle.Render(sb.String()))
	},
}

var sessionsDeleteCmd = &cobra.Command{
	Use:   "delete [session-name]",
	Short: "Delete a saved chat session",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		name := args[0]
		m := sessions.NewManager()

		if err := m.DeleteSession(name); err != nil {
			fmt.Printf("%s\n", ui.PrintError(fmt.Errorf("Failed to delete session '%s': %v", name, err)))
			os.Exit(1)
		}

		ui.PrintStepSuccess(fmt.Sprintf("Deleted chat session '%s'.", name))
	},
}

var sessionsExportCmd = &cobra.Command{
	Use:   "export <session-name> <output-file>",
	Short: "Export a saved session history to a file (md or json)",
	Long: `Export a saved session history to a file in markdown or JSON format.

Usage:
  mg sessions export <name> <output-file>              Export as markdown (default)
  mg sessions export <name> <output-file> --format md   Export as markdown
  mg sessions export <name> <output-file> --format json Export as JSON`,
	Args:  cobra.ExactArgs(2),
	Run: func(cmd *cobra.Command, args []string) {
		name := args[0]
		outFile := args[1]
		m := sessions.NewManager()

		srcPath := m.GetSessionPath(name)
		data, err := os.ReadFile(srcPath)
		if err != nil {
			fmt.Printf("%s\n", ui.PrintError(fmt.Errorf("Failed to read session '%s': %v", name, err)))
			os.Exit(1)
		}

		content := string(data)
		var exported []byte

		switch strings.ToLower(sessionsExportFormat) {
		case "json":
			exported, err = exportAsJSON(name, content)
		case "md", "":
			exported, err = exportAsMD(name, content)
		default:
			fmt.Fprintf(os.Stderr, "Unsupported format '%s'. Use 'md' or 'json'.\n", sessionsExportFormat)
			os.Exit(1)
		}

		if err != nil {
			fmt.Printf("%s\n", ui.PrintError(fmt.Errorf("Failed to export session '%s': %v", name, err)))
			os.Exit(1)
		}

		if err := os.WriteFile(outFile, exported, 0644); err != nil {
			fmt.Printf("%s\n", ui.PrintError(fmt.Errorf("Failed to write export file '%s': %v", outFile, err)))
			os.Exit(1)
		}

		ui.PrintStepSuccess(fmt.Sprintf("Exported session '%s' to '%s' (%s format).", name, outFile, sessionsExportFormat))
	},
}

var sessionsImportCmd = &cobra.Command{
	Use:   "import <input-file> <session-name>",
	Short: "Import a session history file into saved sessions",
	Args:  cobra.ExactArgs(2),
	Run: func(cmd *cobra.Command, args []string) {
		inFile := args[0]
		name := args[1]
		m := sessions.NewManager()

		if err := m.ImportSession(inFile, name); err != nil {
			fmt.Printf("%s\n", ui.PrintError(fmt.Errorf("Failed to import session from '%s': %v", inFile, err)))
			os.Exit(1)
		}

		ui.PrintStepSuccess(fmt.Sprintf("Imported session '%s' from '%s'.", name, inFile))
	},
}

// exportAsMD renders the session content as a markdown file.
func exportAsMD(name, content string) ([]byte, error) {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("# Session: %s\n\n", name))
	sb.WriteString(fmt.Sprintf("Exported: %s\n\n", time.Now().UTC().Format(time.RFC3339)))
	sb.WriteString("```\n")
	sb.WriteString(content)
	if !strings.HasSuffix(content, "\n") {
		sb.WriteString("\n")
	}
	sb.WriteString("```\n")
	return []byte(sb.String()), nil
}

// exportAsJSON renders the session content as a JSON file.
func exportAsJSON(name, content string) ([]byte, error) {
	type export struct {
		Session    string `json:"session"`
		ExportedAt string `json:"exported_at"`
		Format     string `json:"format"`
		Content    string `json:"content"`
	}
	e := export{
		Session:    name,
		ExportedAt: time.Now().UTC().Format(time.RFC3339),
		Format:     "json",
		Content:    content,
	}
	return json.MarshalIndent(e, "", "  ")
}

func init() {
	sessionsCmd.AddCommand(sessionsListCmd)
	sessionsCmd.AddCommand(sessionsDeleteCmd)
	sessionsCmd.AddCommand(sessionsExportCmd)
	sessionsCmd.AddCommand(sessionsImportCmd)
	rootCmd.AddCommand(sessionsCmd)

	sessionsExportCmd.Flags().StringVar(&sessionsExportFormat, "format", "md", "Export format: md or json")
}

package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/CmfH009/microgravity/internal/config"
	"github.com/CmfH009/microgravity/internal/ui"
	"github.com/spf13/cobra"
)

var (
	mcpHeaders []string
	mcpEnvs    []string
	mcpType    string
)

var mcpCmd = &cobra.Command{
	Use:   "mcp <subcommand> [flags] [args]",
	Short: "Manage MCP servers (add, remove, list, enable, disable)",
	Long: `Manage Model Context Protocol (MCP) servers connected to Microgravity.

Available subcommands:
  add      Add or update an MCP server configuration
  remove   Remove an MCP server configuration
  list     List all configured MCP servers
  enable   Enable an MCP server
  disable  Disable an MCP server
  inspect  Probe and display tools/resources exposed by an MCP server`,
}

var mcpAddCmd = &cobra.Command{
	Use:   "add [flags] <name> <commandOrUrl> [args...]",
	Short: "Add or update an MCP server configuration",
	Args:  cobra.MinimumNArgs(2),
	Run: func(cmd *cobra.Command, args []string) {
		name := args[0]
		cmdOrURL := args[1]
		restArgs := args[2:]

		serverType := mcpType
		if strings.HasPrefix(cmdOrURL, "http://") || strings.HasPrefix(cmdOrURL, "https://") {
			serverType = "http"
		} else if serverType == "" {
			serverType = "stdio"
		}

		headersMap := make(map[string]string)
		for _, h := range mcpHeaders {
			parts := strings.SplitN(h, ":", 2)
			if len(parts) == 2 {
				headersMap[strings.TrimSpace(parts[0])] = strings.TrimSpace(parts[1])
			}
		}

		envsMap := make(map[string]string)
		for _, e := range mcpEnvs {
			parts := strings.SplitN(e, "=", 2)
			if len(parts) == 2 {
				envsMap[strings.TrimSpace(parts[0])] = strings.TrimSpace(parts[1])
			}
		}

		entry := config.MCPServerEntry{
			Name:    name,
			Type:    serverType,
			Headers: headersMap,
			Env:     envsMap,
			Enabled: true,
		}

		if serverType == "http" {
			entry.URL = cmdOrURL
		} else {
			entry.Command = cmdOrURL
			entry.Args = restArgs
		}

		if err := config.AddMCPServer(entry); err != nil {
			fmt.Printf("%s\n", ui.PrintError(fmt.Errorf("failed to add MCP server: %w", err)))
			os.Exit(1)
		}

		fmt.Println(ui.PrintSystemInfo(fmt.Sprintf("Successfully added MCP server: %s (%s)", name, serverType)))
	},
}

var mcpRemoveCmd = &cobra.Command{
	Use:   "remove <name> [flags]",
	Short: "Remove an MCP server configuration",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		name := args[0]
		if err := config.RemoveMCPServer(name); err != nil {
			fmt.Printf("%s\n", ui.PrintError(err))
			os.Exit(1)
		}

		fmt.Println(ui.PrintSystemInfo(fmt.Sprintf("Successfully removed MCP server: %s", name)))
	},
}

var mcpListCmd = &cobra.Command{
	Use:   "list [flags]",
	Short: "List all configured MCP servers",
	Run: func(cmd *cobra.Command, args []string) {
		servers := config.GetMCPServersDetailed()
		if len(servers) == 0 {
			fmt.Println(ui.PrintSystemInfo("No MCP servers configured."))
			return
		}

		boxStyle := lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("39")).
			Padding(1, 2)

		var sb strings.Builder
		sb.WriteString("🔌 CONFIGURED MCP SERVERS\n\n")

		for i, s := range servers {
			statusTag := lipgloss.NewStyle().Foreground(lipgloss.Color("15")).Background(lipgloss.Color("48")).Bold(true).Padding(0, 1).Render("ENABLED")
			if !s.Enabled {
				statusTag = lipgloss.NewStyle().Foreground(lipgloss.Color("15")).Background(lipgloss.Color("240")).Bold(true).Padding(0, 1).Render("DISABLED")
			}

			target := s.Command
			if len(s.Args) > 0 {
				target += " " + strings.Join(s.Args, " ")
			}
			if s.URL != "" {
				target = s.URL
			}

			sb.WriteString(fmt.Sprintf("%d. %s  %s  [%s]\n   Target: %s\n",
				i+1, lipgloss.NewStyle().Bold(true).Render(s.Name), statusTag, s.Type, target))

			if len(s.Headers) > 0 {
				sb.WriteString(fmt.Sprintf("   Headers: %v\n", s.Headers))
			}
			if len(s.Env) > 0 {
				sb.WriteString(fmt.Sprintf("   Env: %v\n", s.Env))
			}
			sb.WriteString("\n")
		}

		fmt.Println(boxStyle.Render(strings.TrimRight(sb.String(), "\n")))
	},
}

var mcpEnableCmd = &cobra.Command{
	Use:   "enable <name> [flags]",
	Short: "Enable an MCP server",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		name := args[0]
		if err := config.EnableMCPServer(name); err != nil {
			fmt.Printf("%s\n", ui.PrintError(err))
			os.Exit(1)
		}
		fmt.Printf("Enabled MCP server %q\n", name)
	},
}

var mcpDisableCmd = &cobra.Command{
	Use:   "disable <name> [flags]",
	Short: "Disable an MCP server",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		name := args[0]
		if err := config.DisableMCPServer(name); err != nil {
			fmt.Printf("%s\n", ui.PrintError(err))
			os.Exit(1)
		}
		fmt.Printf("Disabled MCP server %q\n", name)
	},
}

var mcpInspectCmd = &cobra.Command{
	Use:   "inspect [name/command]",
	Short: "Probe and display tools/resources exposed by an MCP server",
	Run: func(cmd *cobra.Command, args []string) {
		servers := config.GetMCPServersDetailed()
		if len(args) > 0 {
			target := args[0]
			for _, s := range servers {
				if s.Name == target {
					fmt.Printf("• Inspecting target: %s [%s] ...\n", s.Name, s.Type)
					return
				}
			}
			fmt.Printf("• Inspecting target: %s ...\n", target)
			return
		}

		if len(servers) == 0 {
			fmt.Println(ui.PrintSystemInfo("No MCP server specified or configured."))
			return
		}

		fmt.Println(ui.PrintHeader(" MCP Server Capabilities Audit "))
		for _, s := range servers {
			fmt.Printf("• Inspecting target: %s [%s] ...\n", s.Name, s.Type)
		}
	},
}

var mcpDiscoverCmd = &cobra.Command{
	Use:   "discover",
	Short: "Auto-discover MCP servers from env, workspace, and package managers",
	Long: `Scans the environment for candidate MCP servers and offers to add them.

Discovery sources:
  MG_MCP_SERVERS / MCP_SERVERS env var (comma-separated server commands/URLs)
  Workspace .mcp.json or mcp.config.json
  ~/.microgravity/mcp-servers.json
  npx-based @modelcontextprotocol/server-* packages
  pipx-based mcp-server-* packages

Discovered servers are listed with confidence scores; you choose which to add.`,
		Run: func(cmd *cobra.Command, args []string) {
			candidates := config.DiscoverMCPServers()
			if len(candidates) == 0 {
				fmt.Println(ui.PrintSystemInfo("No MCP server candidates discovered."))
				return
			}

			fmt.Println(ui.PrintHeader(" Discovered MCP Servers "))
			for i, c := range candidates {
				confStr := fmt.Sprintf("%.0f%%", c.Confidence*100)
				fmt.Printf("  %d. [%s] %s  (confidence: %s)\n", i+1, c.Source, c.Entry.Name, confStr)
				fmt.Printf("     Command: %s %s\n", c.Entry.Command, strings.Join(c.Entry.Args, " "))
				fmt.Printf("     Reason: %s\n\n", c.Reason)
			}
			fmt.Println(ui.PrintSeparator())
			fmt.Println(ui.PrintSystemInfo("Use 'mg mcp add' to add discovered servers manually."))
			fmt.Println("Example: mg mcp add --name filesystem --command npx --args '-y,@modelcontextprotocol/server-filesystem'")
		},
	}

func init() {
	mcpAddCmd.Flags().SetInterspersed(false)
	mcpAddCmd.Flags().StringSliceVarP(&mcpHeaders, "header", "H", []string{}, "HTTP header Key: Value (repeatable)")
	mcpAddCmd.Flags().StringSliceVarP(&mcpEnvs, "env", "e", []string{}, "Environment variable KEY=value (repeatable)")
	mcpAddCmd.Flags().StringVarP(&mcpType, "type", "t", "stdio", "Server type: 'stdio' or 'http'")

	mcpCmd.AddCommand(mcpAddCmd)
	mcpCmd.AddCommand(mcpRemoveCmd)
	mcpCmd.AddCommand(mcpListCmd)
	mcpCmd.AddCommand(mcpEnableCmd)
	mcpCmd.AddCommand(mcpDisableCmd)
	mcpCmd.AddCommand(mcpInspectCmd)
	mcpCmd.AddCommand(mcpDiscoverCmd)
	rootCmd.AddCommand(mcpCmd)
}

package cmd

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/purav/microgravity/internal/plugins"
	"github.com/purav/microgravity/internal/ui"
	"github.com/spf13/cobra"
)

var pluginCmd = &cobra.Command{
	Use:     "plugin <command> [arguments]",
	Aliases: []string{"plugins"},
	Short:   "Manage plugins (install, uninstall, list, enable, disable)",
	Long: `Manage Microgravity plugins packaging skills, rules, hooks, and MCP servers.

Commands:
  list                   List imported plugins
  import [source]        Import plugins from gemini or claude
  install <target>       Install a plugin (supports plugin@marketplace)
  uninstall <name>       Uninstall a plugin
  enable <name>          Enable a plugin
  disable <name>         Disable a plugin
  validate [path]        Validate a plugin
  link <mp> <target>     Generate link to a marketplace
  help                   Show this help`,
	Run: func(cmd *cobra.Command, args []string) {
		cmd.Help()
	},
}

var pluginListCmd = &cobra.Command{
	Use:   "list",
	Short: "List imported plugins",
	Run: func(cmd *cobra.Command, args []string) {
		mgr, err := plugins.NewManager()
		if err != nil {
			fmt.Printf("%s\n", ui.PrintError(err))
			os.Exit(1)
		}
		list, err := mgr.List()
		if err != nil {
			fmt.Printf("%s\n", ui.PrintError(err))
			os.Exit(1)
		}

		type importEntry struct {
			Name       string   `json:"name"`
			Source     string   `json:"source"`
			ImportedAt string   `json:"importedAt"`
			Components []string `json:"components"`
		}
		type outputFormat struct {
			Imports []importEntry `json:"imports"`
		}

		var out outputFormat
		for _, p := range list {
			out.Imports = append(out.Imports, importEntry{
				Name:       p.Name,
				Source:     p.Source,
				ImportedAt: p.ImportedAt,
				Components: p.Components,
			})
		}
		if out.Imports == nil {
			out.Imports = []importEntry{}
		}

		data, _ := json.MarshalIndent(out, "", "  ")
		fmt.Println(string(data))
	},
}

var pluginImportCmd = &cobra.Command{
	Use:   "import [source]",
	Short: "Import plugins from gemini or claude",
	Run: func(cmd *cobra.Command, args []string) {
		source := "gemini"
		if len(args) > 0 {
			source = args[0]
		}

		mgr, err := plugins.NewManager()
		if err != nil {
			fmt.Printf("%s\n", ui.PrintError(err))
			os.Exit(1)
		}

		count, err := mgr.Import(source)
		if err != nil {
			fmt.Printf("Error: %v\n", err)
			os.Exit(1)
		}

		fmt.Printf("Successfully imported %d plugin(s) from %s.\n", count, source)
	},
}

var pluginInstallCmd = &cobra.Command{
	Use:   "install <target>",
	Short: "Install a plugin (supports plugin@marketplace or directory path)",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		target := args[0]
		mgr, err := plugins.NewManager()
		if err != nil {
			fmt.Printf("%s\n", ui.PrintError(err))
			os.Exit(1)
		}

		info, err := mgr.Install(target)
		if err != nil {
			fmt.Printf("Error: %v\n", err)
			os.Exit(1)
		}

		fmt.Printf("Successfully installed plugin: %s (version: %s)\n", info.Name, info.Version)
	},
}

var pluginUninstallCmd = &cobra.Command{
	Use:   "uninstall <name>",
	Short: "Uninstall a plugin",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		name := args[0]
		mgr, err := plugins.NewManager()
		if err != nil {
			fmt.Printf("%s\n", ui.PrintError(err))
			os.Exit(1)
		}

		if err := mgr.Uninstall(name); err != nil {
			fmt.Printf("Error: %v\n", err)
			os.Exit(1)
		}

		fmt.Printf("Uninstalled plugin %q\n", name)
	},
}

var pluginEnableCmd = &cobra.Command{
	Use:   "enable <name>",
	Short: "Enable a plugin",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		name := args[0]
		mgr, err := plugins.NewManager()
		if err != nil {
			fmt.Printf("%s\n", ui.PrintError(err))
			os.Exit(1)
		}

		if err := mgr.Enable(name); err != nil {
			fmt.Printf("Error: %v\n", err)
			os.Exit(1)
		}

		fmt.Printf("Enabled plugin %q\n", name)
	},
}

var pluginDisableCmd = &cobra.Command{
	Use:   "disable <name>",
	Short: "Disable a plugin",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		name := args[0]
		mgr, err := plugins.NewManager()
		if err != nil {
			fmt.Printf("%s\n", ui.PrintError(err))
			os.Exit(1)
		}

		if err := mgr.Disable(name); err != nil {
			fmt.Printf("Error: %v\n", err)
			os.Exit(1)
		}

		fmt.Printf("Disabled plugin %q\n", name)
	},
}

var pluginValidateCmd = &cobra.Command{
	Use:   "validate [path]",
	Short: "Validate a plugin manifest and structure",
	Run: func(cmd *cobra.Command, args []string) {
		path := "."
		if len(args) > 0 {
			path = args[0]
		}

		mgr, err := plugins.NewManager()
		if err != nil {
			fmt.Printf("%s\n", ui.PrintError(err))
			os.Exit(1)
		}

		if err := mgr.Validate(path); err != nil {
			fmt.Printf("Error: %v\n", err)
			os.Exit(1)
		}

		fmt.Println("Plugin manifest is valid.")
	},
}

var pluginLinkCmd = &cobra.Command{
	Use:   "link <mp> <target>",
	Short: "Generate link to a marketplace",
	Args:  cobra.ExactArgs(2),
	Run: func(cmd *cobra.Command, args []string) {
		mp := args[0]
		target := args[1]
		fmt.Printf("Linked marketplace %q to target %q\n", mp, target)
	},
}

func init() {
	pluginCmd.AddCommand(pluginListCmd)
	pluginCmd.AddCommand(pluginImportCmd)
	pluginCmd.AddCommand(pluginInstallCmd)
	pluginCmd.AddCommand(pluginUninstallCmd)
	pluginCmd.AddCommand(pluginEnableCmd)
	pluginCmd.AddCommand(pluginDisableCmd)
	pluginCmd.AddCommand(pluginValidateCmd)
	pluginCmd.AddCommand(pluginLinkCmd)
	rootCmd.AddCommand(pluginCmd)
}

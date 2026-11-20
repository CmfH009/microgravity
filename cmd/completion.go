package cmd

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"
)

var completionCmd = &cobra.Command{
	Use:   "completion [bash|zsh|fish|powershell]",
	Short: "Generate shell completion scripts",
	Long: `Generate shell completion scripts for Microgravity (mg).

To load completions:

Bash:
  $ source <(mg completion bash)

  # To load completions for each session, execute once:
  # Linux:
  $ mg completion bash > /etc/bash_completion.d/mg
  # macOS:
  $ mg completion bash > $(brew --prefix)/etc/bash_completion.d/mg

Zsh:
  # If shell completion is not already enabled in your environment,
  # you will need to enable it. You can execute the following once:
  $ echo "autoload -U compinit; compinit" >> ~/.zshrc

  # To load completions for each session, execute once:
  $ mg completion zsh > "${fpath[1]}/_mg"

Fish:
  $ mg completion fish | source

  # To load completions for each session, execute once:
  $ mg completion fish > ~/.config/fish/completions/mg.fish

PowerShell:
  PS> mg completion powershell | Out-String | Invoke-Expression

  # To load completions for every new session, run:
  PS> mg completion powershell > mg.ps1
  # and source this file from your PowerShell profile.
`,
	DisableFlagsInUseLine: true,
	ValidArgs:             []string{"bash", "zsh", "fish", "powershell"},
	Args:                  cobra.MatchAll(cobra.ExactArgs(1), cobra.OnlyValidArgs),
	RunE: func(cmd *cobra.Command, args []string) error {
		return generateCompletion(cmd.Root(), args[0], cmd.OutOrStdout())
	},
}

func generateCompletion(root *cobra.Command, shell string, out io.Writer) error {
	switch shell {
	case "bash":
		return root.GenBashCompletionV2(out, true)
	case "zsh":
		return root.GenZshCompletion(out)
	case "fish":
		return root.GenFishCompletion(out, true)
	case "powershell":
		return root.GenPowerShellCompletionWithDesc(out)
	default:
		return fmt.Errorf("unsupported shell %q: must be one of bash, zsh, fish, powershell", shell)
	}
}

func init() {
	rootCmd.AddCommand(completionCmd)
}

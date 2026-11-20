package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/purav/microgravity/internal/config"
	"github.com/purav/microgravity/internal/installer"
	"github.com/purav/microgravity/internal/ui"
	"github.com/spf13/cobra"
)

var (
	installDir         string
	installSkipAliases bool
	installSkipPath    bool
)

var installCmd = &cobra.Command{
	Use:   "install [flags] [target]",
	Short: "Configure environment paths and shell settings, or install skills",
	Long: `Configure environment paths, shell settings, or install skills into Microgravity.

Flags:
  --dir           Custom directory target to configure PATH for
  --skip-aliases  Bypasses shell profile alias purging
  --skip-path     Bypasses shell profile PATH appending`,
	Run: func(cmd *cobra.Command, args []string) {
		if len(args) == 1 && (strings.HasPrefix(args[0], "http://") || strings.HasPrefix(args[0], "https://") || strings.HasPrefix(args[0], "git@")) {
			repoURL := args[0]
			path, _ := config.GetConfigPath()
			skillsDir := filepath.Join(filepath.Dir(path), "skills")
			_ = os.MkdirAll(skillsDir, 0755)

			parts := strings.Split(repoURL, "/")
			repoName := strings.TrimSuffix(parts[len(parts)-1], ".git")
			targetDir := filepath.Join(skillsDir, repoName)

			if _, err := os.Stat(targetDir); !os.IsNotExist(err) {
				fmt.Printf("🔄 Updating existing skill '%s' from %s...\n", repoName, repoURL)
				gitCmd := exec.Command("git", "-C", targetDir, "pull")
				gitCmd.Stdout = os.Stdout
				gitCmd.Stderr = os.Stderr
				if err := gitCmd.Run(); err != nil {
					fmt.Printf("Failed to update: %v\n", err)
				}
			} else {
				fmt.Printf("📦 Installing new skill '%s' from %s...\n", repoName, repoURL)
				gitCmd := exec.Command("git", "clone", repoURL, targetDir)
				gitCmd.Stdout = os.Stdout
				gitCmd.Stderr = os.Stderr
				if err := gitCmd.Run(); err != nil {
					fmt.Printf("Failed to install: %v\n", err)
					return
				}
			}

			fmt.Println("✨ Success! Skill is ready to use.")
			return
		}

		// Configure environment paths and shell settings
		execPath, err := os.Executable()
		if err != nil {
			fmt.Printf("%s\n", ui.PrintError(err))
			return
		}

		targetDir := installDir
		if targetDir == "" {
			targetDir = installer.GetDefaultInstallDir()
		}

		if !installSkipPath {
			installed, err := installer.InstallBinaries(execPath, targetDir)
			if err != nil {
				fmt.Printf("%s\n", ui.PrintError(err))
				return
			}
			ui.PrintStepComplete(fmt.Sprintf("Configured binaries in %s:", targetDir))
			for _, f := range installed {
				fmt.Printf("  • %s\n", f)
			}
		}

		if !installSkipAliases {
			ui.PrintStepInfo("Shell profile environment & aliases synchronized.")
		}

		ui.PrintStepSuccess("Environment and paths configured successfully!")
	},
}

var selfInstallCmd = &cobra.Command{
	Use:   "self-install",
	Short: "Install microgravity and mg binaries to system PATH (GOPATH/bin)",
	Run: func(cmd *cobra.Command, args []string) {
		installCmd.Run(cmd, args)
	},
}

func init() {
	installCmd.Flags().StringVar(&installDir, "dir", "", "Custom directory target to configure PATH for")
	installCmd.Flags().BoolVar(&installSkipAliases, "skip-aliases", false, "Bypasses shell profile alias purging")
	installCmd.Flags().BoolVar(&installSkipPath, "skip-path", false, "Bypasses shell profile PATH appending")

	rootCmd.AddCommand(installCmd)
	rootCmd.AddCommand(selfInstallCmd)
}

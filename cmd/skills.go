package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"text/template"

	"github.com/CmfH009/microgravity/internal/config"
	"github.com/CmfH009/microgravity/internal/skills"
	"github.com/CmfH009/microgravity/internal/ui"
	"github.com/spf13/cobra"
)

var skillsCmd = &cobra.Command{
	Use:   "skills",
	Short: "Manage and search Microgravity skills",
	Long:  `List, search, pick, inspect, validate, and create skills that extend Microgravity's capabilities.`,
	Run: func(cmd *cobra.Command, args []string) {
		dir := config.GetSkillsDir()
		loaded, err := skills.LoadAll(dir)
		if err != nil || len(loaded) == 0 {
			fmt.Println(ui.PrintSystemInfo("No skills found or failed to load skills."))
			return
		}
		selected, ok := skills.RunTUI(loaded)
		if ok && selected != nil {
			ui.PrintStepSuccess(fmt.Sprintf("Selected skill: %s (%s)", selected.Name, selected.Description))
		}
	},
}

var skillsSearchCmd = &cobra.Command{
	Use:   "search <query>",
	Short: "Search skills by keyword or topic",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		query := strings.ToLower(args[0])
		dir := config.GetSkillsDir()
		loaded, err := skills.LoadAll(dir)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s\n", ui.PrintError(err))
			os.Exit(1)
		}

		var matches []*skills.Skill
		for _, s := range loaded {
			if strings.Contains(strings.ToLower(s.Name), query) ||
				strings.Contains(strings.ToLower(s.Description), query) ||
				strings.Contains(strings.ToLower(s.Type), query) {
				matches = append(matches, s)
			}
		}

		if len(matches) == 0 {
			fmt.Println(ui.PrintSystemInfo(fmt.Sprintf("No skills found matching %q", query)))
			return
		}

		fmt.Println(ui.PrintHeader(fmt.Sprintf(" Skill Search Results for %q (%d) ", query, len(matches))))
		fmt.Println(ui.PrintSeparator())
		for _, s := range matches {
			fmt.Println(skills.FormatSkillSummary(s))
		}
		fmt.Println(ui.PrintSeparator())
	},
}

var skillsInstallCmd = &cobra.Command{
	Use:   "install <repository-url-or-slug>",
	Short: "Install a skill package from a Git repository",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		target := args[0]
		url := target
		if !strings.HasPrefix(target, "http://") && !strings.HasPrefix(target, "https://") && !strings.HasPrefix(target, "git@") {
			url = "https://github.com/" + target
		}

		home, err := os.UserHomeDir()
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s\n", ui.PrintError(err))
			os.Exit(1)
		}

		baseName := filepath.Base(strings.TrimSuffix(url, ".git"))
		targetDir := filepath.Join(home, ".microgravity", "skills", baseName)

		ui.PrintStepInfo(fmt.Sprintf("Cloning skill repository %s ...", url))

		if _, err := os.Stat(targetDir); err == nil {
			os.RemoveAll(targetDir)
		}

		c := exec.Command("git", "clone", "--depth", "1", url, targetDir)
		out, err := c.CombinedOutput()
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s\n", ui.PrintError(fmt.Errorf("failed to clone repository: %s (%v)", string(out), err)))
			os.Exit(1)
		}

		loaded, _ := skills.LoadDir(targetDir)
		ui.PrintStepSuccess(fmt.Sprintf("Installed %d skill(s) into %s", len(loaded), targetDir))
	},
}

var skillsPickCmd = &cobra.Command{
	Use:   "pick",
	Short: "Open interactive skill selector TUI",
	Run: func(cmd *cobra.Command, args []string) {
		dir := config.GetSkillsDir()
		loaded, err := skills.LoadAll(dir)
		if err != nil || len(loaded) == 0 {
			fmt.Println(ui.PrintSystemInfo("No skills found or failed to load skills."))
			return
		}
		selected, ok := skills.RunTUI(loaded)
		if ok && selected != nil {
			ui.PrintStepSuccess(fmt.Sprintf("Selected skill: %s (%s)", selected.Name, selected.Description))
		}
	},
}

// --- skills list ---
var skillsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all installed skills",
	Run: func(cmd *cobra.Command, args []string) {
		dir := config.GetSkillsDir()
		if dir == "" {
			fmt.Fprintf(os.Stderr, "%s\n", ui.PrintError(fmt.Errorf("could not resolve skills directory")))
			os.Exit(1)
		}

		loaded, err := skills.LoadAll(dir)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s\n", ui.PrintError(fmt.Errorf("failed to load skills: %v", err)))
			os.Exit(1)
		}

		if len(loaded) == 0 {
			fmt.Println(ui.PrintSystemInfo("No skills installed. Run 'microgravity skills add <name>' to create one."))
			return
		}

		fmt.Println(ui.PrintHeader(" Installed Skills "))
		fmt.Println(ui.PrintSeparator())
		for _, s := range loaded {
			fmt.Println(skills.FormatSkillSummary(s))
		}
		fmt.Println(ui.PrintSeparator())
		fmt.Printf("  Total: %d skill(s) in %s\n", len(loaded), dir)
	},
}

// --- skills info ---
var skillsInfoCmd = &cobra.Command{
	Use:   "info <skill-name>",
	Short: "Show details for a specific skill",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		name := args[0]
		dir := config.GetSkillsDir()
		loaded, err := skills.LoadAll(dir)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s\n", ui.PrintError(err))
			os.Exit(1)
		}

		var found *skills.Skill
		for _, s := range loaded {
			if s.Name == name {
				found = s
				break
			}
		}

		if found == nil {
			fmt.Fprintf(os.Stderr, "%s\n", ui.PrintError(fmt.Errorf("skill %q not found", name)))
			os.Exit(1)
		}

		skillType := found.Type
		if skillType == "" {
			skillType = "prompt"
		}

		fmt.Println(ui.PrintHeader(fmt.Sprintf(" Skill: %s ", found.Name)))
		fmt.Println(ui.PrintSeparator())
		fmt.Printf("  Name:        %s\n", found.Name)
		fmt.Printf("  Type:        %s\n", skillType)
		fmt.Printf("  Description: %s\n", found.Description)
		if found.Command != "" {
			fmt.Printf("  Command:     %s\n", found.Command)
		}
		fmt.Println(ui.PrintSeparator())

		issues := skills.Validate(found)
		if len(issues) > 0 {
			fmt.Println("  ⚠️  Validation issues:")
			for _, issue := range issues {
				fmt.Printf("    • %s\n", issue)
			}
		} else {
			fmt.Println("  ✅ Skill is valid")
		}

		fmt.Println(ui.PrintSeparator())
		bodyPreview := found.Body
		if len(bodyPreview) > 300 {
			bodyPreview = bodyPreview[:297] + "..."
		}
		fmt.Printf("  Body:\n%s\n", indent(bodyPreview, "    "))
	},
}

// --- skills validate ---
var skillsValidateCmd = &cobra.Command{
	Use:   "validate",
	Short: "Validate all installed skills",
	Run: func(cmd *cobra.Command, args []string) {
		dir := config.GetSkillsDir()
		loaded, err := skills.LoadAll(dir)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s\n", ui.PrintError(err))
			os.Exit(1)
		}

		if len(loaded) == 0 {
			fmt.Println(ui.PrintSystemInfo("No skills to validate."))
			return
		}

		totalIssues := 0
		for _, s := range loaded {
			issues := skills.Validate(s)
			if len(issues) > 0 {
				fmt.Printf("  ❌ %s:\n", s.Name)
				for _, issue := range issues {
					fmt.Printf("      • %s\n", issue)
				}
				totalIssues += len(issues)
			} else {
				fmt.Printf("  ✅ %s\n", s.Name)
			}
		}

		fmt.Println(ui.PrintSeparator())
		if totalIssues == 0 {
			fmt.Println("  All skills passed validation!")
		} else {
			fmt.Printf("  Found %d issue(s) across %d skill(s)\n", totalIssues, len(loaded))
		}
	},
}

// --- skills add ---
var skillsAddCmd = &cobra.Command{
	Use:   "add <skill-name>",
	Short: "Create a new skill from a template",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		name := args[0]
		dir := config.GetSkillsDir()

		skillType, _ := cmd.Flags().GetString("type")
		if skillType == "" {
			skillType = "prompt"
		}

		filename := filepath.Join(dir, name+".md")

		if _, err := os.Stat(filename); err == nil {
			fmt.Fprintf(os.Stderr, "%s\n", ui.PrintError(fmt.Errorf("skill %q already exists at %s", name, filename)))
			os.Exit(1)
		}

		tmplStr := `---
name: {{.Name}}
description: {{.Description}}
{{- if eq .Type "tool"}}
type: tool
command: {{.Command}}
{{- end}}
---
# {{.Title}}

{{.BodyPlaceholder}}
`
		data := map[string]string{
			"Name":            name,
			"Description":     "TODO: Add a description",
			"Type":            skillType,
			"Command":         "echo hello",
			"Title":           strings.ReplaceAll(strings.Title(strings.ReplaceAll(name, "-", " ")), " ", " "),
			"BodyPlaceholder": "Write your skill instructions here.",
		}

		tmpl, err := template.New("skill").Parse(tmplStr)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s\n", ui.PrintError(err))
			os.Exit(1)
		}

		f, err := os.Create(filename)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s\n", ui.PrintError(err))
			os.Exit(1)
		}
		defer f.Close()

		if err := tmpl.Execute(f, data); err != nil {
			fmt.Fprintf(os.Stderr, "%s\n", ui.PrintError(err))
			os.Exit(1)
		}

		fmt.Printf("  ✨ Created skill %q at %s\n", name, filename)
		fmt.Printf("  Edit the file to customize your skill, then run 'microgravity skills validate'\n")
	},
}

// --- skills remove ---
var skillsRemoveCmd = &cobra.Command{
	Use:   "remove <skill-name>",
	Short: "Remove an installed skill",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		name := args[0]
		dir := config.GetSkillsDir()

		filename := filepath.Join(dir, name+".md")
		if _, err := os.Stat(filename); os.IsNotExist(err) {
			fmt.Fprintf(os.Stderr, "%s\n", ui.PrintError(fmt.Errorf("skill %q not found at %s", name, filename)))
			os.Exit(1)
		}

		if err := os.Remove(filename); err != nil {
			fmt.Fprintf(os.Stderr, "%s\n", ui.PrintError(err))
			os.Exit(1)
		}

		fmt.Printf("  🗑️  Removed skill %q\n", name)
	},
}

func indent(text, prefix string) string {
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		lines[i] = prefix + line
	}
	return strings.Join(lines, "\n")
}

func init() {
	skillsAddCmd.Flags().String("type", "prompt", "Skill type: 'prompt' or 'tool'")

	skillsCmd.AddCommand(skillsListCmd)
	skillsCmd.AddCommand(skillsSearchCmd)
	skillsCmd.AddCommand(skillsPickCmd)
	skillsCmd.AddCommand(skillsInstallCmd)
	skillsCmd.AddCommand(skillsInfoCmd)
	skillsCmd.AddCommand(skillsValidateCmd)
	skillsCmd.AddCommand(skillsAddCmd)
	skillsCmd.AddCommand(skillsRemoveCmd)
	rootCmd.AddCommand(skillsCmd)
}

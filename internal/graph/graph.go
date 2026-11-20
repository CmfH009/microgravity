package graph

import (
	"fmt"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/purav/microgravity/internal/skills"
)

// PackageNode describes a Go package and its internal dependencies.
type PackageNode struct {
	Name       string
	RelPath    string
	ImportPath string
	Imports    []string
}

// SkillNode describes a discovered skill and its summary.
type SkillNode struct {
	Name        string
	Description string
	Path        string
}

// GraphReport contains the parsed project architecture overview.
type GraphReport struct {
	Packages []PackageNode
	Skills   []SkillNode
	Commands []string
}

// BuildPackageGraph scans rootDir for Go packages and parses internal package imports.
func BuildPackageGraph(rootDir string) (*GraphReport, error) {
	var pkgs []PackageNode
	fset := token.NewFileSet()

	err := filepath.Walk(rootDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			// Skip hidden dirs and vendor/worktrees
			base := info.Name()
			if (strings.HasPrefix(base, ".") || base == "vendor" || base == "worktrees") && path != rootDir {
				return filepath.SkipDir
			}

			pkgsParsed, errParse := parser.ParseDir(fset, path, nil, parser.ImportsOnly)
			if errParse != nil || len(pkgsParsed) == 0 {
				return nil
			}

			relPath, _ := filepath.Rel(rootDir, path)
			if relPath == "." {
				relPath = "root"
			}

			for pkgName, astPkg := range pkgsParsed {
				if strings.HasSuffix(pkgName, "_test") {
					continue
				}

				importSet := make(map[string]bool)
				for _, fileAst := range astPkg.Files {
					for _, imp := range fileAst.Imports {
						if imp.Path != nil {
							cleanImp := strings.Trim(imp.Path.Value, `"`)
							if strings.Contains(cleanImp, "microgravity") {
								importSet[cleanImp] = true
							}
						}
					}
				}

				var impList []string
				for imp := range importSet {
					impList = append(impList, imp)
				}
				sort.Strings(impList)

				pkgs = append(pkgs, PackageNode{
					Name:       pkgName,
					RelPath:    relPath,
					ImportPath: "github.com/purav/microgravity/" + relPath,
					Imports:    impList,
				})
			}
		}
		return nil
	})

	if err != nil {
		return nil, err
	}

	sort.Slice(pkgs, func(i, j int) bool {
		return pkgs[i].RelPath < pkgs[j].RelPath
	})

	// Load skills
	var skillNodes []SkillNode
	if loadedSkills, errSkills := skills.LoadAll(rootDir); errSkills == nil {
		for _, s := range loadedSkills {
			if s != nil && s.Name != "" {
				skillNodes = append(skillNodes, SkillNode{
					Name:        s.Name,
					Description: s.Description,
				})
			}
		}
	}

	sort.Slice(skillNodes, func(i, j int) bool {
		return skillNodes[i].Name < skillNodes[j].Name
	})

	cmds := []string{"audit", "auth", "chat", "dashboard", "fix", "graph", "index", "install", "mcp", "models", "paste", "plan", "preview", "pull", "query", "run", "serve", "sessions", "setup", "skills", "status", "subagents", "workflow"}

	return &GraphReport{
		Packages: pkgs,
		Skills:   skillNodes,
		Commands: cmds,
	}, nil
}

// RenderLipGlossGraph renders a tree visualizer of the report based on display mode ("all", "packages", "skills").
func RenderLipGlossGraph(report *GraphReport, mode string) string {
	boxStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("63")).
		Padding(1, 2)

	titleStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("213")).Bold(true)
	headerStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("39")).Bold(true)
	nodeStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("120")).Bold(true)
	depStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("246"))
	badgeStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("255")).Background(lipgloss.Color("57")).Padding(0, 1).Bold(true)

	var sb strings.Builder
	sb.WriteString(titleStyle.Render("📊 MICROGRAVITY DEPENDENCY & ARCHITECTURE GRAPH") + "\n\n")

	if mode == "all" || mode == "packages" {
		sb.WriteString(headerStyle.Render("📦 GO PACKAGE DEPENDENCY TREE") + " " + badgeStyle.Render(fmt.Sprintf("%d Packages", len(report.Packages))) + "\n")
		for i, pkg := range report.Packages {
			isLastPkg := (i == len(report.Packages)-1)
			pkgPrefix := "├── "
			if isLastPkg {
				pkgPrefix = "└── "
			}
			sb.WriteString(fmt.Sprintf("%s%s (%s)\n", pkgPrefix, nodeStyle.Render(pkg.RelPath), pkg.Name))

			childIndent := "│   "
			if isLastPkg {
				childIndent = "    "
			}

			if len(pkg.Imports) == 0 {
				sb.WriteString(fmt.Sprintf("%s└── %s\n", childIndent, depStyle.Render("<no internal dependencies>")))
			} else {
				for j, imp := range pkg.Imports {
					isLastImp := (j == len(pkg.Imports)-1)
					impPrefix := "├── "
					if isLastImp {
						impPrefix = "└── "
					}
					// Trim prefix for readability
					shortImp := strings.TrimPrefix(imp, "github.com/purav/microgravity/")
					sb.WriteString(fmt.Sprintf("%s%s%s\n", childIndent, impPrefix, depStyle.Render(shortImp)))
				}
			}
		}
		sb.WriteString("\n")
	}

	if mode == "all" || mode == "skills" {
		sb.WriteString(headerStyle.Render("🧩 DISCOVERED AGY SKILLS") + " " + badgeStyle.Render(fmt.Sprintf("%d Skills", len(report.Skills))) + "\n")
		for i, sk := range report.Skills {
			isLast := (i == len(report.Skills)-1)
			prefix := "├── "
			if isLast {
				prefix = "└── "
			}
			desc := sk.Description
			if len(desc) > 50 {
				desc = desc[:47] + "..."
			}
			if desc != "" {
				sb.WriteString(fmt.Sprintf("%s%s — %s\n", prefix, nodeStyle.Render(sk.Name), depStyle.Render(desc)))
			} else {
				sb.WriteString(fmt.Sprintf("%s%s\n", prefix, nodeStyle.Render(sk.Name)))
			}
		}
		sb.WriteString("\n")
	}

	if mode == "all" {
		sb.WriteString(headerStyle.Render("⚙️ CLI COMMAND HIERARCHY") + " " + badgeStyle.Render(fmt.Sprintf("%d Commands", len(report.Commands))) + "\n")
		sb.WriteString("└── microgravity\n")
		for i, cmdName := range report.Commands {
			isLast := (i == len(report.Commands)-1)
			prefix := "    ├── "
			if isLast {
				prefix = "    └── "
			}
			sb.WriteString(fmt.Sprintf("%s%s\n", prefix, nodeStyle.Render(cmdName)))
		}
	}

	return boxStyle.Render(sb.String())
}

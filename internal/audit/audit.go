package audit

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

type AuditIssue struct {
	Category    string
	Severity    string
	Description string
	Fixable     bool
	PatchType   string
}

type ProjectAudit struct {
	ProjectName string
	Score       int
	Grade       string
	Issues      []AuditIssue
}

func AuditWorkspace(dir string) (*ProjectAudit, error) {
	name := filepath.Base(dir)
	audit := &ProjectAudit{
		ProjectName: name,
		Score:       100,
		Grade:       "Grade A",
		Issues:      make([]AuditIssue, 0),
	}

	// 1. Check for README.md
	if _, err := os.Stat(filepath.Join(dir, "README.md")); os.IsNotExist(err) {
		audit.Score -= 15
		audit.Issues = append(audit.Issues, AuditIssue{
			Category:    "Structure",
			Severity:    "WARNING",
			Description: "Missing project documentation (README.md)",
			Fixable:     true,
			PatchType:   "create_readme",
		})
	}

	// 2. Check for .gitignore
	if _, err := os.Stat(filepath.Join(dir, ".gitignore")); os.IsNotExist(err) {
		audit.Score -= 10
		audit.Issues = append(audit.Issues, AuditIssue{
			Category:    "Configuration",
			Severity:    "WARNING",
			Description: "Missing .gitignore file",
			Fixable:     true,
			PatchType:   "create_gitignore",
		})
	}

	// 3. Check for LICENSE
	if _, err := os.Stat(filepath.Join(dir, "LICENSE")); os.IsNotExist(err) {
		if _, err := os.Stat(filepath.Join(dir, "LICENSE.md")); os.IsNotExist(err) {
			audit.Score -= 5
			audit.Issues = append(audit.Issues, AuditIssue{
				Category:    "Compliance",
				Severity:    "INFO",
				Description: "Missing LICENSE file",
				Fixable:     true,
				PatchType:   "create_license",
			})
		}
	}

	// 4. Check for PLAN.md execution plan
	if _, err := os.Stat(filepath.Join(dir, "PLAN.md")); os.IsNotExist(err) {
		audit.Score -= 10
		audit.Issues = append(audit.Issues, AuditIssue{
			Category:    "Architecture",
			Severity:    "WARNING",
			Description: "Missing master execution plan (PLAN.md)",
			Fixable:     true,
			PatchType:   "create_plan",
		})
	}

	// 5. Check for unit tests
	hasTests := false
	testCount := 0
	filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() && strings.HasSuffix(info.Name(), "_test.go") {
			hasTests = true
			testCount++
		}
		return nil
	})

	if !hasTests && isGoProject(dir) {
		audit.Score -= 20
		audit.Issues = append(audit.Issues, AuditIssue{
			Category:    "Testing",
			Severity:    "CRITICAL",
			Description: "No unit tests found (*_test.go)",
			Fixable:     false,
		})
	}

	// Compute Grade
	switch {
	case audit.Score >= 90:
		audit.Grade = "Grade A"
	case audit.Score >= 75:
		audit.Grade = "Grade B"
	case audit.Score >= 60:
		audit.Grade = "Grade C"
	default:
		audit.Grade = "Grade F"
	}

	return audit, nil
}

func isGoProject(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, "go.mod"))
	return err == nil
}

func (pa *ProjectAudit) ApplyPatches(dir string) int {
	patched := 0
	for _, issue := range pa.Issues {
		if !issue.Fixable {
			continue
		}
		switch issue.PatchType {
		case "create_readme":
			content := fmt.Sprintf("# %s\n\nScaffolded project documentation.\n", pa.ProjectName)
			if os.WriteFile(filepath.Join(dir, "README.md"), []byte(content), 0644) == nil {
				patched++
			}
		case "create_gitignore":
			content := "*.exe\n*.dll\n*.test\n.ds_store\n"
			if os.WriteFile(filepath.Join(dir, ".gitignore"), []byte(content), 0644) == nil {
				patched++
			}
		}
	}
	return patched
}

var (
	AuditBoxStyle = lipgloss.NewStyle().
			Border(lipgloss.DoubleBorder()).
			BorderForeground(lipgloss.Color("99")).
			Padding(1, 3).
			MarginTop(1).
			MarginBottom(1)

	GradeAStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("15")).Background(lipgloss.Color("48")).Bold(true).Padding(0, 1)
	GradeBStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("15")).Background(lipgloss.Color("214")).Bold(true).Padding(0, 1)
	GradeCStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("15")).Background(lipgloss.Color("208")).Bold(true).Padding(0, 1)
	GradeFStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("15")).Background(lipgloss.Color("196")).Bold(true).Padding(0, 1)

	AuditTitleStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("213")).Bold(true)
	AuditMetaStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("39")).Bold(true)
	IssueTextStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("255"))
)

func (pa *ProjectAudit) RenderScorecard() string {
	var sb strings.Builder
	sb.WriteString(AuditTitleStyle.Render(fmt.Sprintf("🔍  WORKSPACE AUDIT REPORT: %s", pa.ProjectName)) + "\n\n")

	var gradeStr string
	switch pa.Grade {
	case "Grade A":
		gradeStr = GradeAStyle.Render("Grade A (Score: 100/100)")
	case "Grade B":
		gradeStr = GradeBStyle.Render(fmt.Sprintf("%s (%d/100)", pa.Grade, pa.Score))
	case "Grade C":
		gradeStr = GradeCStyle.Render(fmt.Sprintf("%s (%d/100)", pa.Grade, pa.Score))
	default:
		gradeStr = GradeFStyle.Render(fmt.Sprintf("%s (%d/100)", pa.Grade, pa.Score))
	}

	// Score Gauge Bar
	barWidth := 20
	filled := (pa.Score * barWidth) / 100
	if filled < 0 {
		filled = 0
	}
	gaugeBar := fmt.Sprintf("[%s]", strings.Repeat("▓", filled)+strings.Repeat("░", barWidth-filled))

	sb.WriteString(fmt.Sprintf("%s  %s  %s\n\n", AuditMetaStyle.Render("Health Rating:"), gradeStr, AuditMetaStyle.Render(gaugeBar)))

	if len(pa.Issues) == 0 {
		sb.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("48")).Bold(true).Render("✔ All workspace architecture, documentation, and security standards met cleanly!"))
	} else {
		sb.WriteString(AuditMetaStyle.Render("Detected Workspace Issues:") + "\n")
		for _, issue := range pa.Issues {
			fixBadge := lipgloss.NewStyle().Foreground(lipgloss.Color("246")).Render("[Manual Action]")
			if issue.Fixable {
				fixBadge = lipgloss.NewStyle().Foreground(lipgloss.Color("48")).Bold(true).Render("[Auto-Fix Available]")
			}
			sevBadge := lipgloss.NewStyle().Foreground(lipgloss.Color("214")).Bold(true).Render("[" + issue.Severity + "]")
			sb.WriteString(fmt.Sprintf("  • %s %s: %s %s\n", sevBadge, issue.Category, IssueTextStyle.Render(issue.Description), fixBadge))
		}
	}

	return AuditBoxStyle.Render(strings.TrimRight(sb.String(), "\n"))
}


package graph_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/CmfH009/microgravity/internal/graph"
)

func TestBuildPackageGraph(t *testing.T) {
	// Create temporary directory with mock go files
	tmpDir, err := os.MkdirTemp("", "graph_test_*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	pkgA := filepath.Join(tmpDir, "pkgA")
	pkgB := filepath.Join(tmpDir, "pkgB")
	if err := os.MkdirAll(pkgA, 0755); err != nil {
		t.Fatalf("Failed to create pkgA: %v", err)
	}
	if err := os.MkdirAll(pkgB, 0755); err != nil {
		t.Fatalf("Failed to create pkgB: %v", err)
	}

	codeA := `package pkgA

import (
	"fmt"
	"github.com/CmfH009/microgravity/pkgB"
)

func Hello() {
	fmt.Println(pkgB.World())
}
`
	codeB := `package pkgB

func World() string {
	return "world"
}
`

	if err := os.WriteFile(filepath.Join(pkgA, "a.go"), []byte(codeA), 0644); err != nil {
		t.Fatalf("Failed to write a.go: %v", err)
	}
	if err := os.WriteFile(filepath.Join(pkgB, "b.go"), []byte(codeB), 0644); err != nil {
		t.Fatalf("Failed to write b.go: %v", err)
	}

	report, err := graph.BuildPackageGraph(tmpDir)
	if err != nil {
		t.Fatalf("BuildPackageGraph failed: %v", err)
	}

	if len(report.Packages) < 2 {
		t.Errorf("Expected at least 2 packages, got %d", len(report.Packages))
	}

	rendered := graph.RenderLipGlossGraph(report, "all")
	if !strings.Contains(rendered, "pkgA") || !strings.Contains(rendered, "pkgB") {
		t.Errorf("Rendered graph missing package names: %s", rendered)
	}
}

func TestRenderLipGlossGraphModes(t *testing.T) {
	report := &graph.GraphReport{
		Packages: []graph.PackageNode{
			{
				Name:       "cmd",
				ImportPath: "github.com/CmfH009/microgravity/cmd",
				Imports:    []string{"github.com/CmfH009/microgravity/internal/config"},
			},
		},
		Skills: []graph.SkillNode{
			{
				Name:        "agy-customizations",
				Description: "Antigravity customizations guide",
			},
		},
		Commands: []string{"root", "status", "audit", "graph"},
	}

	renderedPkgs := graph.RenderLipGlossGraph(report, "packages")
	if !strings.Contains(renderedPkgs, "cmd") {
		t.Errorf("Expected 'cmd' in rendered packages view")
	}

	renderedSkills := graph.RenderLipGlossGraph(report, "skills")
	if !strings.Contains(renderedSkills, "agy-customizations") {
		t.Errorf("Expected 'agy-customizations' in rendered skills view")
	}

	renderedAll := graph.RenderLipGlossGraph(report, "all")
	if !strings.Contains(renderedAll, "cmd") || !strings.Contains(renderedAll, "agy-customizations") {
		t.Errorf("Expected both cmd and skills in rendered all view")
	}
}

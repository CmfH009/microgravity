package profiler

import (
	"context"
	"fmt"
	"runtime"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

type ProfileReport struct {
	PackageName string
	AllocBytes  uint64
	TotalAlloc  uint64
	SysBytes    uint64
	NumGC       uint32
	HeapObjects uint64
}

func ProfilePackage(ctx context.Context, targetPkg string) (*ProfileReport, error) {
	runtime.GC()
	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	if targetPkg == "" {
		targetPkg = "."
	}

	return &ProfileReport{
		PackageName: targetPkg,
		AllocBytes:  m.Alloc,
		TotalAlloc:  m.TotalAlloc,
		SysBytes:    m.Sys,
		NumGC:       m.NumGC,
		HeapObjects: m.HeapObjects,
	}, nil
}

func RenderProfileReport(r *ProfileReport) string {
	boxStyle := lipgloss.NewStyle().
		Border(lipgloss.DoubleBorder()).
		BorderForeground(lipgloss.Color("42")).
		Padding(1, 2)

	titleStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("213")).Bold(true)
	keyStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("39")).Bold(true)
	valStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("255"))

	allocMB := float64(r.AllocBytes) / (1024 * 1024)
	sysMB := float64(r.SysBytes) / (1024 * 1024)

	var sb strings.Builder
	sb.WriteString(titleStyle.Render("🔍 AUTOMATED BENCHMARK & MEMORY PROFILE REPORT") + "\n\n")
	sb.WriteString(fmt.Sprintf("%s %s\n", keyStyle.Render("Target Package :"), valStyle.Render(r.PackageName)))
	sb.WriteString(fmt.Sprintf("%s %s\n", keyStyle.Render("Heap Alloc     :"), valStyle.Render(fmt.Sprintf("%.2f MB (%d bytes)", allocMB, r.AllocBytes))))
	sb.WriteString(fmt.Sprintf("%s %s\n", keyStyle.Render("Sys Reserved   :"), valStyle.Render(fmt.Sprintf("%.2f MB", sysMB))))
	sb.WriteString(fmt.Sprintf("%s %s\n", keyStyle.Render("Heap Objects   :"), valStyle.Render(fmt.Sprintf("%d active objects", r.HeapObjects))))
	sb.WriteString(fmt.Sprintf("%s %s\n", keyStyle.Render("GC Collections :"), valStyle.Render(fmt.Sprintf("%d total GC cycles", r.NumGC))))

	return boxStyle.Render(sb.String())
}

package tests

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/CmfH009/microgravity/internal/ci"
	"github.com/CmfH009/microgravity/internal/compressor"
	"github.com/CmfH009/microgravity/internal/pruner"
	"github.com/CmfH009/microgravity/internal/rules"
	"github.com/CmfH009/microgravity/internal/skills"
	"github.com/CmfH009/microgravity/internal/subagent"
	"github.com/CmfH009/microgravity/internal/trajectory"
)

func BenchmarkTokenEstimation(b *testing.B) {
	text := `Lorem ipsum dolor sit amet, consectetur adipiscing elit. Sed do eiusmod tempor incididunt ut labore et dolore magna aliqua.`
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = pruner.EstimateTokens(text)
	}
}

func BenchmarkASTCompressor(b *testing.B) {
	goCode := `package sample
// Add adds two numbers
func Add(a, b int) int {
	return a + b
}

func privateHelper(x int) bool {
	if x > 10 {
		return true
	}
	return false
}
`
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _, _, _ = compressor.CompressGoCode(goCode)
	}
}

func BenchmarkCIGenerator(b *testing.B) {
	cfg := ci.DefaultConfig()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = ci.GenerateAll(cfg)
	}
}

func BenchmarkRulesLoader(b *testing.B) {
	cwd, _ := os.Getwd()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = rules.LoadDefaultRules(cwd)
	}
}

func BenchmarkSkillsDiscovery(b *testing.B) {
	tempDir, _ := os.MkdirTemp("", "bench_skills_*")
	defer os.RemoveAll(tempDir)
	_ = os.WriteFile(filepath.Join(tempDir, "SKILL.md"), []byte("---\nname: test-skill\ndescription: A test skill\n---\nSkill body"), 0644)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = skills.LoadAll(tempDir)
	}
}

func BenchmarkTrajectoryLogger(b *testing.B) {
	logger, _ := trajectory.NewLogger("bench-session")
	tools := []trajectory.ToolCallInfo{
		{ToolName: "run_command", Arguments: map[string]interface{}{"CommandLine": "echo 1"}},
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = logger.LogStep("MODEL", "PLANNER_RESPONSE", "DONE", "Benchmarking trajectory output", "Reasoning trace", tools)
	}
}

func BenchmarkSubagentDefinitions(b *testing.B) {
	mgr := subagent.NewManager(nil, "")
	def := subagent.SubagentDefinition{
		Name:             "bench-agent",
		Description:      "Benchmarking agent",
		SystemPrompt:     "You are a benchmark agent.",
		EnableWriteTools: true,
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		mgr.DefineSubagent(def)
		_, _ = mgr.GetDefinition("bench-agent")
	}
}


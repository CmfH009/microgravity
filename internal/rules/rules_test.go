package rules

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadRules(t *testing.T) {
	tempDir := t.TempDir()

	globalRuleFile := filepath.Join(tempDir, "global_rules.md")
	globalContent := "# Global Rules\n- Rule 1: Always verify code.\n"
	if err := os.WriteFile(globalRuleFile, []byte(globalContent), 0644); err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	workspaceRuleFile := filepath.Join(tempDir, "workspace_rules.md")
	workspaceContent := "# Workspace Rules\n- Rule 2: Use Go 1.22+ conventions.\n"
	if err := os.WriteFile(workspaceRuleFile, []byte(workspaceContent), 0644); err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	r, err := LoadFromFiles(globalRuleFile, workspaceRuleFile)
	if err != nil {
		t.Fatalf("LoadFromFiles returned unexpected error: %v", err)
	}

	prompt := r.FormatForSystemPrompt()
	if prompt == "" {
		t.Errorf("Expected formatted system prompt, got empty string")
	}

	if !containsSubstring(prompt, "Rule 1: Always verify code.") {
		t.Errorf("System prompt missing global rule content")
	}
	if !containsSubstring(prompt, "Rule 2: Use Go 1.22+ conventions.") {
		t.Errorf("System prompt missing workspace rule content")
	}
}

func containsSubstring(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || searchSub(s, sub))
}

func searchSub(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func setupProfileTestEnv(t *testing.T) (fakeHome, cwd string) {
	t.Helper()
	fakeHome = t.TempDir()
	cwd = t.TempDir()
	t.Setenv("HOME", fakeHome)
	t.Setenv("USERPROFILE", fakeHome)
	t.Setenv("MG_HOME", "")
	t.Setenv("AGY_HOME", "")
	return fakeHome, cwd
}

func TestLoadDefaultRules_MGHome(t *testing.T) {
	_, cwd := setupProfileTestEnv(t)
	mgHome := t.TempDir()
	t.Setenv("MG_HOME", mgHome)

	if err := os.WriteFile(filepath.Join(mgHome, "USER_PROFILE.md"), []byte("mg-user-profile"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(mgHome, "AGENT_PROFILE.md"), []byte("mg-agent-profile"), 0644); err != nil {
		t.Fatal(err)
	}

	r, err := LoadDefaultRules(cwd)
	if err != nil {
		t.Fatalf("LoadDefaultRules failed: %v", err)
	}
	if r.UserProfile != "mg-user-profile" {
		t.Errorf("expected UserProfile 'mg-user-profile', got %q", r.UserProfile)
	}
	if r.AgentProfile != "mg-agent-profile" {
		t.Errorf("expected AgentProfile 'mg-agent-profile', got %q", r.AgentProfile)
	}
}

func TestLoadDefaultRules_AGYHome(t *testing.T) {
	_, cwd := setupProfileTestEnv(t)
	agyHome := t.TempDir()
	t.Setenv("AGY_HOME", agyHome)

	if err := os.WriteFile(filepath.Join(agyHome, "user_profile.md"), []byte("agy-user-profile"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(agyHome, "agent_profile.md"), []byte("agy-agent-profile"), 0644); err != nil {
		t.Fatal(err)
	}

	r, err := LoadDefaultRules(cwd)
	if err != nil {
		t.Fatalf("LoadDefaultRules failed: %v", err)
	}
	if r.UserProfile != "agy-user-profile" {
		t.Errorf("expected UserProfile 'agy-user-profile', got %q", r.UserProfile)
	}
	if r.AgentProfile != "agy-agent-profile" {
		t.Errorf("expected AgentProfile 'agy-agent-profile', got %q", r.AgentProfile)
	}
}

func TestLoadDefaultRules_MicrogravityHome(t *testing.T) {
	fakeHome, cwd := setupProfileTestEnv(t)
	mgDir := filepath.Join(fakeHome, ".microgravity")
	if err := os.MkdirAll(mgDir, 0755); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(mgDir, "user_profile.md"), []byte("home-mg-user"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(mgDir, "AGENT_PROFILE.md"), []byte("home-mg-agent"), 0644); err != nil {
		t.Fatal(err)
	}

	r, err := LoadDefaultRules(cwd)
	if err != nil {
		t.Fatalf("LoadDefaultRules failed: %v", err)
	}
	if r.UserProfile != "home-mg-user" {
		t.Errorf("expected UserProfile 'home-mg-user', got %q", r.UserProfile)
	}
	if r.AgentProfile != "home-mg-agent" {
		t.Errorf("expected AgentProfile 'home-mg-agent', got %q", r.AgentProfile)
	}
}

func TestLoadDefaultRules_GeminiAntigravityCLIAndPrecedence(t *testing.T) {
	fakeHome, cwd := setupProfileTestEnv(t)
	geminiDir := filepath.Join(fakeHome, ".gemini", "antigravity-cli")
	if err := os.MkdirAll(geminiDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(geminiDir, "user_profile.md"), []byte("gemini-user"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(geminiDir, "agent_profile.md"), []byte("gemini-agent"), 0644); err != nil {
		t.Fatal(err)
	}

	r, err := LoadDefaultRules(cwd)
	if err != nil {
		t.Fatalf("LoadDefaultRules failed: %v", err)
	}
	if r.UserProfile != "gemini-user" || r.AgentProfile != "gemini-agent" {
		t.Errorf("expected gemini fallback profiles, got user=%q agent=%q", r.UserProfile, r.AgentProfile)
	}

	// MG_HOME should take precedence over fallback directories
	mgHome := t.TempDir()
	t.Setenv("MG_HOME", mgHome)
	if err := os.WriteFile(filepath.Join(mgHome, "USER_PROFILE.md"), []byte("mg-override-user"), 0644); err != nil {
		t.Fatal(err)
	}
	r, err = LoadDefaultRules(cwd)
	if err != nil {
		t.Fatalf("LoadDefaultRules failed: %v", err)
	}
	if r.UserProfile != "mg-override-user" {
		t.Errorf("expected MG_HOME override, got %q", r.UserProfile)
	}
	if r.AgentProfile != "gemini-agent" {
		t.Errorf("expected fallback AgentProfile 'gemini-agent', got %q", r.AgentProfile)
	}
}

func TestRulesGoNoHardcodedPaths(t *testing.T) {
	data, err := os.ReadFile("rules.go")
	if err != nil {
		t.Fatalf("failed to read rules.go: %v", err)
	}
	src := string(data)
	lower := strings.ToLower(src)

	forbidden := []string{
		`c:\agyhut`,
		`c:/agyhut`,
		`agyhut`,
		`a:\`,
		`a:/`,
	}
	for _, needle := range forbidden {
		if strings.Contains(lower, needle) {
			t.Errorf("rules.go contains forbidden hardcoded path/string %q", needle)
		}
	}
}

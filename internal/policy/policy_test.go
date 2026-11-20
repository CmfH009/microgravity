package policy

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolvePathRejectsOutsideWorkspace(t *testing.T) {
	root := t.TempDir()
	p := New([]string{root}, false, false)

	inside := filepath.Join(root, "src", "main.go")
	if got, err := p.ResolvePath(inside); err != nil || got != inside {
		t.Fatalf("expected inside path to be allowed, got %q, %v", got, err)
	}

	outside := filepath.Join(t.TempDir(), "secret.txt")
	if _, err := p.ResolvePath(outside); err == nil {
		t.Fatal("expected outside path to be rejected")
	}
}

func TestCommandPolicyRequiresExplicitHostPermission(t *testing.T) {
	p := New([]string{t.TempDir()}, false, false)
	if err := p.CheckCommand("echo hello"); err == nil {
		t.Fatal("expected host command to be rejected by default")
	}

	allowed := New([]string{t.TempDir()}, true, false)
	if err := allowed.CheckCommand("echo hello"); err != nil {
		t.Fatalf("expected explicitly allowed command, got %v", err)
	}
}

func TestResolvePathWithAddDirs(t *testing.T) {
	primaryRoot := t.TempDir()
	extraDir1 := t.TempDir()
	extraDir2 := t.TempDir()
	unapprovedDir := t.TempDir()

	roots := []string{primaryRoot, extraDir1, extraDir2}
	p := New(roots, true, false)

	// Primary root path
	path1 := filepath.Join(primaryRoot, "file1.txt")
	if got, err := p.ResolvePath(path1); err != nil || got != path1 {
		t.Fatalf("expected path in primary root to be resolved, got %q, %v", got, err)
	}

	// Extra dir 1 path
	path2 := filepath.Join(extraDir1, "sub", "file2.txt")
	if got, err := p.ResolvePath(path2); err != nil || got != path2 {
		t.Fatalf("expected path in extraDir1 to be resolved, got %q, %v", got, err)
	}

	// Extra dir 2 path
	path3 := filepath.Join(extraDir2, "file3.txt")
	if got, err := p.ResolvePath(path3); err != nil || got != path3 {
		t.Fatalf("expected path in extraDir2 to be resolved, got %q, %v", got, err)
	}

	// Unapproved dir path
	path4 := filepath.Join(unapprovedDir, "file4.txt")
	if _, err := p.ResolvePath(path4); err == nil {
		t.Fatalf("expected unapproved dir to be rejected")
	}
}

func TestSandboxRejectsCommands(t *testing.T) {
	root := t.TempDir()
	// When sandboxEnabled is true, CheckCommand must reject commands even if allowHostCommands is true
	p := New([]string{root}, true, true)
	if !p.SandboxEnabled() {
		t.Fatalf("expected SandboxEnabled() to return true")
	}
	if err := p.CheckCommand("echo dangerous"); err == nil {
		t.Fatalf("expected sandbox to reject command execution")
	}

	pStrict := New([]string{root}, false, true)
	if err := pStrict.CheckCommand("echo dangerous"); err == nil {
		t.Fatalf("expected sandbox with allowHostCommands=false to reject command execution")
	}
}

func TestCheckToolDenyList(t *testing.T) {
	p := New([]string{t.TempDir()}, true, false)
	p.WithToolRules([]string{"run_command"}, nil, nil)

	if err := p.CheckTool("view_file"); err != nil {
		t.Fatalf("expected view_file to be allowed, got %v", err)
	}
	if err := p.CheckTool("run_command"); err == nil {
		t.Fatal("expected run_command to be denied")
	}
}

func TestCheckToolAllowList(t *testing.T) {
	p := New([]string{t.TempDir()}, true, false)
	p.WithToolRules(nil, []string{"view_file", "list_dir"}, nil)

	if err := p.CheckTool("view_file"); err != nil {
		t.Fatalf("expected view_file to be allowed by allow list, got %v", err)
	}
	if err := p.CheckTool("run_command"); err == nil {
		t.Fatal("expected run_command to be denied (not in allow list)")
	}
}

func TestCheckToolDenyTakesPrecedence(t *testing.T) {
	p := New([]string{t.TempDir()}, true, false)
	p.WithToolRules([]string{"run_command"}, []string{"run_command"}, nil)

	if err := p.CheckTool("run_command"); err == nil {
		t.Fatal("expected run_command to be denied even though in allow list")
	}
}

func TestCheckPathDenied(t *testing.T) {
	root := t.TempDir()
	secretFile := filepath.Join(root, "secrets", "id_rsa")

	p := New([]string{root}, true, false)
	p.WithToolRules(nil, nil, []string{"*/secrets/*"})

	if err := p.CheckPathDenied(secretFile); err == nil {
		t.Fatal("expected path matching deny pattern to be rejected")
	}
	if err := p.CheckPathDenied(filepath.Join(root, "safe.txt")); err != nil {
		t.Fatalf("expected non-matching path to be allowed, got %v", err)
	}
}

func TestToolRulesFromEnv(t *testing.T) {
	os.Setenv("MG_POLICY_DENY_TOOLS", "run_command, manage_task")
	os.Setenv("MG_POLICY_ALLOW_TOOLS", "view_file")
	os.Setenv("MG_POLICY_DENY_PATHS", "*/secrets/*")
	defer func() {
		os.Unsetenv("MG_POLICY_DENY_TOOLS")
		os.Unsetenv("MG_POLICY_ALLOW_TOOLS")
		os.Unsetenv("MG_POLICY_DENY_PATHS")
	}()

	deny, allow, paths := ToolRulesFromEnv()

	found := false
	for _, t := range deny {
		if t == "run_command" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected run_command in deny list from env")
	}

	found = false
	for _, t := range allow {
		if t == "view_file" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected view_file in allow list from env")
	}

	if len(paths) != 1 || paths[0] != "*/secrets/*" {
		t.Fatalf("expected deny paths from env, got %v", paths)
	}
}

func TestToolRulesFromEnvEmpty(t *testing.T) {
	for _, k := range []string{"MG_POLICY_DENY_TOOLS", "MG_POLICY_ALLOW_TOOLS", "MG_POLICY_DENY_PATHS"} {
		os.Unsetenv(k)
	}

	deny, allow, paths := ToolRulesFromEnv()
	if len(deny) != 0 || len(allow) != 0 || len(paths) != 0 {
		t.Fatalf("expected empty rules when env unset, got deny=%v allow=%v paths=%v", deny, allow, paths)
	}
}

func TestWithToolRulesTrimsSpaces(t *testing.T) {
	p := New([]string{t.TempDir()}, true, false)
	p.WithToolRules([]string{" run_command ", "list_dir"}, nil, nil)

	if err := p.CheckTool("run_command"); err == nil {
		t.Fatal("expected trimmed run_command to be denied")
	}
	if err := p.CheckTool("list_dir"); err == nil {
		t.Fatal("expected trimmed list_dir to be denied")
	}
}

func TestCheckToolNilPolicy(t *testing.T) {
	var p *Policy
	if err := p.CheckTool("view_file"); err == nil {
		t.Fatal("expected nil policy to return error")
	}
}


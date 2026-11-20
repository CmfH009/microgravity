package policy

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
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

func TestResolvePathTraversalAndBoundaryBypass(t *testing.T) {
	base := t.TempDir()
	repo := filepath.Join(base, "repo")
	repoEvil := filepath.Join(base, "repo-evil")
	outside := filepath.Join(base, "outside")
	for _, d := range []string{
		filepath.Join(repo, "sub"),
		filepath.Join(repo, "secrets"),
		repoEvil,
		outside,
	} {
		if err := os.MkdirAll(d, 0755); err != nil {
			t.Fatal(err)
		}
	}

	safeFile := filepath.Join(repo, "safe.txt")
	nestedFile := filepath.Join(repo, "sub", "nested.txt")
	evilFile := filepath.Join(repoEvil, "secret.txt")
	outsideFile := filepath.Join(outside, "passwd.txt")
	secretFile := filepath.Join(repo, "secrets", "id_rsa")
	for _, f := range []string{safeFile, nestedFile, evilFile, outsideFile, secretFile} {
		if err := os.WriteFile(f, []byte("data"), 0644); err != nil {
			t.Fatal(err)
		}
	}

	p := New([]string{repo}, false, false).WithToolRules(nil, nil, []string{"*/secrets/*"})

	tests := []struct {
		name      string
		input     string
		wantAllow bool
	}{
		{
			name:      "empty path rejected",
			input:     "",
			wantAllow: false,
		},
		{
			name:      "parent traversal to outside",
			input:     repo + string(filepath.Separator) + ".." + string(filepath.Separator) + "outside" + string(filepath.Separator) + "passwd.txt",
			wantAllow: false,
		},
		{
			name:      "sibling directory prefix bypass via traversal",
			input:     repo + string(filepath.Separator) + ".." + string(filepath.Separator) + "repo-evil" + string(filepath.Separator) + "secret.txt",
			wantAllow: false,
		},
		{
			name:      "direct sibling directory prefix bypass",
			input:     evilFile,
			wantAllow: false,
		},
		{
			name:      "deep nested traversal to sibling prefix",
			input:     filepath.Join(repo, "sub") + string(filepath.Separator) + ".." + string(filepath.Separator) + ".." + string(filepath.Separator) + "repo-evil" + string(filepath.Separator) + "secret.txt",
			wantAllow: false,
		},
		{
			name:      "mixed forward slash traversal",
			input:     filepath.ToSlash(repo) + "/../repo-evil/secret.txt",
			wantAllow: false,
		},
		{
			name:      "nul byte in filename",
			input:     filepath.Join(repo, "safe.txt\x00.png"),
			wantAllow: false,
		},
		{
			name:      "nul byte in directory segment",
			input:     filepath.Join(repo, "sub\x00dir", "file.txt"),
			wantAllow: false,
		},
		{
			name:      "denied directory target itself",
			input:     filepath.Join(repo, "secrets"),
			wantAllow: false,
		},
		{
			name:      "denied file under pattern",
			input:     secretFile,
			wantAllow: false,
		},
		{
			name:      "valid root itself",
			input:     repo,
			wantAllow: true,
		},
		{
			name:      "valid normalized internal path",
			input:     repo + string(filepath.Separator) + "sub" + string(filepath.Separator) + ".." + string(filepath.Separator) + "safe.txt",
			wantAllow: true,
		},
		{
			name:      "valid nonexistent file inside root",
			input:     filepath.Join(repo, "newdir", "future.txt"),
			wantAllow: true,
		},
	}

	if runtime.GOOS == "windows" {
		vol := filepath.VolumeName(repo)
		tests = append(tests,
			struct {
				name      string
				input     string
				wantAllow bool
			}{
				name:      "windows drive-relative path",
				input:     vol + "safe.txt",
				wantAllow: false,
			},
			struct {
				name      string
				input     string
				wantAllow bool
			}{
				name:      "windows bare drive",
				input:     vol,
				wantAllow: false,
			},
			struct {
				name      string
				input     string
				wantAllow bool
			}{
				name:      "windows alternate data stream",
				input:     safeFile + ":hidden:$DATA",
				wantAllow: false,
			},
			struct {
				name      string
				input     string
				wantAllow bool
			}{
				name:      "windows device namespace extended path",
				input:     `\\?\` + safeFile,
				wantAllow: false,
			},
			struct {
				name      string
				input     string
				wantAllow bool
			}{
				name:      "windows device namespace dot path",
				input:     `\\.\` + strings.TrimPrefix(safeFile, vol+`\`),
				wantAllow: false,
			},
			struct {
				name      string
				input     string
				wantAllow bool
			}{
				name:      "windows nt object namespace path",
				input:     `\??\` + safeFile,
				wantAllow: false,
			},
			struct {
				name      string
				input     string
				wantAllow bool
			}{
				name:      "windows reserved dos device NUL",
				input:     filepath.Join(repo, "NUL"),
				wantAllow: false,
			},
			struct {
				name      string
				input     string
				wantAllow bool
			}{
				name:      "windows reserved dos device CON.txt",
				input:     filepath.Join(repo, "CON.txt"),
				wantAllow: false,
			},
			struct {
				name      string
				input     string
				wantAllow bool
			}{
				name:      "windows trailing dot filename bypass",
				input:     safeFile + ".",
				wantAllow: false,
			},
			struct {
				name      string
				input     string
				wantAllow bool
			}{
				name:      "windows trailing space filename bypass",
				input:     safeFile + " ",
				wantAllow: false,
			},
			struct {
				name      string
				input     string
				wantAllow bool
			}{
				name:      "windows case-insensitive deny pattern bypass",
				input:     filepath.Join(repo, "SECRETS", "id_rsa"),
				wantAllow: false,
			},
			struct {
				name      string
				input     string
				wantAllow bool
			}{
				name:      "windows case-insensitive denied dir target",
				input:     filepath.Join(repo, "Secrets"),
				wantAllow: false,
			},
		)
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := p.ResolvePath(tc.input)
			if tc.wantAllow && err != nil {
				t.Fatalf("expected path %q to be allowed, got error: %v", tc.input, err)
			}
			if !tc.wantAllow && err == nil {
				t.Fatalf("expected path %q to be rejected, but got %q", tc.input, got)
			}
		})
	}

	if runtime.GOOS == "windows" {
		t.Run("windows_drive_relative_rejected_even_when_cwd_inside_root", func(t *testing.T) {
			t.Chdir(repo)
			vol := filepath.VolumeName(repo)
			if got, err := p.ResolvePath(vol + "safe.txt"); err == nil {
				t.Fatalf("expected drive-relative path to be rejected even when cwd is inside root, got %q", got)
			}
		})
	}
}

func TestResolvePathSymlinkAndAncestorEscape(t *testing.T) {
	base := t.TempDir()
	repo := filepath.Join(base, "repo")
	outside := filepath.Join(base, "outside")
	for _, d := range []string{
		filepath.Join(repo, "real_dir"),
		filepath.Join(repo, "sub"),
		filepath.Join(repo, "secrets"),
		outside,
	} {
		if err := os.MkdirAll(d, 0755); err != nil {
			t.Fatal(err)
		}
	}

	outsideSecret := filepath.Join(outside, "secret.txt")
	realInside := filepath.Join(repo, "real_dir", "ok.txt")
	secretInside := filepath.Join(repo, "secrets", "token.txt")
	for _, f := range []string{outsideSecret, realInside, secretInside} {
		if err := os.WriteFile(f, []byte("payload"), 0644); err != nil {
			t.Fatal(err)
		}
	}

	// File symlink escaping outside
	fileSymOut := filepath.Join(repo, "file_escape.txt")
	if err := os.Symlink(outsideSecret, fileSymOut); err != nil {
		t.Fatalf("create file symlink: %v", err)
	}

	// Dir symlink escaping outside
	dirSymOut := filepath.Join(repo, "dir_escape")
	if err := os.Symlink(outside, dirSymOut); err != nil {
		t.Fatalf("create dir symlink: %v", err)
	}

	// Broken/dangling file symlink pointing outside
	danglingFileOut := filepath.Join(repo, "dangling_file.txt")
	if err := os.Symlink(filepath.Join(outside, "not_yet_created.txt"), danglingFileOut); err != nil {
		t.Fatalf("create dangling file symlink: %v", err)
	}

	// Broken/dangling dir symlink pointing outside
	danglingDirOut := filepath.Join(repo, "dangling_dir")
	if err := os.Symlink(filepath.Join(outside, "missing_subdir"), danglingDirOut); err != nil {
		t.Fatalf("create dangling dir symlink: %v", err)
	}

	// Relative symlink escaping outside
	relSymOut := filepath.Join(repo, "sub", "rel_escape.txt")
	if err := os.Symlink(filepath.Join("..", "..", "outside", "secret.txt"), relSymOut); err != nil {
		t.Fatalf("create relative symlink: %v", err)
	}

	// Symlink loop
	loopA := filepath.Join(repo, "loop_a")
	loopB := filepath.Join(repo, "loop_b")
	if err := os.Symlink(loopB, loopA); err != nil {
		t.Fatalf("create loop_a: %v", err)
	}
	if err := os.Symlink(loopA, loopB); err != nil {
		t.Fatalf("create loop_b: %v", err)
	}

	// Safe internal symlink
	safeDirSym := filepath.Join(repo, "safe_alias")
	if err := os.Symlink(filepath.Join(repo, "real_dir"), safeDirSym); err != nil {
		t.Fatalf("create safe internal symlink: %v", err)
	}

	// Internal symlink into a denied directory
	deniedAliasSym := filepath.Join(repo, "public_alias")
	if err := os.Symlink(filepath.Join(repo, "secrets"), deniedAliasSym); err != nil {
		t.Fatalf("create alias to secrets: %v", err)
	}

	p := New([]string{repo}, false, false).WithToolRules(nil, nil, []string{"*/secrets/*"})

	tests := []struct {
		name      string
		input     string
		wantAllow bool
	}{
		{
			name:      "existing file symlink escaping root",
			input:     fileSymOut,
			wantAllow: false,
		},
		{
			name:      "existing file via dir symlink escaping root",
			input:     filepath.Join(dirSymOut, "secret.txt"),
			wantAllow: false,
		},
		{
			name:      "nonexistent nested file under escaping dir symlink",
			input:     filepath.Join(dirSymOut, "new_sub", "new_file.txt"),
			wantAllow: false,
		},
		{
			name:      "dangling file symlink pointing outside",
			input:     danglingFileOut,
			wantAllow: false,
		},
		{
			name:      "nonexistent file under dangling dir symlink pointing outside",
			input:     filepath.Join(danglingDirOut, "deep", "payload.txt"),
			wantAllow: false,
		},
		{
			name:      "relative symlink escaping root",
			input:     relSymOut,
			wantAllow: false,
		},
		{
			name:      "symlink loop rejected cleanly",
			input:     loopA,
			wantAllow: false,
		},
		{
			name:      "internal symlink to denied secrets dir rejected",
			input:     filepath.Join(deniedAliasSym, "token.txt"),
			wantAllow: false,
		},
		{
			name:      "safe internal symlink allowed",
			input:     filepath.Join(safeDirSym, "ok.txt"),
			wantAllow: true,
		},
		{
			name:      "nonexistent file under safe internal symlink allowed",
			input:     filepath.Join(safeDirSym, "new_dir", "future.txt"),
			wantAllow: true,
		},
	}

	if runtime.GOOS == "windows" {
		juncOut := filepath.Join(repo, "junc_escape")
		cmd := exec.Command("cmd", "/c", "mklink", "/J", juncOut, outside)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("create junction: %v: %s", err, string(out))
		}
		tests = append(tests,
			struct {
				name      string
				input     string
				wantAllow bool
			}{
				name:      "windows junction existing target escape",
				input:     filepath.Join(juncOut, "secret.txt"),
				wantAllow: false,
			},
			struct {
				name      string
				input     string
				wantAllow bool
			}{
				name:      "windows junction nonexistent nested target escape",
				input:     filepath.Join(juncOut, "new_sub", "escape.txt"),
				wantAllow: false,
			},
		)
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := p.ResolvePath(tc.input)
			if tc.wantAllow && err != nil {
				t.Fatalf("expected %q to be allowed, got error: %v", tc.input, err)
			}
			if !tc.wantAllow && err == nil {
				t.Fatalf("expected %q to be rejected, but resolved to %q", tc.input, got)
			}
		})
	}
}


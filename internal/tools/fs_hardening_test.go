package tools

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/CmfH009/microgravity/internal/policy"
)

func TestFSToolsRejectSymlinkAndTraversalEscapes(t *testing.T) {
	base := t.TempDir()
	repo := filepath.Join(base, "repo")
	repoEvil := filepath.Join(base, "repo-evil")
	outside := filepath.Join(base, "outside")
	for _, d := range []string{
		repo,
		filepath.Join(repo, "secrets"),
		repoEvil,
		outside,
	} {
		if err := os.MkdirAll(d, 0755); err != nil {
			t.Fatal(err)
		}
	}

	safeFile := filepath.Join(repo, "safe.txt")
	if err := os.WriteFile(safeFile, []byte("hello safe world\n"), 0644); err != nil {
		t.Fatal(err)
	}
	secretInside := filepath.Join(repo, "secrets", "api.txt")
	if err := os.WriteFile(secretInside, []byte("needle_secret_inside\n"), 0644); err != nil {
		t.Fatal(err)
	}
	outsideFile := filepath.Join(outside, "leak.txt")
	if err := os.WriteFile(outsideFile, []byte("needle_outside_leak\n"), 0644); err != nil {
		t.Fatal(err)
	}
	evilFile := filepath.Join(repoEvil, "evil.txt")
	if err := os.WriteFile(evilFile, []byte("needle_evil_sibling\n"), 0644); err != nil {
		t.Fatal(err)
	}

	symFile := filepath.Join(repo, "sym_leak.txt")
	if err := os.Symlink(outsideFile, symFile); err != nil {
		t.Fatal(err)
	}
	symDir := filepath.Join(repo, "sym_dir")
	if err := os.Symlink(outside, symDir); err != nil {
		t.Fatal(err)
	}

	pol := policy.New([]string{repo}, false, false).WithToolRules(nil, nil, []string{"*/secrets/*"})
	r := NewRegistry()
	RegisterFSTools(r, pol)
	registerLegacyTools(r, pol, nil)
	ctx := context.Background()

	escapePaths := []struct {
		name string
		path string
	}{
		{"traversal", filepath.Join(repo, "..", "outside", "leak.txt")},
		{"sibling_prefix", evilFile},
		{"file_symlink", symFile},
		{"nonexistent_under_dir_symlink", filepath.Join(symDir, "newdir", "created.txt")},
		{"nul_byte", filepath.Join(repo, "safe.txt\x00.txt")},
		{"denied_pattern", secretInside},
	}
	if runtime.GOOS == "windows" {
		escapePaths = append(escapePaths, struct {
			name string
			path string
		}{"windows_ads", safeFile + ":ads"})
	}

	for _, tc := range escapePaths {
		t.Run("view_file/"+tc.name, func(t *testing.T) {
			if _, err := r.Execute(ctx, "view_file", map[string]interface{}{"AbsolutePath": tc.path}); err == nil {
				t.Fatalf("view_file unexpectedly allowed %q", tc.path)
			}
		})
		t.Run("write_to_file/"+tc.name, func(t *testing.T) {
			if _, err := r.Execute(ctx, "write_to_file", map[string]interface{}{
				"TargetFile":  tc.path,
				"CodeContent": "pwned",
				"Overwrite":   true,
			}); err == nil {
				t.Fatalf("write_to_file unexpectedly allowed %q", tc.path)
			}
		})
		t.Run("replace_file_content/"+tc.name, func(t *testing.T) {
			if _, err := r.Execute(ctx, "replace_file_content", map[string]interface{}{
				"TargetFile":         tc.path,
				"TargetContent":      "needle",
				"ReplacementContent": "pwned",
			}); err == nil {
				t.Fatalf("replace_file_content unexpectedly allowed %q", tc.path)
			}
		})
		t.Run("read_file/"+tc.name, func(t *testing.T) {
			if _, err := r.Execute(ctx, "read_file", map[string]interface{}{"path": tc.path}); err == nil {
				t.Fatalf("read_file unexpectedly allowed %q", tc.path)
			}
		})
		t.Run("write_file/"+tc.name, func(t *testing.T) {
			if _, err := r.Execute(ctx, "write_file", map[string]interface{}{
				"path":    tc.path,
				"content": "pwned",
			}); err == nil {
				t.Fatalf("write_file unexpectedly allowed %q", tc.path)
			}
		})
	}

	// grep_search and find_by_name inside repo must NOT follow sym_leak.txt or expose secrets/api.txt
	t.Run("grep_search_skips_symlink_and_denied", func(t *testing.T) {
		out, err := r.Execute(ctx, "grep_search", map[string]interface{}{
			"SearchPath": repo,
			"Query":      "needle_",
		})
		if err != nil {
			t.Fatalf("grep_search failed: %v", err)
		}
		if strings.Contains(out, "needle_outside_leak") {
			t.Fatalf("grep_search leaked content via file symlink: %s", out)
		}
		if strings.Contains(out, "needle_secret_inside") {
			t.Fatalf("grep_search leaked denied secrets file: %s", out)
		}
	})

	t.Run("find_by_name_skips_symlink_and_denied", func(t *testing.T) {
		out, err := r.Execute(ctx, "find_by_name", map[string]interface{}{
			"SearchDirectory": repo,
			"Pattern":         "*.txt",
		})
		if err != nil {
			t.Fatalf("find_by_name failed: %v", err)
		}
		if strings.Contains(out, "sym_leak.txt") {
			t.Fatalf("find_by_name returned escaping symlink: %s", out)
		}
		if strings.Contains(out, "api.txt") {
			t.Fatalf("find_by_name returned denied file: %s", out)
		}
		if !strings.Contains(out, "safe.txt") {
			t.Fatalf("find_by_name missed safe.txt: %s", out)
		}
	})

	// Verify outside file remained unmodified
	gotOutside, err := os.ReadFile(outsideFile)
	if err != nil || string(gotOutside) != "needle_outside_leak\n" {
		t.Fatalf("outside file was modified! got=%q err=%v", string(gotOutside), err)
	}
}

func TestAtomicWriteFileBackupAndRollback(t *testing.T) {
	t.Run("direct_rename_succeeds", func(t *testing.T) {
		dir := t.TempDir()
		target := filepath.Join(dir, "config.txt")
		if err := os.WriteFile(target, []byte("old"), 0644); err != nil {
			t.Fatal(err)
		}

		if err := atomicWriteFileWithRename(target, []byte("new"), 0644, os.Rename); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		got, err := os.ReadFile(target)
		if err != nil || string(got) != "new" {
			t.Fatalf("got=%q err=%v", string(got), err)
		}
		entries, _ := os.ReadDir(dir)
		if len(entries) != 1 {
			t.Fatalf("expected no leftover temp/backup files, got %d entries", len(entries))
		}
	})

	t.Run("fallback_backup_stage_succeeds_when_direct_replace_fails", func(t *testing.T) {
		dir := t.TempDir()
		target := filepath.Join(dir, "data.txt")
		if err := os.WriteFile(target, []byte("original"), 0644); err != nil {
			t.Fatal(err)
		}

		calls := 0
		renameFn := func(oldPath, newPath string) error {
			calls++
			// Fail initial direct replace (tmp -> target) so backup staging runs.
			if calls == 1 && newPath == target {
				return errors.New("simulated target exists error")
			}
			return os.Rename(oldPath, newPath)
		}

		if err := atomicWriteFileWithRename(target, []byte("updated"), 0644, renameFn); err != nil {
			t.Fatalf("expected backup-stage replace to succeed, got: %v", err)
		}
		got, err := os.ReadFile(target)
		if err != nil || string(got) != "updated" {
			t.Fatalf("expected updated content, got=%q err=%v", string(got), err)
		}
		entries, _ := os.ReadDir(dir)
		if len(entries) != 1 {
			t.Fatalf("expected backup/temp files cleaned up, got %d entries", len(entries))
		}
	})

	t.Run("rollback_restores_original_file_when_replace_fails_after_backup", func(t *testing.T) {
		dir := t.TempDir()
		target := filepath.Join(dir, "precious.txt")
		if err := os.WriteFile(target, []byte("precious original content"), 0644); err != nil {
			t.Fatal(err)
		}

		renameFn := func(oldPath, newPath string) error {
			// Fail both direct and post-backup renames onto target, but allow backup & rollback.
			if newPath == target && strings.Contains(filepath.Base(oldPath), ".mg-tmp-") {
				return errors.New("simulated replace failure")
			}
			return os.Rename(oldPath, newPath)
		}

		err := atomicWriteFileWithRename(target, []byte("new content"), 0644, renameFn)
		if err == nil {
			t.Fatal("expected error when replace fails")
		}
		got, readErr := os.ReadFile(target)
		if readErr != nil {
			t.Fatalf("target file was lost after failed rename! readErr=%v", readErr)
		}
		if string(got) != "precious original content" {
			t.Fatalf("target content corrupted after rollback: got %q", string(got))
		}
		entries, _ := os.ReadDir(dir)
		if len(entries) != 1 {
			t.Fatalf("expected no leftover temp/backup files after rollback, got %d entries", len(entries))
		}
	})

	t.Run("backup_stage_failure_preserves_original_file", func(t *testing.T) {
		dir := t.TempDir()
		target := filepath.Join(dir, "locked.txt")
		if err := os.WriteFile(target, []byte("keep me intact"), 0644); err != nil {
			t.Fatal(err)
		}

		renameFn := func(oldPath, newPath string) error {
			return errors.New("simulated sharing violation")
		}

		if err := atomicWriteFileWithRename(target, []byte("new"), 0644, renameFn); err == nil {
			t.Fatal("expected error when both rename and backup fail")
		}
		got, err := os.ReadFile(target)
		if err != nil || string(got) != "keep me intact" {
			t.Fatalf("original file lost or modified: got=%q err=%v", string(got), err)
		}
	})

	t.Run("rejects_directory_and_symlink_targets", func(t *testing.T) {
		dir := t.TempDir()
		subDir := filepath.Join(dir, "subdir")
		if err := os.MkdirAll(subDir, 0755); err != nil {
			t.Fatal(err)
		}
		if err := atomicWriteFile(subDir, []byte("x"), 0644); err == nil {
			t.Fatal("expected error writing to directory target")
		}

		outside := filepath.Join(t.TempDir(), "outside.txt")
		if err := os.WriteFile(outside, []byte("outside"), 0644); err != nil {
			t.Fatal(err)
		}
		sym := filepath.Join(dir, "sym.txt")
		if err := os.Symlink(outside, sym); err != nil {
			t.Fatal(err)
		}
		if err := atomicWriteFile(sym, []byte("pwn"), 0644); err == nil {
			t.Fatal("expected error writing through symlink target")
		}
		got, _ := os.ReadFile(outside)
		if string(got) != "outside" {
			t.Fatalf("symlink target was modified: %q", string(got))
		}
	})
}

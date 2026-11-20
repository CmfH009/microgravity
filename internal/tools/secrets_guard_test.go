package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/CmfH009/microgravity/internal/events"
	"github.com/CmfH009/microgravity/internal/rag"
	"github.com/CmfH009/microgravity/internal/scheduler"
)

// newFSTestRegistry builds a registry with filesystem tools registered,
// mirroring the setup used by TestBuiltins.
func newFSTestRegistry(t *testing.T) *Registry {
	t.Helper()
	registry := NewRegistry()
	bus := events.NewEventBus()
	sched := scheduler.New()
	t.Cleanup(sched.Stop)
	ch := make(chan string, 5)

	store, err := rag.NewStore(filepath.Join(t.TempDir(), "guard_rag.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })

	RegisterBuiltins(registry, bus, sched, ch, store)
	return registry
}

func TestWriteToFileSyncBlocksSecrets(t *testing.T) {
	t.Setenv("MG_ALLOW_SECRET_WRITE", "")
	r := newFSTestRegistry(t)
	ctx := context.Background()
	target := filepath.Join(os.TempDir(), "mg_secret_guard_test.txt")
	defer os.Remove(target)

	_, err := r.Execute(ctx, "write_to_file", map[string]interface{}{
		"TargetFile":  target,
		"CodeContent": "aws_key = \"AKIAQWERTYUIOPASDFGH\"\n",
		"Overwrite":   true,
	})
	if err == nil {
		t.Fatal("expected secret scan to block write")
	}
	if !strings.Contains(err.Error(), "secret scan") {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, statErr := os.Stat(target); statErr == nil {
		t.Fatal("blocked write must not create the file")
	}

	// AllowSecrets=true is the per-call override.
	out, err := r.Execute(ctx, "write_to_file", map[string]interface{}{
		"TargetFile":   target,
		"CodeContent":  "aws_key = \"AKIAQWERTYUIOPASDFGH\"\n",
		"Overwrite":    true,
		"AllowSecrets": true,
	})
	if err != nil {
		t.Fatalf("override write failed: %v", err)
	}
	if !strings.Contains(out, "Successfully wrote") {
		t.Fatalf("unexpected success message: %s", out)
	}
}

func TestReplaceFileSyncScansInsertedText(t *testing.T) {
	t.Setenv("MG_ALLOW_SECRET_WRITE", "")
	r := newFSTestRegistry(t)
	ctx := context.Background()
	target := filepath.Join(os.TempDir(), "mg_secret_replace_test.txt")
	defer os.Remove(target)

	if err := os.WriteFile(target, []byte("config placeholder\n"), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := r.Execute(ctx, "replace_file_content", map[string]interface{}{
		"TargetFile":         target,
		"TargetContent":      "config placeholder",
		"ReplacementContent": "api_key = \"b3c1f9d2e4a680715c3e\"",
	})
	if err == nil {
		t.Fatal("expected secret scan to block replacement")
	}
	if !strings.Contains(err.Error(), "secret scan") {
		t.Fatalf("unexpected error: %v", err)
	}

	// Pre-existing secrets in the file must not block unrelated edits.
	if err := os.WriteFile(target, []byte("api_key = \"b3c1f9d2e4a680715c3e\"\nconfig placeholder\n"), 0644); err != nil {
		t.Fatal(err)
	}
	out, err := r.Execute(ctx, "replace_file_content", map[string]interface{}{
		"TargetFile":         target,
		"TargetContent":      "config placeholder",
		"ReplacementContent": "config ready",
	})
	if err != nil {
		t.Fatalf("unrelated edit of secret-bearing file failed: %v", err)
	}
	if !strings.Contains(out, "Successfully replaced") {
		t.Fatalf("unexpected success message: %s", out)
	}
}

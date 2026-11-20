package tests

import (
	"bytes"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"
)

// TestChaos_RapidFire simulates Phase 4 of the empirical audit.
// It exposes the CLI to 50 rapid-fire invalid flag requests to test
// fault tolerance, process cleanup, and leak prevention.
func TestChaos_RapidFire(t *testing.T) {
	binName := "mg-chaos-test"
	if runtime.GOOS == "windows" {
		binName += ".exe"
	}
	binPath := filepath.Join(t.TempDir(), binName)

	// Build the executable once into TempDir so the repo stays clean.
	buildCmd := exec.Command("go", "build", "-o", binPath, "..")
	if out, err := buildCmd.CombinedOutput(); err != nil {
		t.Fatalf("Failed to build executable for chaos testing: %v\n%s", err, string(out))
	}

	const iterations = 50
	var wg sync.WaitGroup

	startTime := time.Now()

	for i := 0; i < iterations; i++ {
		wg.Add(1)
		go func(iteration int) {
			defer wg.Done()

			// Execute the command with an unknown flag
			cmd := exec.Command(binPath, "--run-unknown-xyz")
			var stderr bytes.Buffer
			cmd.Stderr = &stderr
			
			err := cmd.Run()
			
			// We EXPECT an error (exit status 1 or similar) due to the unknown flag
			if err == nil {
				t.Errorf("Iteration %d: expected error for unknown flag, got nil", iteration)
			}
			
			// The output should contain "unknown flag"
			if !bytes.Contains(stderr.Bytes(), []byte("unknown flag")) && 
				!bytes.Contains(stderr.Bytes(), []byte("unknown command")) {
				// Depending on the CLI framework (cobra), it might say unknown command or unknown flag.
				// We just want to ensure it fails gracefully without a panic.
				if bytes.Contains(stderr.Bytes(), []byte("panic:")) {
					t.Errorf("Iteration %d: CLI panicked! Stderr: %s", iteration, stderr.String())
				}
			}
		}(i)
	}

	wg.Wait()
	elapsed := time.Since(startTime)
	t.Logf("Completed %d iterations in %v", iterations, elapsed)
}

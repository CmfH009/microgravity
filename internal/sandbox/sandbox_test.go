package sandbox

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/docker/docker/client"
)

func skipIfNoDocker(t *testing.T) {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		t.Skipf("Skipping test: docker client creation failed: %v", err)
	}
	defer cli.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if _, err = cli.Ping(ctx); err != nil {
		t.Skipf("Skipping test: docker daemon not available: %v", err)
	}
}

func TestRunPython_Success(t *testing.T) {
	skipIfNoDocker(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	code := `print("hello from python")`
	out, err := RunPython(ctx, code)
	if err != nil {
		t.Fatalf("RunPython failed: %v", err)
	}
	if !strings.Contains(out, "hello from python") {
		t.Errorf("Expected output to contain 'hello from python', got: %q", out)
	}
}

func TestRunNodeJS_Success(t *testing.T) {
	skipIfNoDocker(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	code := `console.log("hello from nodejs")`
	out, err := RunNodeJS(ctx, code)
	if err != nil {
		t.Fatalf("RunNodeJS failed: %v", err)
	}
	if !strings.Contains(out, "hello from nodejs") {
		t.Errorf("Expected output to contain 'hello from nodejs', got: %q", out)
	}
}

func TestRunPython_SyntaxError(t *testing.T) {
	skipIfNoDocker(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	code := `print("hello"` // Syntax error
	out, err := RunPython(ctx, code)
	if err != nil {
		t.Fatalf("Expected nil error but got %v. Script failures should return logs as output.", err)
	}
	if !strings.Contains(out, "SyntaxError") {
		t.Errorf("Expected output to contain 'SyntaxError', got: %q", out)
	}
}

func TestRunPython_Timeout(t *testing.T) {
	skipIfNoDocker(t)
	// Timeout short enough to interrupt long execution
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	code := `import time; time.sleep(10); print("done")`
	_, err := RunPython(ctx, code)
	if err == nil {
		t.Fatalf("Expected timeout error, got nil")
	}
	if !strings.Contains(err.Error(), "context done") && !strings.Contains(err.Error(), "timeout") && !strings.Contains(err.Error(), "cancelled") {
		t.Errorf("Expected context timeout error, got: %v", err)
	}
}

func TestRunNodeJS_NoNetwork(t *testing.T) {
	skipIfNoDocker(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// Try to fetch a webpage, should fail since network is disabled
	code := `
const http = require('http');
const req = http.get('http://93.184.216.34', (res) => { // example.com IP
  console.log('connected');
});
req.on('error', (e) => {
  console.error(e.message);
});
`
	out, err := RunNodeJS(ctx, code)
	if err != nil {
		t.Fatalf("Expected err to be nil, got: %v", err)
	}
	if !strings.Contains(out, "ENETUNREACH") && !strings.Contains(out, "ECONNREFUSED") && !strings.Contains(out, "EAI_AGAIN") && !strings.Contains(out, "socket hang up") {
		t.Errorf("Expected network error in output, got: %q", out)
	}
}

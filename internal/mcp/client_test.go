package mcp

import (
	"context"
	"testing"
	"time"
)

func TestSendRequestHonorsContextCancellation(t *testing.T) {
	client := NewClient("", nil)
	client.stdin = failingWriteCloser{}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	_, err := client.sendRequest(ctx, "test", nil)
	if err == nil {
		t.Fatal("expected request to fail when MCP context is cancelled")
	}
}

type failingWriteCloser struct{}

func (failingWriteCloser) Write([]byte) (int, error) { return 0, nil }
func (failingWriteCloser) Close() error              { return nil }

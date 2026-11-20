package updater

import (
	"testing"
	"time"

	"github.com/CmfH009/microgravity/internal/ollama"
)

func TestDaemonStartStop(t *testing.T) {
	client := ollama.NewClient("http://localhost:11434")
	daemon := NewDaemon(client, "gemma:2b")

	daemon.Start()
	time.Sleep(50 * time.Millisecond)
	daemon.Stop()
}

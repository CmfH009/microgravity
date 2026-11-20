package updater

import (
	"context"
	"sync"
	"time"

	"github.com/purav/microgravity/internal/ollama"
)

type Daemon struct {
	client     *ollama.Client
	modelName  string
	pullTicker *time.Ticker
	done       chan struct{}
	mu         sync.Mutex
	stopOnce   sync.Once
}

func NewDaemon(client *ollama.Client, modelName string) *Daemon {
	return &Daemon{
		client:    client,
		modelName: modelName,
		done:      make(chan struct{}),
	}
}

func (d *Daemon) Start() {
	go func() {
		d.pullModel()

		// Run periodic check every 24 hours
		ticker := time.NewTicker(24 * time.Hour)
		d.mu.Lock()
		d.pullTicker = ticker
		d.mu.Unlock()
		for {
			select {
			case <-ticker.C:
				d.pullModel()
			case <-d.done:
				return
			}
		}
	}()
}

func (d *Daemon) Stop() {
	d.stopOnce.Do(func() {
		d.mu.Lock()
		ticker := d.pullTicker
		d.mu.Unlock()
		if ticker != nil {
			ticker.Stop()
		}
		close(d.done)
	})
}

func (d *Daemon) pullModel() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	hasModel, err := d.client.HasModel(ctx, d.modelName)
	if err == nil && hasModel {
		// Model is ready, no download needed
		return
	}

	// Pull target model in background
	_ = d.client.Pull(ctx, d.modelName, nil)
}

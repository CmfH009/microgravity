package cmd

import (
	"context"
	"fmt"
	"time"

	"github.com/CmfH009/microgravity/internal/engine"
	"github.com/CmfH009/microgravity/internal/git"
	"github.com/CmfH009/microgravity/internal/history"
	"github.com/CmfH009/microgravity/internal/models"
	"github.com/CmfH009/microgravity/internal/ollama"
	"github.com/CmfH009/microgravity/internal/scheduler"
	"github.com/CmfH009/microgravity/internal/slash"
)

type engineSlashSession struct {
	sess       *engine.Session
	notifyChan chan string
}

var _ slash.Session = (*engineSlashSession)(nil)

func adaptEngineSessionForSlash(sess *engine.Session, notifyChan chan string) slash.Session {
	if sess == nil {
		return nil
	}
	return &engineSlashSession{
		sess:       sess,
		notifyChan: notifyChan,
	}
}

func (a *engineSlashSession) Snapshot() slash.SessionInfo {
	if a == nil || a.sess == nil {
		return slash.SessionInfo{}
	}
	msgs := a.sess.GetMessages()
	chars := 0
	for _, m := range msgs {
		chars += len(m.Content)
	}
	return slash.SessionInfo{
		SessionName:      a.sess.Config.SessionName,
		Cwd:              a.sess.Config.Cwd,
		ActiveModel:      a.sess.Config.ActiveModel,
		CloudModelName:   a.sess.Config.CloudModelName,
		LocalModelName:   a.sess.Config.LocalModelName,
		LocalFastModel:   a.sess.Config.LocalFastModel,
		LocalHeavyModel:  a.sess.Config.LocalHeavyModel,
		OpenAIBaseURL:    a.sess.Config.OpenAIBaseURL,
		RoutingMode:      string(a.sess.GetRoutingMode()),
		ConversationText: a.sess.Conversation.String(),
		MessageChars:     chars,
		MessageCount:     len(msgs),
		MCPCount:         len(a.sess.Config.MCPClients),
		LoadedSkills:     a.sess.Config.LoadedSkills,
	}
}

func (a *engineSlashSession) ClearHistory() {
	if a != nil && a.sess != nil {
		a.sess.ClearHistory()
	}
}

func (a *engineSlashSession) SetRoutingMode(mode string) {
	if a != nil && a.sess != nil {
		a.sess.SetRoutingMode(mode)
	}
}

func (a *engineSlashSession) SetModel(modelID, provider string) {
	if a == nil || a.sess == nil {
		return
	}
	a.sess.Config.ExplicitProvider = provider
	a.sess.Config.ActiveModel = modelID
	if provider == "cloud" {
		a.sess.Config.CloudModelName = modelID
	} else {
		a.sess.Config.LocalModelName = modelID
	}
}

func (a *engineSlashSession) SetMode(mode string) {
	if a != nil && a.sess != nil {
		a.sess.Config.Mode = mode
	}
}

func (a *engineSlashSession) SetCavemanLevel(level string) {
	if a != nil && a.sess != nil {
		a.sess.Config.CavemanLevel = level
	}
}

func (a *engineSlashSession) SwitchSession(name string) error {
	if a == nil || a.sess == nil {
		return fmt.Errorf("no active session")
	}
	history.CurrentSession = name
	a.sess.Config.SessionName = name
	return a.sess.LoadSavedHistory()
}

func (a *engineSlashSession) Rewind() (*git.Checkpoint, error) {
	if a == nil || a.sess == nil {
		return nil, fmt.Errorf("no active engine session to rewind")
	}
	return a.sess.Rewind()
}

func (a *engineSlashSession) RestoreCheckpoint(commitSHA string) error {
	if a == nil || a.sess == nil || a.sess.CheckpointMgr == nil {
		return fmt.Errorf("checkpoint manager not initialized")
	}
	return a.sess.CheckpointMgr.RestoreCheckpoint(commitSHA)
}

func (a *engineSlashSession) LatestCheckpoint() (*git.Checkpoint, bool) {
	if a == nil || a.sess == nil || a.sess.CheckpointMgr == nil {
		return nil, false
	}
	return a.sess.CheckpointMgr.Latest()
}

func (a *engineSlashSession) ScheduleTimer(dur time.Duration, msg string) (int, error) {
	if a == nil || a.sess == nil || a.sess.Config.Scheduler == nil {
		return 0, fmt.Errorf("scheduler is not initialized on this session")
	}
	notify := a.notifyChan
	if notify == nil {
		notify = a.sess.Config.NotifyChan
	}
	id := a.sess.Config.Scheduler.ScheduleTimer(dur, msg, scheduler.ConditionNever, "", notify)
	return id, nil
}

func newSlashHandler(sess *engine.Session, notifyChan chan string, supportsOverlays bool) *slash.Handler {
	return slash.NewHandler(adaptEngineSessionForSlash(sess, notifyChan), slash.Options{
		DefaultLocalModel: modelName,
		SupportsOverlays:  supportsOverlays,
		ScanModels: func(ctx context.Context) ([]slash.ModelOption, error) {
			catalog, err := models.NewScanner().Scan(ctx)
			if err != nil {
				return nil, err
			}
			opts := make([]slash.ModelOption, 0, len(catalog))
			for _, m := range catalog {
				opts = append(opts, slash.ModelOption{
					ID:       m.ID,
					Name:     m.Name,
					Provider: m.Provider,
					Badge:    m.RenderBadge(),
				})
			}
			return opts, nil
		},
		EnsureLocalModel: func(ctx context.Context, modelID string) bool {
			url := baseURL
			if sess != nil && sess.Config.OllamaBaseURL != "" {
				url = sess.Config.OllamaBaseURL
			}
			if url == "" {
				url = "http://localhost:11434"
			}
			ollama.EnsureOllama()
			client := ollama.NewClient(url)
			hasModel, _ := client.HasModel(ctx, modelID)
			if !hasModel {
				go func(mn string) {
					_ = client.Pull(context.Background(), mn, nil)
				}(modelID)
				return true
			}
			return false
		},
	})
}

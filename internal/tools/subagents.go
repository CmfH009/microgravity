package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/CmfH009/microgravity/internal/events"
	"github.com/CmfH009/microgravity/internal/mcp"
	"github.com/CmfH009/microgravity/internal/subagent"
)

var subagentIDSeq atomic.Uint64

// RegisterSubagentTools registers multi-subagent orchestration and communication tools.
func RegisterSubagentTools(r *Registry, subMgr *subagent.Manager, eb *events.EventBus) {
	// ── 1. invoke_subagent ──
	r.Register(mcp.Tool{
		Name:        "invoke_subagent",
		Description: "Invoke one or more subagents concurrently in background tasks",
		InputSchema: mcp.Schema{
			Type: "object",
			Properties: map[string]interface{}{
				"Subagents": map[string]interface{}{
					"type": "array",
					"items": map[string]interface{}{
						"type": "object",
						"properties": map[string]interface{}{
							"TypeName":  map[string]interface{}{"type": "string"},
							"Role":      map[string]interface{}{"type": "string"},
							"Prompt":    map[string]interface{}{"type": "string"},
							"Model":     map[string]interface{}{"type": "string"},
							"Workspace": map[string]interface{}{"type": "string"},
						},
						"required": []string{"TypeName", "Role", "Prompt"},
					},
				},
				"toolAction":  map[string]interface{}{"type": "string"},
				"toolSummary": map[string]interface{}{"type": "string"},
			},
			Required: []string{"Subagents"},
		},
	}, func(ctx context.Context, args map[string]interface{}) (string, error) {
		rawList, ok := args["Subagents"].([]interface{})
		if !ok || len(rawList) == 0 {
			return "", fmt.Errorf("Subagents array is required and cannot be empty")
		}

		type spawnedResult struct {
			ConversationID string `json:"conversationId"`
			Role           string `json:"role"`
			TypeName       string `json:"type"`
			Status         string `json:"status"`
			TranscriptURI  string `json:"transcript"`
		}

		var spawned []spawnedResult
		for _, raw := range rawList {
			item, ok := raw.(map[string]interface{})
			if !ok {
				continue
			}

			typeName, _ := item["TypeName"].(string)
			role, _ := item["Role"].(string)
			prompt, _ := item["Prompt"].(string)
			model, _ := item["Model"].(string)
			workspaceStr, _ := item["Workspace"].(string)

			workspace := subagent.WorkspaceInherit
			if workspaceStr == "branch" {
				workspace = subagent.WorkspaceBranch
			} else if workspaceStr == "share" {
				workspace = subagent.WorkspaceShare
			}

			seq := subagentIDSeq.Add(1)
			id := fmt.Sprintf("sub-%d-%d", time.Now().UnixNano(), seq)
			info, err := subMgr.Spawn(ctx, id, role, subagent.SubagentProfile(typeName), prompt, model, workspace, nil)
			if err != nil {
				for _, prev := range spawned {
					subMgr.Kill(prev.ConversationID)
				}
				return "", fmt.Errorf("failed to spawn subagent [%s]: %w", role, err)
			}

			spawned = append(spawned, spawnedResult{
				ConversationID: info.ConversationID,
				Role:           info.Role,
				TypeName:       typeName,
				Status:         info.Status,
				TranscriptURI:  info.TranscriptURI,
			})
		}

		data, _ := json.MarshalIndent(spawned, "", "  ")
		return fmt.Sprintf("Successfully launched %d subagent(s):\n%s", len(spawned), string(data)), nil
	})

	// ── 2. define_subagent ──
	r.Register(mcp.Tool{
		Name:        "define_subagent",
		Description: "Define a new type of subagent that can be invoked via invoke_subagent",
		InputSchema: mcp.Schema{
			Type: "object",
			Properties: map[string]interface{}{
				"name":                  map[string]interface{}{"type": "string"},
				"description":           map[string]interface{}{"type": "string"},
				"system_prompt":         map[string]interface{}{"type": "string"},
				"enable_write_tools":    map[string]interface{}{"type": "boolean"},
				"enable_subagent_tools": map[string]interface{}{"type": "boolean"},
				"enable_mcp_tools":      map[string]interface{}{"type": "boolean"},
				"toolAction":            map[string]interface{}{"type": "string"},
				"toolSummary":           map[string]interface{}{"type": "string"},
			},
			Required: []string{"name", "description", "system_prompt"},
		},
	}, func(ctx context.Context, args map[string]interface{}) (string, error) {
		name, _ := args["name"].(string)
		desc, _ := args["description"].(string)
		sysPrompt, _ := args["system_prompt"].(string)
		writeTools, _ := args["enable_write_tools"].(bool)
		subTools, _ := args["enable_subagent_tools"].(bool)
		mcpTools, _ := args["enable_mcp_tools"].(bool)

		if name == "" || desc == "" || sysPrompt == "" {
			return "", fmt.Errorf("name, description, and system_prompt are required")
		}

		subMgr.DefineSubagent(subagent.SubagentDefinition{
			Name:                name,
			Description:         desc,
			SystemPrompt:        sysPrompt,
			EnableWriteTools:    writeTools,
			EnableSubagentTools: subTools,
			EnableMCPTools:      mcpTools,
		})

		return fmt.Sprintf("Subagent type %q successfully defined and registered for invoke_subagent.", name), nil
	})

	// ── 3. manage_subagents ──
	r.Register(mcp.Tool{
		Name:        "manage_subagents",
		Description: "Manage active subagents (list, kill, kill_all)",
		InputSchema: mcp.Schema{
			Type: "object",
			Properties: map[string]interface{}{
				"Action":          map[string]interface{}{"type": "string"},
				"ConversationIds": map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}},
				"toolAction":      map[string]interface{}{"type": "string"},
				"toolSummary":     map[string]interface{}{"type": "string"},
			},
			Required: []string{"Action"},
		},
	}, func(ctx context.Context, args map[string]interface{}) (string, error) {
		action, _ := args["Action"].(string)

		switch action {
		case "list":
			list := subMgr.List()
			data, _ := json.MarshalIndent(list, "", "  ")
			return string(data), nil
		case "kill_all":
			count := subMgr.KillAll()
			return fmt.Sprintf("Terminated %d subagent(s).", count), nil
		case "kill":
			ids, _ := args["ConversationIds"].([]interface{})
			killed := 0
			for _, idRaw := range ids {
				if id, ok := idRaw.(string); ok {
					if subMgr.Kill(id) {
						killed++
					}
				}
			}
			return fmt.Sprintf("Killed %d specified subagent(s).", killed), nil
		default:
			return "", fmt.Errorf("unknown action %q", action)
		}
	})

	// ── 4. send_message ──
	r.Register(mcp.Tool{
		Name:        "send_message",
		Description: "Send inter-agent message to another subagent",
		InputSchema: mcp.Schema{
			Type: "object",
			Properties: map[string]interface{}{
				"Recipient":   map[string]interface{}{"type": "string"},
				"Message":     map[string]interface{}{"type": "string"},
				"toolAction":  map[string]interface{}{"type": "string"},
				"toolSummary": map[string]interface{}{"type": "string"},
			},
			Required: []string{"Recipient", "Message"},
		},
	}, func(ctx context.Context, args map[string]interface{}) (string, error) {
		recipient, _ := args["Recipient"].(string)
		msg, _ := args["Message"].(string)
		if recipient == "" || msg == "" {
			return "", fmt.Errorf("Recipient and Message are required")
		}

		err := subMgr.SendMessage("main", recipient, msg)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("Message delivered to %s", recipient), nil
	})
}

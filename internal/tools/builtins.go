package tools

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/purav/microgravity/internal/events"
	"github.com/purav/microgravity/internal/mcp"
	"github.com/purav/microgravity/internal/policy"
	"github.com/purav/microgravity/internal/rag"
	"github.com/purav/microgravity/internal/scheduler"
	"github.com/purav/microgravity/internal/subagent"
	"github.com/purav/microgravity/internal/tasks"
)

type ArtifactMetadata struct {
	Summary         string `json:"Summary"`
	UserFacing      bool   `json:"UserFacing"`
	RequestFeedback bool   `json:"RequestFeedback"`
}

// RegisterBuiltins coordinates registration of all domain tools into the registry with default policy.
func RegisterBuiltins(r *Registry, eb *events.EventBus, sched *scheduler.Scheduler, notifyChan chan<- string, ragStore *rag.Store) {
	root, _ := os.Getwd()
	defaultPolicy := policy.New([]string{root, os.TempDir()}, true, false)
	RegisterBuiltinsWithPolicy(r, eb, sched, notifyChan, ragStore, defaultPolicy)
}

// RegisterBuiltinsWithPolicy registers all domain tools using an explicitly configured policy.
func RegisterBuiltinsWithPolicy(r *Registry, eb *events.EventBus, sched *scheduler.Scheduler, notifyChan chan<- string, ragStore *rag.Store, pol *policy.Policy) {
	if pol == nil {
		root, _ := os.Getwd()
		pol = policy.New([]string{root, os.TempDir()}, true, false)
	}
	taskMgr := tasks.NewManager("", eb, notifyChan)
	subMgr := subagent.GetManager()
	if eb != nil {
		subMgr.SetEventBus(eb)
	}

	// 1. Filesystem tools (view_file, write_to_file, replace_file_content, grep_search, find_by_name, list_dir)
	RegisterFSTools(r, pol)

	// 2. Command execution tools (run_command, manage_task)
	RegisterCmdTools(r, pol, taskMgr)

	// 3. Subagent orchestration tools (invoke_subagent, define_subagent, manage_subagents, send_message)
	RegisterSubagentTools(r, subMgr, eb)

	// 4. Scheduling tools (schedule, get_current_time)
	RegisterScheduleTools(r, sched, notifyChan)

	// 5. Web tools (search_web, read_url_content)
	RegisterWebTools(r)

	// 6. Interactive UI & Artifact tools (ask_question, generate_image)
	RegisterUIArtifactTools(r)

	// 7. Legacy tools & backward compatibility
	registerLegacyTools(r, pol, ragStore)

	// 8. Letta Hierarchical Memory Tools
	RegisterLettaMemoryTools(r)

	// 9. Browser-Use Automation Tools
	RegisterBrowserTools(r)
}

func registerLegacyTools(r *Registry, defaultPolicy *policy.Policy, ragStore *rag.Store) {
	r.Register(mcp.Tool{
		Name:        "run_terminal_command",
		Description: "Run a terminal command on the host system",
		InputSchema: mcp.Schema{
			Type: "object",
			Properties: map[string]interface{}{
				"command": map[string]interface{}{"type": "string"},
			},
			Required: []string{"command"},
		},
	}, func(ctx context.Context, args map[string]interface{}) (string, error) {
		cmdStr, _ := args["command"].(string)
		if cmdStr == "" {
			cmdStr, _ = args["CommandLine"].(string)
		}
		if cmdStr == "" {
			return "", fmt.Errorf("command is required")
		}
		if err := defaultPolicy.CheckCommand(cmdStr); err != nil {
			return "", err
		}

		ctxTimeout, cancel := context.WithTimeout(ctx, 1*time.Minute)
		defer cancel()

		var cmd *exec.Cmd
		if runtime.GOOS == "windows" {
			cmd = exec.CommandContext(ctxTimeout, "powershell", "-NoProfile", "-NonInteractive", "-Command", cmdStr)
		} else {
			cmd = exec.CommandContext(ctxTimeout, "sh", "-c", cmdStr)
		}

		out, err := cmd.CombinedOutput()
		if err != nil {
			return string(out), fmt.Errorf("command failed: %v, output: %s", err, string(out))
		}
		return string(out), nil
	})

	r.Register(mcp.Tool{
		Name:        "read_file",
		Description: "Read contents of a file",
		InputSchema: mcp.Schema{
			Type: "object",
			Properties: map[string]interface{}{
				"path": map[string]interface{}{"type": "string"},
			},
			Required: []string{"path"},
		},
	}, func(ctx context.Context, args map[string]interface{}) (string, error) {
		path, _ := args["path"].(string)
		if path == "" {
			path, _ = args["AbsolutePath"].(string)
		}
		if path == "" {
			return "", fmt.Errorf("path is required")
		}
		resolvedPath, err := defaultPolicy.ResolvePath(path)
		if err != nil {
			return "", err
		}
		data, err := os.ReadFile(resolvedPath)
		if err != nil {
			return "", err
		}
		return string(data), nil
	})

	r.Register(mcp.Tool{
		Name:        "write_file",
		Description: "Write contents to a file",
		InputSchema: mcp.Schema{
			Type: "object",
			Properties: map[string]interface{}{
				"path":    map[string]interface{}{"type": "string"},
				"content": map[string]interface{}{"type": "string"},
			},
			Required: []string{"path", "content"},
		},
	}, func(ctx context.Context, args map[string]interface{}) (string, error) {
		path, _ := args["path"].(string)
		content, _ := args["content"].(string)
		if path == "" {
			return "", fmt.Errorf("path is required")
		}
		resolvedPath, err := defaultPolicy.ResolvePath(path)
		if err != nil {
			return "", err
		}
		_ = os.MkdirAll(filepath.Dir(resolvedPath), 0755)
		if err := os.WriteFile(resolvedPath, []byte(content), 0644); err != nil {
			return "", err
		}
		return fmt.Sprintf("Successfully wrote %d bytes to %s", len(content), resolvedPath), nil
	})

	if ragStore != nil {
		r.Register(mcp.Tool{
			Name:        "semantic_search",
			Description: "Perform a semantic search on the local codebase or documentation",
			InputSchema: mcp.Schema{
				Type: "object",
				Properties: map[string]interface{}{
					"query_embedding": map[string]interface{}{"type": "array"},
					"top_k":           map[string]interface{}{"type": "number"},
				},
				Required: []string{"query_embedding", "top_k"},
			},
		}, func(ctx context.Context, args map[string]interface{}) (string, error) {
			queryEmbRaw, _ := args["query_embedding"].([]interface{})
			topKRaw, _ := args["top_k"].(float64)
			topK := int(topKRaw)
			if topK <= 0 {
				topK = 5
			}

			var query []float32
			for _, v := range queryEmbRaw {
				switch val := v.(type) {
				case float64:
					query = append(query, float32(val))
				case int:
					query = append(query, float32(val))
				}
			}

			results, err := ragStore.Search(ctx, query, topK)
			if err != nil {
				return "", fmt.Errorf("search failed: %w", err)
			}

			var sb strings.Builder
			for i, res := range results {
				sb.WriteString(fmt.Sprintf("%d. [%s] Score: %.4f\n%s\n\n", i+1, res.ID, res.Score, res.Text))
			}
			return sb.String(), nil
		})
	}
}

// RegisterMCPResourceTools exposes dynamic MCP tool and resource inspection.
func RegisterMCPResourceTools(r *Registry, mcpNamedClients map[string]*mcp.Client) {
	r.Register(mcp.Tool{
		Name:        "call_mcp_tool",
		Description: "Call an MCP tool dynamically from a named MCP server",
		InputSchema: mcp.Schema{
			Type: "object",
			Properties: map[string]interface{}{
				"ServerName":  map[string]interface{}{"type": "string"},
				"ToolName":    map[string]interface{}{"type": "string"},
				"Arguments":   map[string]interface{}{"type": "object"},
				"toolAction":  map[string]interface{}{"type": "string"},
				"toolSummary": map[string]interface{}{"type": "string"},
			},
			Required: []string{"ServerName", "ToolName", "Arguments"},
		},
	}, func(ctx context.Context, args map[string]interface{}) (string, error) {
		serverName, _ := args["ServerName"].(string)
		toolName, _ := args["ToolName"].(string)
		toolArgs, _ := args["Arguments"].(map[string]interface{})

		if serverName == "" || toolName == "" {
			return "", fmt.Errorf("ServerName and ToolName are required")
		}

		if client, ok := mcpNamedClients[serverName]; ok {
			content, err := client.CallTool(toolName, toolArgs)
			if err == nil && len(content) > 0 {
				var sb strings.Builder
				for _, c := range content {
					sb.WriteString(c.Text)
				}
				return sb.String(), nil
			}
		}

		for _, client := range mcpNamedClients {
			content, err := client.CallTool(toolName, toolArgs)
			if err == nil && len(content) > 0 {
				var sb strings.Builder
				for _, c := range content {
					sb.WriteString(c.Text)
				}
				return sb.String(), nil
			}
		}
		return fmt.Sprintf("MCP Tool [%s] on server [%s] executed with empty output.", toolName, serverName), nil
	})

	r.Register(mcp.Tool{
		Name:        "list_resources",
		Description: "List available resources from connected MCP servers",
		InputSchema: mcp.Schema{
			Type:       "object",
			Properties: map[string]interface{}{},
		},
	}, func(ctx context.Context, args map[string]interface{}) (string, error) {
		var sb strings.Builder
		for _, client := range mcpNamedClients {
			resources, err := client.ListResources()
			if err == nil {
				for _, res := range resources {
					sb.WriteString(fmt.Sprintf("- [%s] %s (%s)\n", res.URI, res.Name, res.MimeType))
				}
			}
		}
		if sb.Len() == 0 {
			return "No MCP resources found.", nil
		}
		return sb.String(), nil
	})

	r.Register(mcp.Tool{
		Name:        "read_resource",
		Description: "Read content from a specified MCP resource URI",
		InputSchema: mcp.Schema{
			Type: "object",
			Properties: map[string]interface{}{
				"uri":        map[string]interface{}{"type": "string", "description": "Resource URI to read"},
				"ServerName": map[string]interface{}{"type": "string"},
			},
			Required: []string{"uri"},
		},
	}, func(ctx context.Context, args map[string]interface{}) (string, error) {
		uri, ok := args["uri"].(string)
		if !ok {
			return "", fmt.Errorf("uri required")
		}
		var sb strings.Builder
		for _, client := range mcpNamedClients {
			contents, err := client.ReadResource(uri)
			if err == nil && len(contents) > 0 {
				for _, c := range contents {
					sb.WriteString(c.Text)
				}
				return sb.String(), nil
			}
		}
		return "", fmt.Errorf("resource not found or error reading URI: %s", uri)
	})
}

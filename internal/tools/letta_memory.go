package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/purav/microgravity/internal/mcp"
	"github.com/purav/microgravity/internal/sidecar"
)

// RegisterLettaMemoryTools registers Letta-style hierarchical and episodic memory tools.
func RegisterLettaMemoryTools(r *Registry) {
	// ── 1. letta_memory_core ──
	r.Register(mcp.Tool{
		Name:        "letta_memory_core",
		Description: "Manage Letta hierarchical core memory blocks (human, persona, scratchpad). Supports operations: get, set, append, replace, list, compile.",
		InputSchema: mcp.Schema{
			Type: "object",
			Properties: map[string]interface{}{
				"op": map[string]interface{}{
					"type":        "string",
					"description": "Operation: get, set, append, replace, list, compile",
					"enum":        []string{"get", "set", "append", "replace", "list", "compile"},
				},
				"block": map[string]interface{}{
					"type":        "string",
					"description": "Block name (e.g. human, persona, scratchpad, or custom block)",
				},
				"content": map[string]interface{}{
					"type":        "string",
					"description": "Content for set or append operations",
				},
				"old_str": map[string]interface{}{
					"type":        "string",
					"description": "Target substring to replace in block",
				},
				"new_str": map[string]interface{}{
					"type":        "string",
					"description": "Replacement string",
				},
				"char_limit": map[string]interface{}{
					"type":        "integer",
					"description": "Character limit for the block (default 2000)",
				},
				"toolAction":  map[string]interface{}{"type": "string"},
				"toolSummary": map[string]interface{}{"type": "string"},
			},
			Required: []string{"op"},
		},
	}, func(ctx context.Context, args map[string]interface{}) (string, error) {
		op, _ := args["op"].(string)
		if op == "" {
			return "", fmt.Errorf("op is required (get, set, append, replace, list, compile)")
		}

		req := map[string]interface{}{
			"op": "core_" + strings.TrimPrefix(op, "core_"),
		}
		if b, ok := args["block"].(string); ok && b != "" {
			req["block"] = b
		}
		if c, ok := args["content"].(string); ok {
			req["content"] = c
		}
		if o, ok := args["old_str"].(string); ok {
			req["old_str"] = o
		}
		if n, ok := args["new_str"].(string); ok {
			req["new_str"] = n
		}
		if l, ok := args["char_limit"].(float64); ok {
			req["char_limit"] = int(l)
		} else if lInt, ok := args["char_limit"].(int); ok {
			req["char_limit"] = lInt
		}

		resp, err := RunLettaMemory(ctx, req)
		if err != nil {
			return "", err
		}

		if op == "compile" {
			if comp, ok := resp["compiled"].(string); ok {
				return comp, nil
			}
		}

		respBytes, _ := json.MarshalIndent(resp, "", "  ")
		return string(respBytes), nil
	})

	// ── 2. letta_memory_episodic ──
	r.Register(mcp.Tool{
		Name:        "letta_memory_episodic",
		Description: "Record and query Letta episodic recall memory (events, messages, observations, thoughts) with temporal, role, importance, and keyword filters.",
		InputSchema: mcp.Schema{
			Type: "object",
			Properties: map[string]interface{}{
				"op": map[string]interface{}{
					"type":        "string",
					"description": "Operation: log, query, recent, compact",
					"enum":        []string{"log", "query", "recent", "compact"},
				},
				"role": map[string]interface{}{
					"type":        "string",
					"description": "Event role (user, assistant, system, tool, agent)",
				},
				"content": map[string]interface{}{
					"type":        "string",
					"description": "Message or event content",
				},
				"event_type": map[string]interface{}{
					"type":        "string",
					"description": "Type of event (message, action, observation, thought, checkpoint)",
				},
				"session_id": map[string]interface{}{
					"type":        "string",
					"description": "Session ID scope (default: default)",
				},
				"tags": map[string]interface{}{
					"type":        "array",
					"description": "Categorization tags for filtering",
				},
				"importance": map[string]interface{}{
					"type":        "number",
					"description": "Importance score (1.0 to 10.0)",
				},
				"query": map[string]interface{}{
					"type":        "string",
					"description": "Search query for semantic/keyword retrieval",
				},
				"limit": map[string]interface{}{
					"type":        "integer",
					"description": "Maximum events to return",
				},
				"summary_text": map[string]interface{}{
					"type":        "string",
					"description": "Summary text when compacting older episodes",
				},
				"toolAction":  map[string]interface{}{"type": "string"},
				"toolSummary": map[string]interface{}{"type": "string"},
			},
			Required: []string{"op"},
		},
	}, func(ctx context.Context, args map[string]interface{}) (string, error) {
		op, _ := args["op"].(string)
		if op == "" {
			return "", fmt.Errorf("op is required (log, query, recent, compact)")
		}

		req := map[string]interface{}{
			"op": "episodic_" + strings.TrimPrefix(op, "episodic_"),
		}
		for k, v := range args {
			if k != "op" && k != "toolAction" && k != "toolSummary" {
				req[k] = v
			}
		}

		resp, err := RunLettaMemory(ctx, req)
		if err != nil {
			return "", err
		}

		respBytes, _ := json.MarshalIndent(resp, "", "  ")
		return string(respBytes), nil
	})

	// ── 3. letta_memory_archival ──
	r.Register(mcp.Tool{
		Name:        "letta_memory_archival",
		Description: "Store, retrieve, and search Letta archival knowledge passages using semantic similarity and tag indexing.",
		InputSchema: mcp.Schema{
			Type: "object",
			Properties: map[string]interface{}{
				"op": map[string]interface{}{
					"type":        "string",
					"description": "Operation: insert, search, delete, list",
					"enum":        []string{"insert", "search", "delete", "list"},
				},
				"content": map[string]interface{}{
					"type":        "string",
					"description": "Knowledge passage text to store",
				},
				"tags": map[string]interface{}{
					"type":        "array",
					"description": "Passage categorization tags",
				},
				"source": map[string]interface{}{
					"type":        "string",
					"description": "Source origin (user, web, documentation, agent)",
				},
				"query": map[string]interface{}{
					"type":        "string",
					"description": "Search query for semantic/relevance ranking",
				},
				"top_k": map[string]interface{}{
					"type":        "integer",
					"description": "Number of top results to return (default 5)",
				},
				"tag_filter": map[string]interface{}{
					"type":        "string",
					"description": "Filter results by specific tag",
				},
				"passage_id": map[string]interface{}{
					"type":        "string",
					"description": "Passage ID for deletion",
				},
				"toolAction":  map[string]interface{}{"type": "string"},
				"toolSummary": map[string]interface{}{"type": "string"},
			},
			Required: []string{"op"},
		},
	}, func(ctx context.Context, args map[string]interface{}) (string, error) {
		op, _ := args["op"].(string)
		if op == "" {
			return "", fmt.Errorf("op is required (insert, search, delete, list)")
		}

		req := map[string]interface{}{
			"op": "archival_" + strings.TrimPrefix(op, "archival_"),
		}
		for k, v := range args {
			if k != "op" && k != "toolAction" && k != "toolSummary" {
				req[k] = v
			}
		}

		resp, err := RunLettaMemory(ctx, req)
		if err != nil {
			return "", err
		}

		respBytes, _ := json.MarshalIndent(resp, "", "  ")
		return string(respBytes), nil
	})

	// ── 4. letta_memory_stats ──
	r.Register(mcp.Tool{
		Name:        "letta_memory_stats",
		Description: "Retrieve memory usage statistics and block counts across all Letta memory tiers (core, episodic, archival).",
		InputSchema: mcp.Schema{
			Type:       "object",
			Properties: map[string]interface{}{},
		},
	}, func(ctx context.Context, args map[string]interface{}) (string, error) {
		resp, err := RunLettaMemory(ctx, map[string]interface{}{"op": "stats"})
		if err != nil {
			return "", err
		}
		respBytes, _ := json.MarshalIndent(resp, "", "  ")
		return string(respBytes), nil
	})
}

// RunLettaMemory sends a JSON request to mg_memory_letta.py and parses the JSON response.
func RunLettaMemory(ctx context.Context, req map[string]interface{}) (map[string]interface{}, error) {
	scriptPath := FindLettaMemoryScript()
	python := sidecar.FindPython()

	reqBytes, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal Letta memory request: %w", err)
	}

	cmd := exec.CommandContext(ctx, python, scriptPath)
	cmd.Stdin = bytes.NewReader(reqBytes)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		stderrStr := stderr.String()
		if stderrStr != "" {
			return nil, fmt.Errorf("Letta memory script failed: %v\nstderr: %s", err, stderrStr)
		}
		return nil, fmt.Errorf("Letta memory script failed: %v", err)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		return nil, fmt.Errorf("failed to parse Letta memory output: %v\nraw: %s", err, stdout.String())
	}

	if status, ok := result["status"].(string); ok && status == "error" {
		errMsg, _ := result["error"].(string)
		return nil, fmt.Errorf("Letta memory error: %s", errMsg)
	}

	return result, nil
}

// FindLettaMemoryScript locates mg_memory_letta.py using the dynamic sidecar locator.
func FindLettaMemoryScript() string {
	return sidecar.FindScript(filepath.Join("letta", "mg_memory_letta.py"), "mg_memory_letta.py")
}

// GetCompiledLettaMemory retrieves the compiled XML core memory block (<core_memory>...</core_memory>)
// via RunLettaMemory. Returns empty string if unavailable or on error.
func GetCompiledLettaMemory(ctx context.Context) string {
	if ctx == nil {
		ctx = context.Background()
	}
	resp, err := RunLettaMemory(ctx, map[string]interface{}{"op": "core_compile"})
	if err != nil {
		return ""
	}
	if comp, ok := resp["compiled"].(string); ok {
		return comp
	}
	return ""
}


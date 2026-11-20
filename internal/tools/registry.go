package tools

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/purav/microgravity/internal/cache"
	"github.com/purav/microgravity/internal/mcp"
	"github.com/purav/microgravity/internal/policy"
)

type ToolHandler func(ctx context.Context, args map[string]interface{}) (string, error)

// Registry manages native tool schemas and handlers.
type Registry struct {
	mu            sync.RWMutex
	handlers      map[string]ToolHandler
	nativeSchemas []mcp.Tool
	cache         *cache.TTLCache
	pol           *policy.Policy
	dryRun        bool
}

// SetCache attaches a TTL result cache to this registry. When set, Execute
// checks the cache before invoking a handler and stores successful results
// afterward. Cache is keyed by tool name + deterministic argument hash.
func (r *Registry) SetCache(c *cache.TTLCache) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.cache = c
}

// SetPolicy attaches a fine-grained tool execution policy. When set, Execute
// calls CheckTool before invoking a handler, denying tools that are blocked
// by allow/deny rules.
func (r *Registry) SetPolicy(p *policy.Policy) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.pol = p
}

// SetDryRun enables or disables dry-run mode. When enabled, Execute prints
// the planned tool call instead of executing it and returns a placeholder
// result, allowing operators to preview what an agent turn would do.
func (r *Registry) SetDryRun(dry bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.dryRun = dry
}

// NewRegistry creates a new native tools registry.
func NewRegistry() *Registry {
	return &Registry{
		handlers:      make(map[string]ToolHandler),
		nativeSchemas: make([]mcp.Tool, 0),
	}
}

// Register adds a tool to the native registry. If the tool is already registered, it silently skips.
func (r *Registry) Register(schema mcp.Tool, handler ToolHandler) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.handlers[schema.Name]; exists {
		return nil
	}

	r.handlers[schema.Name] = handler
	r.nativeSchemas = append(r.nativeSchemas, schema)
	return nil
}

// GetNativeTools returns the list of native tool schemas.
func (r *Registry) GetNativeTools() []mcp.Tool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	// Return a copy to prevent external modification
	schemasCopy := make([]mcp.Tool, len(r.nativeSchemas))
	copy(schemasCopy, r.nativeSchemas)
	return schemasCopy
}

// IsNative checks if a tool exists in the native registry.
func (r *Registry) IsNative(name string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	_, exists := r.handlers[name]
	return exists
}

// Execute runs a native tool by name.
func (r *Registry) Execute(ctx context.Context, name string, args map[string]interface{}) (string, error) {
	r.mu.RLock()
	handler, exists := r.handlers[name]
	rcache := r.cache
	rpol := r.pol
	rdry := r.dryRun
	r.mu.RUnlock()

	if !exists {
		return "", fmt.Errorf("native tool %s not found", name)
	}

	// Dry-run mode: preview the planned call without executing.
	if rdry {
		argsStr := ""
		if len(args) > 0 {
			parts := make([]string, 0, len(args))
			for k, v := range args {
				parts = append(parts, fmt.Sprintf("%s=%v", k, v))
			}
			argsStr = " " + strings.Join(parts, ", ")
		}
		return fmt.Sprintf("[DRY-RUN] Would execute tool %s%s", name, argsStr), nil
	}

	// Check fine-grained tool policy before executing the handler.
	if rpol != nil {
		if err := rpol.CheckTool(name); err != nil {
			return "", err
		}
	}

	// Check cache before executing the handler.
	var cacheKey string
	if rcache != nil {
		cacheKey = cache.ToolKey(name, args)
		if val, ok := rcache.Get(cacheKey); ok {
			return val, nil
		}
	}

	// Execute without holding the lock to allow concurrent tool executions.
	result, err := handler(ctx, args)

	// Store successful results in cache.
	if rcache != nil && err == nil && cacheKey != "" {
		rcache.Set(cacheKey, result)
	}

	return result, err
}

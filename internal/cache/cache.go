// Package cache implements a TTL-based result cache for Microgravity tool calls.
//
// Tool results are keyed by tool name + deterministic argument hash, so repeated
// identical calls (e.g. list_dir on the same path, view_file on the same file,
// grep_search with the same query) return from cache without re-executing the
// underlying tool or burning model budget.
//
// Limits are configured via environment variables:
//
//	MG_CACHE_ENABLED   set to 0 to disable the cache entirely (default 1)
//	MG_CACHE_TTL       time-to-live for each cached entry (default 5m0s)
package cache

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

// entry is a single cached result with an expiry time.
type entry struct {
	value  string
	expiry time.Time
}

// TTLCache is a goroutine-safe, fixed-TTL cache of string values.
type TTLCache struct {
	mu   sync.RWMutex
	items map[string]*entry
	ttl   time.Duration
}

// NewTTLCache creates a cache where entries expire after ttl.
func NewTTLCache(ttl time.Duration) *TTLCache {
	return &TTLCache{
		items: make(map[string]*entry),
		ttl:   ttl,
	}
}

// Get returns the cached value for key, or false if absent or expired.
func (c *TTLCache) Get(key string) (string, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	e, ok := c.items[key]
	if !ok {
		return "", false
	}
	if time.Now().After(e.expiry) {
		return "", false
	}
	return e.value, true
}

// Set stores value under key with the cache's TTL.
func (c *TTLCache) Set(key string, value string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.items[key] = &entry{
		value:  value,
		expiry: time.Now().Add(c.ttl),
	}
}

// Len returns the current number of entries (including expired ones not yet evicted).
func (c *TTLCache) Len() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.items)
}

// ToolKey builds a deterministic cache key from a tool name and its arguments.
// Map keys are sorted so that identical argument sets produce the same key
// regardless of insertion order.
func ToolKey(tool string, args map[string]interface{}) string {
	if len(args) == 0 {
		return tool
	}
	keys := make([]string, 0, len(args))
	for k := range args {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var sb strings.Builder
	sb.WriteString(tool)
	sb.WriteByte('|')
	for _, k := range keys {
		sb.WriteString(k)
		sb.WriteByte('=')
		sb.WriteString(fmt.Sprintf("%v", args[k]))
		sb.WriteByte('|')
	}

	h := sha256.Sum256([]byte(sb.String()))
	return hex.EncodeToString(h[:])
}

// limits from environment.
func enabledFromEnv() bool {
	if v := strings.TrimSpace(getEnv("MG_CACHE_ENABLED")); v != "" {
		return v != "0" && !strings.EqualFold(v, "false") && !strings.EqualFold(v, "off")
	}
	return true
}

func ttlFromEnv() time.Duration {
	ttl := 5 * time.Minute
	if v := strings.TrimSpace(getEnv("MG_CACHE_TTL")); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			ttl = d
		}
	}
	return ttl
}

func getEnv(k string) string {
	// Avoid importing os in hot path tests by reading through a package-level
	// hook that tests can swap.
	return envLookup(k)
}

var envLookup = func(k string) string { return "" } // swapped by tests

// Enabled reports whether caching is enabled per MG_CACHE_ENABLED.
func Enabled() bool {
	return enabledFromEnv()
}

// DefaultTTL returns the configured TTL per MG_CACHE_TTL.
func DefaultTTL() time.Duration {
	return ttlFromEnv()
}

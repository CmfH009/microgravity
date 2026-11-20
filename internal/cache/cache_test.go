package cache

import (
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestTTLCache_SetGet(t *testing.T) {
	c := NewTTLCache(time.Minute)

	val, ok := c.Get("missing")
	assert.Empty(t, val)
	assert.False(t, ok)

	c.Set("foo", "bar")
	val, ok = c.Get("foo")
	assert.True(t, ok)
	assert.Equal(t, "bar", val)
}

func TestTTLCache_Expiration(t *testing.T) {
	c := NewTTLCache(50 * time.Millisecond)
	c.Set("foo", "bar")

	val, ok := c.Get("foo")
	assert.True(t, ok)
	assert.Equal(t, "bar", val)

	time.Sleep(100 * time.Millisecond)
	val, ok = c.Get("foo")
	assert.False(t, ok)
	assert.Empty(t, val)
}

func TestTTLCache_Overwrite(t *testing.T) {
	c := NewTTLCache(time.Minute)
	c.Set("foo", "v1")
	c.Set("foo", "v2")

	val, ok := c.Get("foo")
	assert.True(t, ok)
	assert.Equal(t, "v2", val)
}

func TestToolKey_Deterministic(t *testing.T) {
	k1 := ToolKey("list_dir", map[string]interface{}{"path": ".", "recursive": true})
	k2 := ToolKey("list_dir", map[string]interface{}{"recursive": true, "path": "."})
	assert.Equal(t, k1, k2, "same args in different order must produce same key")

	k3 := ToolKey("list_dir", map[string]interface{}{"path": ".."})
	assert.NotEqual(t, k1, k3, "different args must produce different keys")

	k4 := ToolKey("different_tool", map[string]interface{}{"path": "."})
	assert.NotEqual(t, k1, k4, "different tool names must produce different keys")

	kEmpty := ToolKey("tool", map[string]interface{}{})
	kEmpty2 := ToolKey("tool", nil)
	assert.Equal(t, kEmpty, kEmpty2)
}

func TestEnvDefaults(t *testing.T) {
	// Reset env hooks
	envLookup = func(k string) string { return "" }
	assert.True(t, Enabled())
	assert.Equal(t, 5*time.Minute, DefaultTTL())
}

func TestEnvOverrides(t *testing.T) {
	envLookup = func(k string) string {
		switch k {
		case "MG_CACHE_ENABLED":
			return "0"
		case "MG_CACHE_TTL":
			return "10s"
		}
		return ""
	}
	assert.False(t, Enabled())
	assert.Equal(t, 10*time.Second, DefaultTTL())
}

func TestMain(m *testing.M) {
	// Ensure env hook is reset after tests run.
	code := m.Run()
	envLookup = func(k string) string { return "" }
	os.Exit(code)
}

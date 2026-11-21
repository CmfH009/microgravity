package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/CmfH009/microgravity/internal/mcp"
	"github.com/CmfH009/microgravity/internal/sidecar"
)

var browserScriptRelPath = filepath.Join("browser", "mg_tool_browser.py")

const browserScriptFallback = "mg_tool_browser.py"

// RegisterBrowserTools registers Browser-Use action tools to the native registry.
func RegisterBrowserTools(r *Registry) {
	// ── 1. browser_navigate ──
	r.Register(mcp.Tool{
		Name:        "browser_navigate",
		Description: "Navigate to a URL using the Microgravity Browser engine. Returns page title, status code, summary markdown, and indexed interactive elements.",
		InputSchema: mcp.Schema{
			Type: "object",
			Properties: map[string]interface{}{
				"url": map[string]interface{}{
					"type":        "string",
					"description": "URL to navigate to (http://, https://, or file://)",
				},
				"new_tab": map[string]interface{}{
					"type":        "boolean",
					"description": "Open in a new tab (default false)",
				},
				"toolAction":  map[string]interface{}{"type": "string"},
				"toolSummary": map[string]interface{}{"type": "string"},
			},
			Required: []string{"url"},
		},
	}, func(ctx context.Context, args map[string]interface{}) (string, error) {
		urlStr, _ := args["url"].(string)
		if urlStr == "" {
			urlStr, _ = args["target"].(string)
		}
		if urlStr == "" {
			return "", fmt.Errorf("url is required")
		}

		req := map[string]interface{}{
			"action": "navigate",
			"url":    urlStr,
		}
		if nt, ok := args["new_tab"].(bool); ok {
			req["new_tab"] = nt
		}

		resp, err := RunBrowserTool(ctx, req)
		if err != nil {
			return "", err
		}

		respBytes, _ := json.MarshalIndent(resp, "", "  ")
		return string(respBytes), nil
	})

	// ── 2. browser_interact ──
	r.Register(mcp.Tool{
		Name:        "browser_interact",
		Description: "Perform interaction on the active browser page (click, input, scroll, search_page, find_elements, switch_tab, close_tab).",
		InputSchema: mcp.Schema{
			Type: "object",
			Properties: map[string]interface{}{
				"action": map[string]interface{}{
					"type":        "string",
					"description": "Interaction action: click, input, scroll, search_page, find_elements, switch_tab, close_tab",
					"enum":        []string{"click", "input", "scroll", "search_page", "find_elements", "switch_tab", "close_tab"},
				},
				"selector": map[string]interface{}{
					"type":        "string",
					"description": "CSS selector or element text",
				},
				"index": map[string]interface{}{
					"type":        "integer",
					"description": "Element 1-based index from page elements list",
				},
				"text": map[string]interface{}{
					"type":        "string",
					"description": "Text to enter for input action",
				},
				"clear": map[string]interface{}{
					"type":        "boolean",
					"description": "Clear field before typing (default true)",
				},
				"submit": map[string]interface{}{
					"type":        "boolean",
					"description": "Press Enter after typing (default false)",
				},
				"down": map[string]interface{}{
					"type":        "boolean",
					"description": "Scroll direction: true for down, false for up",
				},
				"pages": map[string]interface{}{
					"type":        "number",
					"description": "Number of pages to scroll (default 1.0)",
				},
				"pattern": map[string]interface{}{
					"type":        "string",
					"description": "Search pattern for search_page",
				},
				"regex": map[string]interface{}{
					"type":        "boolean",
					"description": "Treat search pattern as regular expression",
				},
				"tab_id": map[string]interface{}{
					"type":        "string",
					"description": "Tab ID to switch to or close",
				},
				"toolAction":  map[string]interface{}{"type": "string"},
				"toolSummary": map[string]interface{}{"type": "string"},
			},
			Required: []string{"action"},
		},
	}, func(ctx context.Context, args map[string]interface{}) (string, error) {
		action, _ := args["action"].(string)
		if action == "" {
			return "", fmt.Errorf("action is required")
		}

		req := map[string]interface{}{
			"action": action,
		}
		for k, v := range args {
			if k != "toolAction" && k != "toolSummary" {
				req[k] = v
			}
		}

		resp, err := RunBrowserTool(ctx, req)
		if err != nil {
			return "", err
		}

		respBytes, _ := json.MarshalIndent(resp, "", "  ")
		return string(respBytes), nil
	})

	// ── 3. browser_extract ──
	r.Register(mcp.Tool{
		Name:        "browser_extract",
		Description: "Extract structured markdown content, hyperlinks, and images from the active or specified webpage.",
		InputSchema: mcp.Schema{
			Type: "object",
			Properties: map[string]interface{}{
				"url": map[string]interface{}{
					"type":        "string",
					"description": "Optional URL to navigate to before extraction",
				},
				"query": map[string]interface{}{
					"type":        "string",
					"description": "Optional keyword or topic filter to extract relevant sections",
				},
				"extract_links": map[string]interface{}{
					"type":        "boolean",
					"description": "Include all page hyperlinks in output",
				},
				"extract_images": map[string]interface{}{
					"type":        "boolean",
					"description": "Include image sources and alt tags",
				},
				"start_from_char": map[string]interface{}{
					"type":        "integer",
					"description": "Character offset for paginated markdown extraction",
				},
				"toolAction":  map[string]interface{}{"type": "string"},
				"toolSummary": map[string]interface{}{"type": "string"},
			},
		},
	}, func(ctx context.Context, args map[string]interface{}) (string, error) {
		req := map[string]interface{}{
			"action": "extract",
		}
		for k, v := range args {
			if k != "toolAction" && k != "toolSummary" {
				req[k] = v
			}
		}

		resp, err := RunBrowserTool(ctx, req)
		if err != nil {
			return "", err
		}

		// If markdown is present, return rendered markdown or json
		if md, ok := resp["markdown"].(string); ok && md != "" {
			var sb strings.Builder
			title, _ := resp["title"].(string)
			urlStr, _ := resp["url"].(string)
			sb.WriteString(fmt.Sprintf("# %s\nURL: %s\n\n%s", title, urlStr, md))
			return sb.String(), nil
		}

		respBytes, _ := json.MarshalIndent(resp, "", "  ")
		return string(respBytes), nil
	})

	// ── 4. browser_screenshot ──
	r.Register(mcp.Tool{
		Name:        "browser_screenshot",
		Description: "Capture a visual screenshot of the current browser page (saved to PNG/SVG).",
		InputSchema: mcp.Schema{
			Type: "object",
			Properties: map[string]interface{}{
				"file_name": map[string]interface{}{
					"type":        "string",
					"description": "Output filename or path (e.g. page.png)",
				},
				"full_page": map[string]interface{}{
					"type":        "boolean",
					"description": "Capture full scrollable page height (default false)",
				},
				"url": map[string]interface{}{
					"type":        "string",
					"description": "Optional URL to navigate to before capture",
				},
				"toolAction":  map[string]interface{}{"type": "string"},
				"toolSummary": map[string]interface{}{"type": "string"},
			},
		},
	}, func(ctx context.Context, args map[string]interface{}) (string, error) {
		req := map[string]interface{}{
			"action": "screenshot",
		}
		for k, v := range args {
			if k != "toolAction" && k != "toolSummary" {
				req[k] = v
			}
		}

		resp, err := RunBrowserTool(ctx, req)
		if err != nil {
			return "", err
		}

		path, _ := resp["screenshot_path"].(string)
		title, _ := resp["title"].(string)
		return fmt.Sprintf("Screenshot captured successfully for '%s'.\nPath: %s", title, path), nil
	})

	// ── 5. browser_execute_plan ──
	r.Register(mcp.Tool{
		Name:        "browser_execute_plan",
		Description: "Execute a multi-step composite browser automation action plan.",
		InputSchema: mcp.Schema{
			Type: "object",
			Properties: map[string]interface{}{
				"actions": map[string]interface{}{
					"type":        "array",
					"description": "Array of browser action objects, e.g. [{'action': 'navigate', 'url': '...'}, {'action': 'click', 'selector': '...'}]",
				},
				"toolAction":  map[string]interface{}{"type": "string"},
				"toolSummary": map[string]interface{}{"type": "string"},
			},
			Required: []string{"actions"},
		},
	}, func(ctx context.Context, args map[string]interface{}) (string, error) {
		actions, ok := args["actions"]
		if !ok {
			return "", fmt.Errorf("actions array is required")
		}

		req := map[string]interface{}{
			"action":  "execute_plan",
			"actions": actions,
		}

		resp, err := RunBrowserTool(ctx, req)
		if err != nil {
			return "", err
		}

		respBytes, _ := json.MarshalIndent(resp, "", "  ")
		return string(respBytes), nil
	})
}

// RunBrowserTool sends a JSON request to mg_tool_browser.py via the shared sidecar runner.
func RunBrowserTool(ctx context.Context, req map[string]interface{}) (map[string]interface{}, error) {
	return sidecar.Invoke(ctx, browserScriptRelPath, browserScriptFallback, req)
}

// FindBrowserToolScript locates mg_tool_browser.py using the dynamic sidecar locator.
func FindBrowserToolScript() string {
	return sidecar.FindScript(browserScriptRelPath, browserScriptFallback)
}

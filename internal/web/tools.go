package web

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"github.com/purav/microgravity/internal/mcp"
	"github.com/purav/microgravity/internal/tools"
)

// RegisterWebTools registers the web-related native tools.
func RegisterWebTools(registry *tools.Registry) error {
	// read_url tool
	readURLSchema := mcp.Tool{
		Name:        "read_url",
		Description: "Fetches a webpage and converts its HTML content to markdown.",
		InputSchema: mcp.Schema{
			Type: "object",
			Properties: map[string]interface{}{
				"url": map[string]interface{}{
					"type":        "string",
					"description": "The URL of the webpage to fetch.",
				},
			},
			Required: []string{"url"},
		},
	}

	err := registry.Register(readURLSchema, func(ctx context.Context, args map[string]interface{}) (string, error) {
		urlStr, ok := args["url"].(string)
		if !ok {
			return "", fmt.Errorf("url is required and must be a string")
		}
		
		urlStr = strings.TrimSpace(urlStr)
		if urlStr == "" {
			return "", fmt.Errorf("url cannot be empty")
		}

		u, err := url.ParseRequestURI(urlStr)
		if err != nil || u.Scheme == "" || u.Host == "" {
			return "", fmt.Errorf("invalid url provided: %s", urlStr)
		}

		markdown, err := Extract(ctx, urlStr)
		if err != nil {
			return "", fmt.Errorf("failed to read url: %w", err)
		}
		return markdown, nil
	})

	return err
}

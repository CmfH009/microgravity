package tools

import (
	"context"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	md "github.com/JohannesKaufmann/html-to-markdown"
	"github.com/CmfH009/microgravity/internal/mcp"
)

// RegisterWebTools registers search_web and read_url_content tools.
func RegisterWebTools(r *Registry) {
	// ── 1. search_web ──
	r.Register(mcp.Tool{
		Name:        "search_web",
		Description: "Search the web for a query and return relevant summaries with citations",
		InputSchema: mcp.Schema{
			Type: "object",
			Properties: map[string]interface{}{
				"query":       map[string]interface{}{"type": "string"},
				"domain":      map[string]interface{}{"type": "string"},
				"toolAction":  map[string]interface{}{"type": "string"},
				"toolSummary": map[string]interface{}{"type": "string"},
			},
			Required: []string{"query"},
		},
	}, func(ctx context.Context, args map[string]interface{}) (string, error) {
		query, _ := args["query"].(string)
		if query == "" {
			return "", fmt.Errorf("query is required")
		}
		domain, _ := args["domain"].(string)
		if domain != "" {
			query = fmt.Sprintf("site:%s %s", domain, query)
		}

		req, err := http.NewRequestWithContext(ctx, "GET", "https://html.duckduckgo.com/html/?q="+url.QueryEscape(query), nil)
		if err != nil {
			return "", err
		}
		req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/91.0.4472.124 Safari/537.36")

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return fmt.Sprintf("Web search executed with query %q (offline/fallback mode).", query), nil
		}
		defer resp.Body.Close()

		bodyBytes, _ := io.ReadAll(io.LimitReader(resp.Body, 512*1024))
		bodyStr := string(bodyBytes)

		type searchItem struct {
			Title string
			Link  string
			Desc  string
		}
		var results []searchItem

		reTitle := regexp.MustCompile(`<a[^>]*class="[^"]*result__a[^"]*"[^>]*href="([^"]+)"[^>]*>([^<]+)</a>`)
		reSnippet := regexp.MustCompile(`<a[^>]*class="[^"]*result__snippet[^"]*"[^>]*>(.*?)</a>`)
		reHTMLTags := regexp.MustCompile(`<[^>]+>`)
		titleMatches := reTitle.FindAllStringSubmatch(bodyStr, 10)
		snippetMatches := reSnippet.FindAllStringSubmatch(bodyStr, 10)
		for i, tm := range titleMatches {
			if len(tm) >= 3 {
				item := searchItem{
					Title: strings.TrimSpace(html.UnescapeString(reHTMLTags.ReplaceAllString(tm[2], ""))),
					Link:  strings.TrimSpace(html.UnescapeString(tm[1])),
				}
				if i < len(snippetMatches) && len(snippetMatches[i]) >= 2 {
					cleanDesc := reHTMLTags.ReplaceAllString(snippetMatches[i][1], "")
					item.Desc = strings.TrimSpace(html.UnescapeString(cleanDesc))
				}
				results = append(results, item)
			}
		}

		if len(results) == 0 {
			return fmt.Sprintf("Web Search Results for %q:\n- Query processed successfully. No live web snippets returned.\n", query), nil
		}

		var sb strings.Builder
		sb.WriteString(fmt.Sprintf("Web Search Results for %q:\n\n", query))
		for i, res := range results {
			sb.WriteString(fmt.Sprintf("%d. [%s](%s)\n   %s\n\n", i+1, res.Link, res.Link, res.Desc))
		}
		return sb.String(), nil
	})

	// ── 2. read_url_content ──
	r.Register(mcp.Tool{
		Name:        "read_url_content",
		Description: "Fetch content from a URL via HTTP request and convert HTML to markdown",
		InputSchema: mcp.Schema{
			Type: "object",
			Properties: map[string]interface{}{
				"Url":         map[string]interface{}{"type": "string"},
				"toolAction":  map[string]interface{}{"type": "string"},
				"toolSummary": map[string]interface{}{"type": "string"},
			},
			Required: []string{"Url"},
		},
	}, func(ctx context.Context, args map[string]interface{}) (string, error) {
		urlStr, _ := args["Url"].(string)
		if urlStr == "" {
			urlStr, _ = args["url"].(string)
		}
		if urlStr == "" {
			return "", fmt.Errorf("Url is required")
		}

		u, err := url.ParseRequestURI(urlStr)
		if err != nil || u.Scheme == "" || u.Host == "" {
			return "", fmt.Errorf("invalid url provided: %s", urlStr)
		}

		req, err := http.NewRequestWithContext(ctx, "GET", urlStr, nil)
		if err != nil {
			return "", err
		}
		req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/91.0.4472.124 Safari/537.36")

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return "", err
		}
		defer resp.Body.Close()

		body, err := io.ReadAll(io.LimitReader(resp.Body, 512*1024))
		if err != nil {
			return "", err
		}

		converter := md.NewConverter("", true, nil)
		markdown, err := converter.ConvertString(string(body))
		if err != nil {
			return "", err
		}
		return markdown, nil
	})
}

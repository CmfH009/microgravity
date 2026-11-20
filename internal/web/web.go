package web

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/JohannesKaufmann/html-to-markdown"
	"github.com/gocolly/colly/v2"
)

var (
	BaseSearchURL = "https://html.duckduckgo.com/html/"
	UserAgent     = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/91.0.4472.124 Safari/537.36"
)

type contextTransport struct {
	ctx context.Context
	rt  http.RoundTripper
}

func (c *contextTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.WithContext(c.ctx)
	return c.rt.RoundTrip(req)
}

type SearchResult struct {
	Title       string
	Link        string
	Description string
}

func Search(ctx context.Context, query string) ([]SearchResult, error) {
	var results []SearchResult
	c := colly.NewCollector(
		colly.UserAgent(UserAgent),
	)
	c.SetRequestTimeout(10 * time.Second)
	c.WithTransport(&contextTransport{ctx: ctx, rt: http.DefaultTransport})

	c.OnRequest(func(r *colly.Request) {
		r.Headers.Set("Referer", "https://html.duckduckgo.com/")
	})

	c.OnHTML(".result", func(e *colly.HTMLElement) {
		title := e.ChildText(".result__title")
		link := e.ChildAttr(".result__url", "href")
		snippet := e.ChildText(".result__snippet")

		if title != "" && link != "" {
			results = append(results, SearchResult{
				Title:       strings.TrimSpace(title),
				Link:        strings.TrimSpace(link),
				Description: strings.TrimSpace(snippet),
			})
		}
	})

	var blocked bool
	c.OnResponse(func(r *colly.Response) {
		body := string(r.Body)
		if strings.Contains(strings.ToLower(body), "captcha") || strings.Contains(strings.ToLower(body), "automated request") {
			blocked = true
		}
	})

	err := c.Post(BaseSearchURL, map[string]string{"q": query})
	if err != nil {
		return nil, err
	}

	if blocked {
		return nil, fmt.Errorf("duckduckgo search blocked (CAPTCHA or automated request limit)")
	}

	if len(results) > 10 {
		results = results[:10]
	}

	return results, nil
}

func Extract(ctx context.Context, targetURL string) (string, error) {
	c := colly.NewCollector(
		colly.UserAgent(UserAgent),
	)
	c.SetRequestTimeout(10 * time.Second)
	c.WithTransport(&contextTransport{ctx: ctx, rt: http.DefaultTransport})

	var htmlContent string
	c.OnResponse(func(r *colly.Response) {
		htmlContent = string(r.Body)
	})

	err := c.Visit(targetURL)
	if err != nil {
		return "", err
	}

	converter := md.NewConverter("", true, nil)
	markdown, err := converter.ConvertString(htmlContent)
	if err != nil {
		return "", fmt.Errorf("failed to convert html to markdown: %w", err)
	}

	return markdown, nil
}

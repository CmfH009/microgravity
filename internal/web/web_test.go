package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSearch(t *testing.T) {
	mockHTML := `
		<html>
		<body>
			<div class="result">
				<h2 class="result__title">Test Title</h2>
				<a class="result__url" href="https://example.com/test">Link</a>
				<div class="result__snippet">This is a snippet</div>
			</div>
		</body>
		</html>
	`
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(mockHTML))
	}))
	defer ts.Close()

	// Override the base URL
	oldBase := BaseSearchURL
	BaseSearchURL = ts.URL
	defer func() { BaseSearchURL = oldBase }()

	results, err := Search(context.Background(), "test query")
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("Expected 1 result, got %d", len(results))
	}
	if results[0].Title != "Test Title" {
		t.Errorf("Expected title 'Test Title', got '%s'", results[0].Title)
	}
}

func TestSearchCAPTCHA(t *testing.T) {
	mockHTML := `
		<html>
		<body>
			<div>duckduckgo blocked you</div>
			<div>something about captcha</div>
		</body>
		</html>
	`
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(mockHTML))
	}))
	defer ts.Close()

	oldBase := BaseSearchURL
	BaseSearchURL = ts.URL
	defer func() { BaseSearchURL = oldBase }()

	_, err := Search(context.Background(), "test query")
	if err == nil {
		t.Fatal("Expected error due to CAPTCHA, got nil")
	}
	if !strings.Contains(err.Error(), "blocked") {
		t.Errorf("Expected blocked error, got %v", err)
	}
}

func TestExtract(t *testing.T) {
	mockHTML := `
		<html>
		<head><title>Example Domain</title></head>
		<body>
			<h1>Example Domain</h1>
			<p>This domain is for use in illustrative examples.</p>
		</body>
		</html>
	`
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(mockHTML))
	}))
	defer ts.Close()

	markdown, err := Extract(context.Background(), ts.URL)
	if err != nil {
		t.Fatalf("Extract failed: %v", err)
	}
	if !strings.Contains(markdown, "Example Domain") {
		t.Errorf("Expected markdown to contain 'Example Domain', got: %s", markdown)
	}
}

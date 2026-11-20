package git

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	
	"github.com/google/go-github/v62/github"
	"github.com/purav/microgravity/internal/mcp"
	"github.com/purav/microgravity/internal/tools"
)

// RegisterGitTools registers git and github tools with the registry.
func RegisterGitTools(registry *tools.Registry) error {
	diffSchema := mcp.Tool{
		Name:        "git_diff",
		Description: "Get the git diff of a repository.",
		InputSchema: mcp.Schema{
			Type: "object",
			Properties: map[string]interface{}{
				"dir": map[string]interface{}{
					"type":        "string",
					"description": "Path to the local git repository.",
				},
				"staged": map[string]interface{}{
					"type":        "boolean",
					"description": "Whether to get staged diff.",
				},
			},
			Required: []string{"dir"},
		},
	}
	if err := registry.Register(diffSchema, handleGitDiff); err != nil {
		return err
	}

	commitSchema := mcp.Tool{
		Name:        "git_commit",
		Description: "Commit staged changes in a local git repository.",
		InputSchema: mcp.Schema{
			Type: "object",
			Properties: map[string]interface{}{
				"dir": map[string]interface{}{
					"type":        "string",
					"description": "Path to the local git repository.",
				},
				"message": map[string]interface{}{
					"type":        "string",
					"description": "Commit message.",
				},
			},
			Required: []string{"dir", "message"},
		},
	}
	if err := registry.Register(commitSchema, handleGitCommitTool); err != nil {
		return err
	}

	createPRSchema := mcp.Tool{
		Name:        "github_create_pr",
		Description: "Create a GitHub pull request.",
		InputSchema: mcp.Schema{
			Type: "object",
			Properties: map[string]interface{}{
				"owner": map[string]interface{}{
					"type": "string",
				},
				"repo": map[string]interface{}{
					"type": "string",
				},
				"title": map[string]interface{}{
					"type": "string",
				},
				"head": map[string]interface{}{
					"type": "string",
				},
				"base": map[string]interface{}{
					"type": "string",
				},
				"body": map[string]interface{}{
					"type": "string",
				},
			},
			Required: []string{"owner", "repo", "title", "head", "base"},
		},
	}
	if err := registry.Register(createPRSchema, handleGithubCreatePR); err != nil {
		return err
	}

	return nil
}

func handleGitDiff(ctx context.Context, args map[string]interface{}) (string, error) {
	dir, ok := args["dir"].(string)
	if !ok || dir == "" {
		return "", fmt.Errorf("missing or invalid 'dir' argument")
	}
	
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return "", fmt.Errorf("directory does not exist: %s", dir)
	}

	if _, err := os.Stat(filepath.Join(dir, ".git")); os.IsNotExist(err) {
		return "", fmt.Errorf("not a git repository: %s", dir)
	}

	if _, err := exec.LookPath("git"); err != nil {
		return "", fmt.Errorf("git executable not found in PATH")
	}

	staged := false
	if st, ok := args["staged"].(bool); ok {
		staged = st
	}

	diff, err := GitDiff(dir, staged)
	if err != nil {
		return "", err
	}

	return diff, nil
}

func handleGitCommitTool(ctx context.Context, args map[string]interface{}) (string, error) {
	dir, ok := args["dir"].(string)
	if !ok || dir == "" {
		return "", fmt.Errorf("missing or invalid 'dir' argument")
	}
	
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return "", fmt.Errorf("directory does not exist: %s", dir)
	}

	if _, err := os.Stat(filepath.Join(dir, ".git")); os.IsNotExist(err) {
		return "", fmt.Errorf("not a git repository: %s", dir)
	}

	if _, err := exec.LookPath("git"); err != nil {
		return "", fmt.Errorf("git executable not found in PATH")
	}

	message, ok := args["message"].(string)
	if !ok || message == "" {
		return "", fmt.Errorf("missing or invalid 'message' argument")
	}

	err := GitCommit(dir, message)
	if err != nil {
		return "", err
	}

	return "Committed successfully", nil
}

func handleGithubCreatePR(ctx context.Context, args map[string]interface{}) (string, error) {
	owner, ok := args["owner"].(string)
	if !ok || owner == "" {
		return "", fmt.Errorf("missing 'owner'")
	}
	repo, ok := args["repo"].(string)
	if !ok || repo == "" {
		return "", fmt.Errorf("missing 'repo'")
	}
	title, ok := args["title"].(string)
	if !ok || title == "" {
		return "", fmt.Errorf("missing 'title'")
	}
	head, ok := args["head"].(string)
	if !ok || head == "" {
		return "", fmt.Errorf("missing 'head'")
	}
	base, ok := args["base"].(string)
	if !ok || base == "" {
		return "", fmt.Errorf("missing 'base'")
	}
	body, _ := args["body"].(string)

	client := github.NewClient(nil)
	if token := os.Getenv("GITHUB_TOKEN"); token != "" {
		client = client.WithAuthToken(token)
	}

	newPR := &github.NewPullRequest{
		Title: github.String(title),
		Head:  github.String(head),
		Base:  github.String(base),
		Body:  github.String(body),
	}

	pr, _, err := client.PullRequests.Create(ctx, owner, repo, newPR)
	if err != nil {
		return "", fmt.Errorf("failed to create PR: %v", err)
	}

	return fmt.Sprintf("Created PR #%d: %s\nURL: %s", pr.GetNumber(), pr.GetTitle(), pr.GetHTMLURL()), nil
}

package git

import (
	"context"
	"fmt"
	"github.com/google/go-github/v62/github"
)

// GitHubClient is an interface to allow mocking of GitHub API calls.
type GitHubClient interface {
	GetPRDiff(ctx context.Context, owner, repo string, prNumber int) (string, error)
}

// DefaultGitHubClient is the concrete implementation wrapping go-github.
type DefaultGitHubClient struct {
	client *github.Client
}

// NewDefaultGitHubClient creates a new GitHub client using the provided go-github client.
func NewDefaultGitHubClient(client *github.Client) *DefaultGitHubClient {
	return &DefaultGitHubClient{client: client}
}

// GetPRDiff fetches the raw diff of a pull request.
func (c *DefaultGitHubClient) GetPRDiff(ctx context.Context, owner, repo string, prNumber int) (string, error) {
	opts := github.RawOptions{Type: github.Diff}
	diff, _, err := c.client.PullRequests.GetRaw(ctx, owner, repo, prNumber, opts)
	if err != nil {
		return "", fmt.Errorf("failed to get PR diff: %w", err)
	}
	return diff, nil
}

// GithubReviewPR takes a context, client, owner, repo, and PR number and returns the diff.
func GithubReviewPR(ctx context.Context, client GitHubClient, owner, repo string, prNumber int) (string, error) {
	return client.GetPRDiff(ctx, owner, repo, prNumber)
}

package git

import (
	"context"
	"testing"
)

type mockGitHubClient struct {
	diff string
	err  error
}

func (m *mockGitHubClient) GetPRDiff(ctx context.Context, owner, repo string, prNumber int) (string, error) {
	return m.diff, m.err
}

func TestGithubReviewPR(t *testing.T) {
	mock := &mockGitHubClient{diff: "test diff"}
	diff, err := GithubReviewPR(context.Background(), mock, "owner", "repo", 1)
	if err != nil {
		t.Fatalf("GithubReviewPR failed: %v", err)
	}

	if diff != "test diff" {
		t.Errorf("Expected diff 'test diff', got %s", diff)
	}
}

func TestGithubReviewPR_Error(t *testing.T) {
	mock := &mockGitHubClient{err: context.DeadlineExceeded}
	_, err := GithubReviewPR(context.Background(), mock, "owner", "repo", 1)
	if err == nil {
		t.Errorf("Expected GithubReviewPR to fail when client returns error")
	}
}

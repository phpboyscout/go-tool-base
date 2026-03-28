//go:build integration

package github

import (
	"context"
	"strings"
	"testing"

	"github.com/google/go-github/v80/github"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/phpboyscout/go-tool-base/pkg/config"
	"github.com/phpboyscout/go-tool-base/pkg/logger"
)

func ensureIntegrationPrerequisites(t *testing.T, client GitHubClient, owner, repo, branchBase, branchHead string) *github.PullRequest {
	t.Helper()

	ctx := context.Background()
	gh := client.GetClient()

	// 1. Ensure Head Branch Exists
	_, _, err := gh.Git.GetRef(ctx, owner, repo, "refs/heads/"+branchHead)
	if err != nil {
		// Assume 404
		// Get Base SHA
		ref, _, err := gh.Git.GetRef(ctx, owner, repo, "refs/heads/"+branchBase)
		require.NoError(t, err, "failed to get base branch ref")

		// Create Head Branch
		newRef := github.CreateRef{
			Ref: "refs/heads/" + branchHead,
			SHA: ref.Object.GetSHA(),
		}
		_, _, err = gh.Git.CreateRef(ctx, owner, repo, newRef)
		require.NoError(t, err, "failed to create integration test branch")
	}

	// 2. Ensure PR exists
	pr, err := client.GetPullRequestByBranch(ctx, owner, repo, branchHead, "open")
	if err != nil {
		if strings.Contains(err.Error(), "no pull request found") {
			newPR := &github.NewPullRequest{
				Title:               new("Integration Test PR"),
				Head:                new(branchHead),
				Base:                new(branchBase),
				Body:                new("This is an integration test PR created by test suite"),
				MaintainerCanModify: new(true),
			}
			pr, err = client.CreatePullRequest(ctx, owner, repo, newPR)
			require.NoError(t, err, "failed to create pull request")
		} else {
			require.NoError(t, err, "failed to get pull request")
		}
	}

	return pr
}

func TestGithubFindPullRequestByBranch(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "test-token")

	cfg := config.NewReaderContainer(logger.NewNoop(), "yaml", strings.NewReader(integrationConfigGithub))
	client, err := NewGitHubClient(cfg.Sub("github"))
	require.NoError(t, err)

	pr := ensureIntegrationPrerequisites(t, client, GitHubOrg, GitHubRepo, "main", "integration-test-branch")
	assert.NotNil(t, pr)
	assert.Equal(t, "integration-test-branch", pr.GetHead().GetRef())
}

func TestAddLabelsToPullRequest(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "test-token")

	cfg := config.NewReaderContainer(logger.NewNoop(), "yaml", strings.NewReader(integrationConfigGithub))
	client, err := NewGitHubClient(cfg.Sub("github"))
	require.NoError(t, err)

	pr := ensureIntegrationPrerequisites(t, client, GitHubOrg, GitHubRepo, "main", "integration-test-branch")
	require.NotNil(t, pr)

	err = client.AddLabelsToPullRequest(context.Background(), GitHubOrg, GitHubRepo, pr.GetNumber(), []string{"test", "do-not-merge", "release-train"})
	assert.NoError(t, err)
}

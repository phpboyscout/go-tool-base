package github

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/phpboyscout/gtb/pkg/logger"
	"github.com/google/go-github/v80/github"
	"github.com/phpboyscout/gtb/pkg/config"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var apiConfigGithub = `
url:
  api: %s
  upload: %s
auth:
  env: GITHUB_TOKEN
  value: mock-token
`

// setupMockGitHubServer creates a mock HTTP server and returns a client configured to use it.
func setupMockGitHubServer(t *testing.T, handler http.HandlerFunc) (*httptest.Server, *GHClient) {
	server := httptest.NewServer(handler)

	cfg := fmt.Sprintf(apiConfigGithub, server.URL, server.URL)

	// Configure container with mock server URL
	containable := config.NewReaderContainer(logger.NewNoop(), "yaml", strings.NewReader(cfg))

	client, err := NewGitHubClient(containable)
	require.NoError(t, err)

	return server, client
}

func TestCreatePullRequest(t *testing.T) {
	handler := func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/v3/repos/owner/repo/pulls", r.URL.Path)
		assert.Equal(t, "POST", r.Method)

		var body github.NewPullRequest
		_ = json.NewDecoder(r.Body).Decode(&body)
		assert.Equal(t, "Title", *body.Title)

		resp := github.PullRequest{
			Number: new(123),
			Title:  body.Title,
		}
		_ = json.NewEncoder(w).Encode(resp)
	}

	server, client := setupMockGitHubServer(t, handler)
	defer server.Close()

	pr := &github.NewPullRequest{Title: new("Title")}
	result, err := client.CreatePullRequest(context.Background(), "owner", "repo", pr)

	require.NoError(t, err)
	assert.Equal(t, 123, *result.Number)
}

func TestGetPullRequestByBranch(t *testing.T) {
	t.Run("Found", func(t *testing.T) {
		handler := func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "/api/v3/repos/owner/repo/pulls", r.URL.Path)
			assert.Equal(t, "GET", r.Method)
			// Verify filtering params
			assert.Equal(t, "open", r.URL.Query().Get("state"))
			assert.Equal(t, "owner:feature-branch", r.URL.Query().Get("head"))

			resp := []*github.PullRequest{
				{
					Number: new(456),
					Head: &github.PullRequestBranch{
						Ref: new("feature-branch"),
					},
				},
			}
			_ = json.NewEncoder(w).Encode(resp)
		}

		server, client := setupMockGitHubServer(t, handler)
		defer server.Close()

		pr, err := client.GetPullRequestByBranch(context.Background(), "owner", "repo", "feature-branch", "open")
		require.NoError(t, err)
		assert.Equal(t, 456, *pr.Number)
	})

	t.Run("NotFound", func(t *testing.T) {
		handler := func(w http.ResponseWriter, r *http.Request) {
			_ = json.NewEncoder(w).Encode([]*github.PullRequest{})
		}

		server, client := setupMockGitHubServer(t, handler)
		defer server.Close()

		_, err := client.GetPullRequestByBranch(context.Background(), "owner", "repo", "missing-branch", "open")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "no pull request found")
	})
}

func TestUpdatePullRequest(t *testing.T) {
	handler := func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/v3/repos/owner/repo/pulls/123", r.URL.Path)
		assert.Equal(t, "PATCH", r.Method)

		var body github.PullRequest
		_ = json.NewDecoder(r.Body).Decode(&body)
		assert.Equal(t, "Updated Title", *body.Title)

		resp := github.PullRequest{
			Number: new(123),
			Title:  body.Title,
		}
		_ = json.NewEncoder(w).Encode(resp)
	}

	server, client := setupMockGitHubServer(t, handler)
	defer server.Close()

	update := &github.PullRequest{Title: new("Updated Title")}
	result, _, err := client.UpdatePullRequest(context.Background(), "owner", "repo", 123, update)

	require.NoError(t, err)
	assert.Equal(t, "Updated Title", *result.Title)
}

func TestAddLabelsToPullRequest_Unit(t *testing.T) {
	handler := func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/v3/repos/owner/repo/issues/123/labels", r.URL.Path)
		assert.Equal(t, "POST", r.Method)

		var labels []string
		_ = json.NewDecoder(r.Body).Decode(&labels)
		assert.Contains(t, labels, "bug")

		_ = json.NewEncoder(w).Encode([]*github.Label{{Name: new("bug")}})
	}

	server, client := setupMockGitHubServer(t, handler)
	defer server.Close()

	err := client.AddLabelsToPullRequest(context.Background(), "owner", "repo", 123, []string{"bug"})
	require.NoError(t, err)
}

func TestCreateRepo(t *testing.T) {
	handler := func(w http.ResponseWriter, r *http.Request) {
		// assert.Equal(t, "/api/v3/user/repos", r.URL.Path)
		// Note from previous output check logic:
		if strings.HasPrefix(r.URL.Path, "/api/v3/orgs") {
			assert.Equal(t, "/api/v3/orgs/owner/repos", r.URL.Path)
		} else {
			assert.Equal(t, "/api/v3/user/repos", r.URL.Path)
		}

		var body github.Repository
		_ = json.NewDecoder(r.Body).Decode(&body)
		assert.Equal(t, "new-repo", *body.Name)
		assert.Equal(t, "internal", *body.Visibility)

		_ = json.NewEncoder(w).Encode(&github.Repository{
			Name: new("new-repo"),
		})
	}

	server, client := setupMockGitHubServer(t, handler)
	defer server.Close()

	// Testing with "owner" usually implies org in go-github if passed as first arg to Create
	repo, err := client.CreateRepo(context.Background(), "owner", "new-repo")
	require.NoError(t, err)
	assert.Equal(t, "new-repo", *repo.Name)
}

func TestUploadKey(t *testing.T) {
	handler := func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/v3/user/keys", r.URL.Path)
		assert.Equal(t, "POST", r.Method)

		var body github.Key
		_ = json.NewDecoder(r.Body).Decode(&body)
		assert.Equal(t, "my-key", *body.Title)
		assert.Equal(t, "ssh-rsa AAA...", *body.Key)

		_ = json.NewEncoder(w).Encode(&github.Key{
			ID: new(int64(1)),
		})
	}

	server, client := setupMockGitHubServer(t, handler)
	defer server.Close()

	err := client.UploadKey(context.Background(), "my-key", []byte("ssh-rsa AAA..."))
	require.NoError(t, err)
}

func TestListReleases(t *testing.T) {
	handler := func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/v3/repos/owner/repo/releases", r.URL.Path)
		assert.Equal(t, "GET", r.Method)

		_ = json.NewEncoder(w).Encode([]*github.RepositoryRelease{
			{TagName: new("v1.0.0")},
			{TagName: new("v1.1.0")},
		})
	}

	server, client := setupMockGitHubServer(t, handler)
	defer server.Close()

	releases, err := client.ListReleases(context.Background(), "owner", "repo")
	require.NoError(t, err)
	assert.Len(t, releases, 2)
	assert.Contains(t, releases, "v1.0.0")
	assert.Contains(t, releases, "v1.1.0")
}

func TestDownloadAsset(t *testing.T) {
	assetID := int64(12345)

	t.Run("DownloadStream", func(t *testing.T) {
		handler := func(w http.ResponseWriter, r *http.Request) {
			path := fmt.Sprintf("/api/v3/repos/owner/repo/releases/assets/%d", assetID)
			assert.Equal(t, path, r.URL.Path)
			assert.Equal(t, "application/octet-stream", r.Header.Get("Accept"))

			w.Write([]byte("asset-content"))
		}

		server, client := setupMockGitHubServer(t, handler)
		defer server.Close()

		rc, err := client.DownloadAsset(context.Background(), "owner", "repo", assetID)
		require.NoError(t, err)
		defer rc.Close()

		content, err := io.ReadAll(rc)
		require.NoError(t, err)
		assert.Equal(t, "asset-content", string(content))
	})

	t.Run("DownloadToFS", func(t *testing.T) {
		handler := func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte("file-content"))
		}

		server, client := setupMockGitHubServer(t, handler)
		defer server.Close()

		fs := afero.NewMemMapFs()
		err := client.DownloadAssetTo(context.Background(), fs, "owner", "repo", assetID, "/tmp/asset")
		require.NoError(t, err)

		exists, _ := afero.Exists(fs, "/tmp/asset")
		assert.True(t, exists)

		content, _ := afero.ReadFile(fs, "/tmp/asset")
		assert.Equal(t, "file-content", string(content))
	})
}

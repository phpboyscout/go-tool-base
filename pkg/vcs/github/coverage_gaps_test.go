package github

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/go-github/v80/github"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/pkg/config"
	"gitlab.com/phpboyscout/go-tool-base/pkg/vcs/release"
)

// newErrorServer returns a mock GitHub server that always responds with the
// given status code and message, plus a client pointed at it.
func newErrorServer(t *testing.T, status int, message string) (*httptest.Server, *GHClient) {
	t.Helper()

	return setupMockGitHubServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(map[string]string{"message": message})
	})
}

// --- release.Provider error/branch coverage ---

// TestReleaseProvider_DownloadReleaseAsset_Error exercises the error-wrapping
// branch in DownloadReleaseAsset when the GitHub API rejects the request.
func TestReleaseProvider_DownloadReleaseAsset_Error(t *testing.T) {
	t.Parallel()

	assetID := int64(404)

	server, client := setupMockGitHubServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]string{"message": "Not Found"})
	})
	defer server.Close()

	provider := NewReleaseProvider(client)
	asset := &githubAsset{asset: &github.ReleaseAsset{ID: &assetID}}

	rc, redirect, err := provider.DownloadReleaseAsset(context.Background(), "owner", "repo", asset)
	require.Error(t, err)
	assert.Nil(t, rc)
	assert.Empty(t, redirect)
}

// TestReleaseProvider_DownloadReleaseAsset_CDNRedirect exercises the path where
// the GitHub API responds with a 302 to a CDN URL. go-github follows the
// redirect; the helper must return the streamed bytes without error.
func TestReleaseProvider_DownloadReleaseAsset_CDNRedirect(t *testing.T) {
	t.Parallel()

	assetID := int64(7)

	// A dedicated CDN server that serves the actual asset bytes.
	cdn := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write([]byte("cdn-bytes"))
	}))
	defer cdn.Close()

	// The API server issues a redirect to the CDN for the asset download.
	server, client := setupMockGitHubServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Accept") == "application/octet-stream" {
			http.Redirect(w, r, cdn.URL, http.StatusFound)

			return
		}

		w.WriteHeader(http.StatusOK)
	})
	defer server.Close()

	provider := NewReleaseProvider(client)
	asset := &githubAsset{asset: &github.ReleaseAsset{ID: &assetID}}

	rc, _, err := provider.DownloadReleaseAsset(context.Background(), "owner", "repo", asset)
	require.NoError(t, err)
	defer func() { _ = rc.Close() }()

	data, err := io.ReadAll(rc)
	require.NoError(t, err)
	assert.Equal(t, "cdn-bytes", string(data))
}

// --- GHClient error-branch coverage ---

func TestCreatePullRequest_Error(t *testing.T) {
	t.Parallel()

	server, client := newErrorServer(t, http.StatusUnprocessableEntity, "validation failed")
	defer server.Close()

	pr := &github.NewPullRequest{Title: github.Ptr("Title")}
	result, err := client.CreatePullRequest(context.Background(), "owner", "repo", pr)
	require.Error(t, err)
	assert.Nil(t, result)
}

func TestAddLabelsToPullRequest_Error(t *testing.T) {
	t.Parallel()

	server, client := newErrorServer(t, http.StatusNotFound, "Not Found")
	defer server.Close()

	err := client.AddLabelsToPullRequest(context.Background(), "owner", "repo", 1, []string{"bug"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to add labels")
}

func TestCreateRepo_Error(t *testing.T) {
	t.Parallel()

	server, client := newErrorServer(t, http.StatusForbidden, "forbidden")
	defer server.Close()

	repo, err := client.CreateRepo(context.Background(), "owner", "new-repo")
	require.Error(t, err)
	assert.Nil(t, repo)
	assert.Contains(t, err.Error(), "failed to create repository")
}

func TestUploadKey_Error(t *testing.T) {
	t.Parallel()

	server, client := newErrorServer(t, http.StatusUnprocessableEntity, "key is invalid")
	defer server.Close()

	err := client.UploadKey(context.Background(), "my-key", []byte("bad-key"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to upload SSH key")
}

func TestUpdatePullRequest_Error(t *testing.T) {
	t.Parallel()

	server, client := newErrorServer(t, http.StatusNotFound, "Not Found")
	defer server.Close()

	update := &github.PullRequest{Title: github.Ptr("Updated")}
	result, _, err := client.UpdatePullRequest(context.Background(), "owner", "repo", 999, update)
	require.Error(t, err)
	assert.Nil(t, result)
}

func TestGetPullRequestByBranch_Error(t *testing.T) {
	t.Parallel()

	server, client := newErrorServer(t, http.StatusInternalServerError, "boom")
	defer server.Close()

	pr, err := client.GetPullRequestByBranch(context.Background(), "owner", "repo", "branch", "open")
	require.Error(t, err)
	assert.Nil(t, pr)
	assert.Contains(t, err.Error(), "failed to list pull requests")
}

// TestGetFileContents_NilContent exercises the nil-content branch: a 200 OK
// with a JSON null body yields a nil *RepositoryContent.
func TestGetFileContents_NilContent(t *testing.T) {
	t.Parallel()

	server, client := setupMockGitHubServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("null"))
	})
	defer server.Close()

	content, err := client.GetFileContents(context.Background(), "owner", "repo", "README.md", "main")
	require.Error(t, err)
	assert.Empty(t, content)
	assert.Contains(t, err.Error(), "nil content")
}

// TestListReleases_Pagination exercises the multi-page branch of ListReleases:
// the first page advertises a next page via the Link header, and the loop must
// follow it and concatenate the tags from both pages.
func TestListReleases_Pagination(t *testing.T) {
	t.Parallel()

	var serverURL string

	server, client := setupMockGitHubServer(t, func(w http.ResponseWriter, r *http.Request) {
		page := r.URL.Query().Get("page")
		if page == "" || page == "1" {
			// Advertise a second page; go-github parses the Link header.
			w.Header().Set("Link", `<`+serverURL+`/api/v3/repos/owner/repo/releases?page=2>; rel="next"`)
			_ = json.NewEncoder(w).Encode([]*github.RepositoryRelease{{TagName: github.Ptr("v1.0.0")}})

			return
		}

		_ = json.NewEncoder(w).Encode([]*github.RepositoryRelease{{TagName: github.Ptr("v2.0.0")}})
	})
	defer server.Close()

	serverURL = server.URL

	tags, err := client.ListReleases(context.Background(), "owner", "repo")
	require.NoError(t, err)
	assert.Equal(t, []string{"v1.0.0", "v2.0.0"}, tags)
}

// TestDownloadAssetTo_CreateError exercises the fs.Create error branch: a
// read-only filesystem refuses to create the destination file after a
// successful download.
func TestDownloadAssetTo_CreateError(t *testing.T) {
	t.Parallel()

	server, client := setupMockGitHubServer(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("payload"))
	})
	defer server.Close()

	roFS := afero.NewReadOnlyFs(afero.NewMemMapFs())

	err := client.DownloadAssetTo(context.Background(), roFS, "owner", "repo", 1, "/out")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to create file")
}

// --- NewGitHubClient enterprise-URL error branch ---

// TestNewGitHubClient_InvalidEnterpriseURL exercises the WithEnterpriseURLs
// error path: a host-derived API URL that go-github cannot parse must surface
// an error rather than a usable client.
func TestNewGitHubClient_InvalidEnterpriseURL(t *testing.T) {
	t.Parallel()

	// A host containing a control character produces an unparseable
	// "https://<host>/api/v3/" URL inside WithEnterpriseURLs.
	src := release.ReleaseSourceConfig{Host: "ghe.example.com\x7f"}

	client, err := NewGitHubClient(src, nil)
	require.Error(t, err)
	assert.Nil(t, client)
}

// --- login flow error branches ---

// TestNewLoginFlow_InvalidHostname exercises the NewGitHubHost error branch in
// newLoginFlow when the constructed URL cannot be parsed.
func TestNewLoginFlow_InvalidHostname(t *testing.T) {
	t.Parallel()

	flow, err := newLoginFlow("bad\x7fhost")
	require.Error(t, err)
	assert.Nil(t, flow)
}

// TestGHLogin_InvalidHostname exercises the early-return error branch in
// GHLogin when newLoginFlow fails (no network required).
func TestGHLogin_InvalidHostname(t *testing.T) {
	t.Parallel()

	token, err := GHLogin("bad\x7fhost")
	require.Error(t, err)
	assert.Empty(t, token)
}

// --- init.go registered factory closure ---

// TestRegisteredGitHubFactory drives the provider factory registered in
// init.go via the release registry, covering the closure body (success path).
func TestRegisteredGitHubFactory(t *testing.T) {
	t.Parallel()

	factory, err := release.Lookup(release.SourceTypeGitHub)
	require.NoError(t, err)
	require.NotNil(t, factory)

	// An empty config subtree is valid: a public repo needs no github section.
	cfg := config.NewReaderContainer(afero.NewMemMapFs(), config.WithConfigFormat("yaml"))

	provider, err := factory(release.ReleaseSourceConfig{Host: "github.com"}, cfg)
	require.NoError(t, err)
	assert.NotNil(t, provider)
}

// TestRegisteredGitHubFactory_Error covers the error branch of the registered
// factory closure: an unparseable Enterprise host makes NewGitHubClient fail.
func TestRegisteredGitHubFactory_Error(t *testing.T) {
	t.Parallel()

	factory, err := release.Lookup(release.SourceTypeGitHub)
	require.NoError(t, err)

	cfg := config.NewReaderContainer(afero.NewMemMapFs(), config.WithConfigFormat("yaml"))

	provider, err := factory(release.ReleaseSourceConfig{Host: "ghe.example.com\x7f"}, cfg)
	require.Error(t, err)
	assert.Nil(t, provider)
}

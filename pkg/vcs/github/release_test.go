package github

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"testing"

	"github.com/google/go-github/v80/github"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- githubRelease accessor tests ---

func TestGithubRelease_Accessors(t *testing.T) {
	t.Parallel()

	name := "v1.0.0"
	tag := "v1.0.0"
	body := "Release notes"
	draft := true

	rel := &githubRelease{
		release: &github.RepositoryRelease{
			Name:    &name,
			TagName: &tag,
			Body:    &body,
			Draft:   &draft,
		},
	}

	assert.Equal(t, "v1.0.0", rel.GetName())
	assert.Equal(t, "v1.0.0", rel.GetTagName())
	assert.Equal(t, "Release notes", rel.GetBody())
	assert.True(t, rel.GetDraft())
}

func TestGithubRelease_NilFields(t *testing.T) {
	t.Parallel()

	rel := &githubRelease{release: &github.RepositoryRelease{}}
	assert.Empty(t, rel.GetName())
	assert.Empty(t, rel.GetTagName())
	assert.Empty(t, rel.GetBody())
	assert.False(t, rel.GetDraft())
	assert.Empty(t, rel.GetAssets())
}

func TestGithubRelease_GetAssets_WithAssets(t *testing.T) {
	t.Parallel()

	id := int64(1)
	assetName := "binary.tar.gz"
	url := "https://example.com/binary.tar.gz"

	rel := &githubRelease{
		release: &github.RepositoryRelease{
			Assets: []*github.ReleaseAsset{
				{
					ID:                 &id,
					Name:               &assetName,
					BrowserDownloadURL: &url,
				},
			},
		},
	}

	assets := rel.GetAssets()
	require.Len(t, assets, 1)
	assert.Equal(t, int64(1), assets[0].GetID())
	assert.Equal(t, "binary.tar.gz", assets[0].GetName())
	assert.Equal(t, "https://example.com/binary.tar.gz", assets[0].GetBrowserDownloadURL())
}

// --- githubAsset accessor tests ---

func TestGithubAsset_Accessors(t *testing.T) {
	t.Parallel()

	id := int64(42)
	name := "artifact.zip"
	url := "https://example.com/artifact.zip"

	asset := &githubAsset{
		asset: &github.ReleaseAsset{
			ID:                 &id,
			Name:               &name,
			BrowserDownloadURL: &url,
		},
	}

	assert.Equal(t, int64(42), asset.GetID())
	assert.Equal(t, "artifact.zip", asset.GetName())
	assert.Equal(t, "https://example.com/artifact.zip", asset.GetBrowserDownloadURL())
}

func TestGithubAsset_NilFields(t *testing.T) {
	t.Parallel()

	asset := &githubAsset{asset: &github.ReleaseAsset{}}
	assert.Equal(t, int64(0), asset.GetID())
	assert.Empty(t, asset.GetName())
	assert.Empty(t, asset.GetBrowserDownloadURL())
}

// --- GitHubReleaseProvider tests ---

func TestNewReleaseProvider(t *testing.T) {
	t.Parallel()

	server, client := setupMockGitHubServer(t, func(w http.ResponseWriter, _ *http.Request) {})
	defer server.Close()

	provider := NewReleaseProvider(client)
	assert.NotNil(t, provider)
}

func TestReleaseProvider_GetLatestRelease(t *testing.T) {
	name := "v1.0.0"
	tag := "v1.0.0"

	server, client := setupMockGitHubServer(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/v3/repos/owner/repo/releases/latest", r.URL.Path)
		_ = json.NewEncoder(w).Encode(&github.RepositoryRelease{Name: &name, TagName: &tag})
	})
	defer server.Close()

	provider := NewReleaseProvider(client)
	rel, err := provider.GetLatestRelease(context.Background(), "owner", "repo")
	require.NoError(t, err)
	assert.Equal(t, "v1.0.0", rel.GetTagName())
}

func TestReleaseProvider_GetLatestRelease_Error(t *testing.T) {
	server, client := setupMockGitHubServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]string{"message": "Not Found"})
	})
	defer server.Close()

	provider := NewReleaseProvider(client)
	rel, err := provider.GetLatestRelease(context.Background(), "owner", "repo")
	require.Error(t, err)
	assert.Nil(t, rel)
}

func TestReleaseProvider_GetReleaseByTag(t *testing.T) {
	name := "v2.0.0"
	tag := "v2.0.0"

	server, client := setupMockGitHubServer(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/v3/repos/owner/repo/releases/tags/v2.0.0", r.URL.Path)
		_ = json.NewEncoder(w).Encode(&github.RepositoryRelease{Name: &name, TagName: &tag})
	})
	defer server.Close()

	provider := NewReleaseProvider(client)
	rel, err := provider.GetReleaseByTag(context.Background(), "owner", "repo", "v2.0.0")
	require.NoError(t, err)
	assert.Equal(t, "v2.0.0", rel.GetTagName())
}

func TestReleaseProvider_GetReleaseByTag_Error(t *testing.T) {
	server, client := setupMockGitHubServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]string{"message": "Not Found"})
	})
	defer server.Close()

	provider := NewReleaseProvider(client)
	rel, err := provider.GetReleaseByTag(context.Background(), "owner", "repo", "v99.0.0")
	require.Error(t, err)
	assert.Nil(t, rel)
}

func TestReleaseProvider_ListReleases(t *testing.T) {
	v1, v2 := "v1.0.0", "v2.0.0"

	server, client := setupMockGitHubServer(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/v3/repos/owner/repo/releases", r.URL.Path)
		_ = json.NewEncoder(w).Encode([]*github.RepositoryRelease{
			{TagName: &v2},
			{TagName: &v1},
		})
	})
	defer server.Close()

	provider := NewReleaseProvider(client)
	releases, err := provider.ListReleases(context.Background(), "owner", "repo", 10)
	require.NoError(t, err)
	require.Len(t, releases, 2)
	assert.Equal(t, "v2.0.0", releases[0].GetTagName())
}

func TestReleaseProvider_ListReleases_Error(t *testing.T) {
	server, client := setupMockGitHubServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"message": "internal error"})
	})
	defer server.Close()

	provider := NewReleaseProvider(client)
	releases, err := provider.ListReleases(context.Background(), "owner", "repo", 10)
	require.Error(t, err)
	assert.Nil(t, releases)
}

func TestReleaseProvider_DownloadReleaseAsset(t *testing.T) {
	assetID := int64(99)

	server, client := setupMockGitHubServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write([]byte("asset-data"))
	})
	defer server.Close()

	provider := NewReleaseProvider(client)
	asset := &githubAsset{asset: &github.ReleaseAsset{ID: &assetID}}

	rc, _, err := provider.DownloadReleaseAsset(context.Background(), "owner", "repo", asset)
	require.NoError(t, err)
	defer func() { _ = rc.Close() }()

	data, err := io.ReadAll(rc)
	require.NoError(t, err)
	assert.Equal(t, "asset-data", string(data))
}

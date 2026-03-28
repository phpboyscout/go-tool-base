package gitlab

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	gitlab "gitlab.com/gitlab-org/api/client-go"

	mockConfig "github.com/phpboyscout/go-tool-base/mocks/pkg/config"
)

func TestGitlabRelease_Accessors(t *testing.T) {
	t.Parallel()

	rel := &gitlabRelease{
		release: &gitlab.Release{
			Name:        "v1.0.0",
			TagName:     "v1.0.0",
			Description: "First release",
		},
	}

	assert.Equal(t, "v1.0.0", rel.GetName())
	assert.Equal(t, "v1.0.0", rel.GetTagName())
	assert.Equal(t, "First release", rel.GetBody())
	assert.False(t, rel.GetDraft())
}

func TestGitlabRelease_GetAssets_Empty(t *testing.T) {
	t.Parallel()

	rel := &gitlabRelease{
		release: &gitlab.Release{
			Assets: gitlab.ReleaseAssets{},
		},
	}
	assert.Nil(t, rel.GetAssets())
}

func TestGitlabRelease_GetAssets_WithLinks(t *testing.T) {
	t.Parallel()

	rel := &gitlabRelease{
		release: &gitlab.Release{
			Assets: gitlab.ReleaseAssets{
				Links: []*gitlab.ReleaseLink{
					{ID: 1, Name: "binary.tar.gz", URL: "https://example.com/binary.tar.gz"},
					{ID: 2, Name: "checksum.txt", URL: "https://example.com/checksum.txt"},
				},
			},
		},
	}

	assets := rel.GetAssets()
	require.Len(t, assets, 2)
	assert.Equal(t, int64(1), assets[0].GetID())
	assert.Equal(t, "binary.tar.gz", assets[0].GetName())
	assert.Equal(t, "https://example.com/binary.tar.gz", assets[0].GetBrowserDownloadURL())
	assert.Equal(t, int64(2), assets[1].GetID())
}

func TestGitlabAsset_Accessors(t *testing.T) {
	t.Parallel()

	asset := &gitlabAsset{
		link: &gitlab.ReleaseLink{
			ID:   42,
			Name: "artifact.zip",
			URL:  "https://gitlab.com/artifact.zip",
		},
	}

	assert.Equal(t, int64(42), asset.GetID())
	assert.Equal(t, "artifact.zip", asset.GetName())
	assert.Equal(t, "https://gitlab.com/artifact.zip", asset.GetBrowserDownloadURL())
}

func TestNewReleaseProvider_NilConfig(t *testing.T) {
	t.Parallel()

	provider, err := NewReleaseProvider(nil)
	require.Error(t, err)
	assert.Nil(t, provider)
	assert.Contains(t, err.Error(), "gitlab configuration is missing")
}

func TestNewReleaseProvider_DefaultBaseURL(t *testing.T) {
	t.Parallel()

	cfg := mockConfig.NewMockContainable(t)
	cfg.EXPECT().Has("auth.env").Return(false)
	cfg.EXPECT().Has("auth.value").Return(false)
	cfg.EXPECT().GetString("url.api").Return("")

	provider, err := NewReleaseProvider(cfg)
	require.NoError(t, err)
	assert.NotNil(t, provider)
}

func TestNewReleaseProvider_WithToken(t *testing.T) {
	t.Parallel()

	cfg := mockConfig.NewMockContainable(t)
	cfg.EXPECT().Has("auth.env").Return(false)
	cfg.EXPECT().Has("auth.value").Return(true)
	cfg.EXPECT().GetString("auth.value").Return("test-token")
	cfg.EXPECT().GetString("url.api").Return("https://custom.gitlab.com/api/v4")

	provider, err := NewReleaseProvider(cfg)
	require.NoError(t, err)
	assert.NotNil(t, provider)
}

func TestDownloadReleaseAsset_Success(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "test-token", r.Header.Get("PRIVATE-TOKEN"))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("binary-content"))
	}))
	defer server.Close()

	provider := &GitLabReleaseProvider{token: "test-token"}
	asset := &gitlabAsset{
		link: &gitlab.ReleaseLink{
			Name: "artifact.zip",
			URL:  server.URL + "/artifact.zip",
		},
	}

	body, _, err := provider.DownloadReleaseAsset(context.Background(), "owner", "repo", asset)
	require.NoError(t, err)
	defer func() { _ = body.Close() }()

	data, err := io.ReadAll(body)
	require.NoError(t, err)
	assert.Equal(t, "binary-content", string(data))
}

func TestDownloadReleaseAsset_EmptyURL(t *testing.T) {
	t.Parallel()

	provider := &GitLabReleaseProvider{}
	asset := &gitlabAsset{
		link: &gitlab.ReleaseLink{URL: ""},
	}

	body, _, err := provider.DownloadReleaseAsset(context.Background(), "owner", "repo", asset)
	require.Error(t, err)
	assert.Nil(t, body)
	assert.Contains(t, err.Error(), "asset URL is empty")
}

func TestDownloadReleaseAsset_ServerError(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	provider := &GitLabReleaseProvider{}
	asset := &gitlabAsset{
		link: &gitlab.ReleaseLink{URL: server.URL + "/artifact.zip"},
	}

	body, _, err := provider.DownloadReleaseAsset(context.Background(), "owner", "repo", asset)
	require.Error(t, err)
	assert.Nil(t, body)
	assert.Contains(t, err.Error(), "status 500")
}

func newTestProvider(t *testing.T, server *httptest.Server) *GitLabReleaseProvider {
	t.Helper()

	client, err := gitlab.NewClient("", gitlab.WithBaseURL(server.URL+"/api/v4"))
	require.NoError(t, err)

	return &GitLabReleaseProvider{client: client}
}

func TestGetLatestRelease_Success(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"name":"v1.0.0","tag_name":"v1.0.0","description":"First release","assets":{"links":[]}}]`))
	}))
	defer server.Close()

	provider := newTestProvider(t, server)
	rel, err := provider.GetLatestRelease(context.Background(), "owner", "repo")
	require.NoError(t, err)
	assert.Equal(t, "v1.0.0", rel.GetTagName())
	assert.Equal(t, "First release", rel.GetBody())
}

func TestGetLatestRelease_Empty(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[]`))
	}))
	defer server.Close()

	provider := newTestProvider(t, server)
	rel, err := provider.GetLatestRelease(context.Background(), "owner", "repo")
	require.Error(t, err)
	assert.Nil(t, rel)
	assert.Contains(t, err.Error(), "no releases found")
}

func TestGetLatestRelease_ServerError(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"message":"internal error"}`))
	}))
	defer server.Close()

	provider := newTestProvider(t, server)
	rel, err := provider.GetLatestRelease(context.Background(), "owner", "repo")
	require.Error(t, err)
	assert.Nil(t, rel)
}

func TestGetReleaseByTag_Success(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"name":"v2.0.0","tag_name":"v2.0.0","description":"Second release","assets":{"links":[]}}`))
	}))
	defer server.Close()

	provider := newTestProvider(t, server)
	rel, err := provider.GetReleaseByTag(context.Background(), "owner", "repo", "v2.0.0")
	require.NoError(t, err)
	assert.Equal(t, "v2.0.0", rel.GetTagName())
}

func TestGetReleaseByTag_NotFound(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"message":"404 Tag Not Found"}`))
	}))
	defer server.Close()

	provider := newTestProvider(t, server)
	rel, err := provider.GetReleaseByTag(context.Background(), "owner", "repo", "v99.0.0")
	require.Error(t, err)
	assert.Nil(t, rel)
}

func TestListReleases_Success(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"name":"v1.0.0","tag_name":"v1.0.0","description":"","assets":{"links":[]}},{"name":"v0.9.0","tag_name":"v0.9.0","description":"","assets":{"links":[]}}]`))
	}))
	defer server.Close()

	provider := newTestProvider(t, server)
	releases, err := provider.ListReleases(context.Background(), "owner", "repo", 10)
	require.NoError(t, err)
	require.Len(t, releases, 2)
	assert.Equal(t, "v1.0.0", releases[0].GetTagName())
	assert.Equal(t, "v0.9.0", releases[1].GetTagName())
}

func TestListReleases_ServerError(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"message":"internal error"}`))
	}))
	defer server.Close()

	provider := newTestProvider(t, server)
	releases, err := provider.ListReleases(context.Background(), "owner", "repo", 10)
	require.Error(t, err)
	assert.Nil(t, releases)
}

func TestDownloadReleaseAsset_NoToken(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Empty(t, r.Header.Get("PRIVATE-TOKEN"))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("public-content"))
	}))
	defer server.Close()

	provider := &GitLabReleaseProvider{token: ""}
	asset := &gitlabAsset{
		link: &gitlab.ReleaseLink{URL: server.URL + "/artifact.zip"},
	}

	body, _, err := provider.DownloadReleaseAsset(context.Background(), "owner", "repo", asset)
	require.NoError(t, err)
	defer func() { _ = body.Close() }()

	data, err := io.ReadAll(body)
	require.NoError(t, err)
	assert.Equal(t, "public-content", string(data))
}

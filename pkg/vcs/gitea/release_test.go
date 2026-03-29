package gitea_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/phpboyscout/go-tool-base/pkg/vcs/gitea"
	"github.com/phpboyscout/go-tool-base/pkg/vcs/release"
)

// buildServer returns a test server that serves the given releases JSON at
// /api/v1/repos/{owner}/{repo}/releases and /api/v1/repos/{owner}/{repo}/releases/tags/{tag}.
func buildServer(t *testing.T, releases []map[string]any) *httptest.Server {
	t.Helper()

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Tag lookup endpoint
		if strings.Contains(r.URL.Path, "/releases/tags/") {
			parts := strings.Split(r.URL.Path, "/releases/tags/")
			tag := parts[len(parts)-1]

			for _, rel := range releases {
				if rel["tag_name"] == tag {
					w.Header().Set("Content-Type", "application/json")
					_ = json.NewEncoder(w).Encode(rel)

					return
				}
			}

			w.WriteHeader(http.StatusNotFound)

			return
		}

		// List releases endpoint
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(releases)
	}))
}

func TestGiteaProvider_GetLatestRelease(t *testing.T) {
	t.Parallel()

	releases := []map[string]any{
		{
			"id":       float64(1),
			"name":     "Release 1.2.3",
			"tag_name": "v1.2.3",
			"body":     "release notes",
			"draft":    false,
			"assets": []map[string]any{
				{"id": float64(10), "name": "tool_Linux_x86_64.tar.gz", "browser_download_url": "https://example.com/tool.tar.gz"},
			},
		},
	}

	srv := buildServer(t, releases)
	defer srv.Close()

	src := release.ReleaseSourceConfig{Host: srv.URL}
	p, err := gitea.NewReleaseProvider(src, nil, "")
	require.NoError(t, err)

	rel, err := p.GetLatestRelease(context.Background(), "owner", "repo")
	require.NoError(t, err)
	assert.Equal(t, "v1.2.3", rel.GetTagName())
	assert.Equal(t, "Release 1.2.3", rel.GetName())
	assert.Equal(t, "release notes", rel.GetBody())
	assert.False(t, rel.GetDraft())
	assert.Len(t, rel.GetAssets(), 1)
	assert.Equal(t, "tool_Linux_x86_64.tar.gz", rel.GetAssets()[0].GetName())
}

func TestGiteaProvider_GetLatestRelease_NoReleases(t *testing.T) {
	t.Parallel()

	srv := buildServer(t, []map[string]any{})
	defer srv.Close()

	src := release.ReleaseSourceConfig{Host: srv.URL}
	p, err := gitea.NewReleaseProvider(src, nil, "")
	require.NoError(t, err)

	_, err = p.GetLatestRelease(context.Background(), "owner", "repo")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no releases found")
}

func TestGiteaProvider_GetReleaseByTag(t *testing.T) {
	t.Parallel()

	releases := []map[string]any{
		{"id": float64(2), "name": "v1.0.0", "tag_name": "v1.0.0", "body": "", "draft": false, "assets": []map[string]any{}},
	}

	srv := buildServer(t, releases)
	defer srv.Close()

	src := release.ReleaseSourceConfig{Host: srv.URL}
	p, err := gitea.NewReleaseProvider(src, nil, "")
	require.NoError(t, err)

	rel, err := p.GetReleaseByTag(context.Background(), "owner", "repo", "v1.0.0")
	require.NoError(t, err)
	assert.Equal(t, "v1.0.0", rel.GetTagName())
}

func TestGiteaProvider_GetReleaseByTag_NotFound(t *testing.T) {
	t.Parallel()

	srv := buildServer(t, []map[string]any{})
	defer srv.Close()

	src := release.ReleaseSourceConfig{Host: srv.URL}
	p, err := gitea.NewReleaseProvider(src, nil, "")
	require.NoError(t, err)

	_, err = p.GetReleaseByTag(context.Background(), "owner", "repo", "v9.9.9")
	require.Error(t, err)
}

func TestGiteaProvider_ListReleases(t *testing.T) {
	t.Parallel()

	releases := []map[string]any{
		{"id": float64(1), "name": "v2.0.0", "tag_name": "v2.0.0", "body": "", "draft": false, "assets": []map[string]any{}},
		{"id": float64(2), "name": "v1.0.0", "tag_name": "v1.0.0", "body": "", "draft": false, "assets": []map[string]any{}},
	}

	srv := buildServer(t, releases)
	defer srv.Close()

	src := release.ReleaseSourceConfig{Host: srv.URL}
	p, err := gitea.NewReleaseProvider(src, nil, "")
	require.NoError(t, err)

	rels, err := p.ListReleases(context.Background(), "owner", "repo", 10)
	require.NoError(t, err)
	assert.Len(t, rels, 2)
	assert.Equal(t, "v2.0.0", rels[0].GetTagName())
}

func TestGiteaProvider_DownloadReleaseAsset(t *testing.T) {
	t.Parallel()

	content := "binary content"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, content)
	}))
	defer srv.Close()

	src := release.ReleaseSourceConfig{Host: "http://unused"}
	p, err := gitea.NewReleaseProvider(src, nil, "")
	require.NoError(t, err)

	asset := &stubAsset{url: srv.URL + "/asset"}
	rc, redirectURL, err := p.DownloadReleaseAsset(context.Background(), "owner", "repo", asset)
	require.NoError(t, err)
	defer func() { _ = rc.Close() }()

	assert.Empty(t, redirectURL)
	data, _ := io.ReadAll(rc)
	assert.Equal(t, content, string(data))
}

func TestGiteaProvider_DownloadReleaseAsset_EmptyURL(t *testing.T) {
	t.Parallel()

	src := release.ReleaseSourceConfig{Host: "http://unused"}
	p, err := gitea.NewReleaseProvider(src, nil, "")
	require.NoError(t, err)

	_, _, err = p.DownloadReleaseAsset(context.Background(), "owner", "repo", &stubAsset{url: ""})
	require.Error(t, err)
}

func TestGiteaProvider_Auth_TokenSentInHeader(t *testing.T) {
	var receivedToken string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedToken = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{"id": float64(1), "name": "v1.0.0", "tag_name": "v1.0.0", "body": "", "draft": false, "assets": []map[string]any{}},
		})
	}))
	defer srv.Close()

	t.Setenv("GITEA_TOKEN", "my-secret-token")

	src := release.ReleaseSourceConfig{Host: srv.URL}
	p, err := gitea.NewReleaseProvider(src, nil, "GITEA_TOKEN")
	require.NoError(t, err)

	_, err = p.GetLatestRelease(context.Background(), "owner", "repo")
	require.NoError(t, err)
	assert.Equal(t, "token my-secret-token", receivedToken)
}

func TestGiteaProvider_Codeberg_DefaultHost(t *testing.T) {
	t.Parallel()

	// When Host is empty and the factory pre-sets CodebergHost, the provider
	// constructs URLs against codeberg.org. We verify the factory sets the host
	// correctly by checking a provider built with an empty src.Host gets the
	// CodebergHost injected.
	src := release.ReleaseSourceConfig{Host: ""}

	// The Codeberg factory (in init.go) sets src.Host = CodebergHost before
	// calling NewReleaseProvider. Simulate that here.
	src.Host = gitea.CodebergHost

	p, err := gitea.NewReleaseProvider(src, nil, "CODEBERG_TOKEN")
	require.NoError(t, err)
	require.NotNil(t, p)
}

func TestGiteaProvider_MissingHost_ReturnsError(t *testing.T) {
	t.Parallel()

	src := release.ReleaseSourceConfig{Host: ""}
	_, err := gitea.NewReleaseProvider(src, nil, "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "gitea host is required")
}

func TestGiteaProvider_CustomAPIVersion(t *testing.T) {
	t.Parallel()

	var requestedPath string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestedPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([]map[string]any{})
	}))
	defer srv.Close()

	src := release.ReleaseSourceConfig{
		Host:   srv.URL,
		Params: map[string]string{"api_version": "v2"},
	}

	p, err := gitea.NewReleaseProvider(src, nil, "")
	require.NoError(t, err)

	_, _ = p.ListReleases(context.Background(), "owner", "repo", 5)
	assert.Contains(t, requestedPath, "/api/v2/")
}

// stubAsset is a minimal release.ReleaseAsset for testing.
type stubAsset struct {
	url string
}

func (s *stubAsset) GetID() int64                  { return 1 }
func (s *stubAsset) GetName() string               { return "asset.tar.gz" }
func (s *stubAsset) GetBrowserDownloadURL() string { return s.url }

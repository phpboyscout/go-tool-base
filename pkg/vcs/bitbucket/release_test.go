package bitbucket_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/phpboyscout/go-tool-base/pkg/vcs/bitbucket"
	"github.com/phpboyscout/go-tool-base/pkg/vcs/release"
)

// buildDownloadsServer returns a test server that serves a Bitbucket Downloads
// page response for all GET requests.
func buildDownloadsServer(t *testing.T, downloads []map[string]any) *httptest.Server {
	t.Helper()

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"values": downloads,
			"next":   "",
		})
	}))
}

func makeDownload(name, createdOn, selfHref string) map[string]any {
	return map[string]any{
		"name":       name,
		"created_on": createdOn,
		"size":       int64(1024),
		"links": map[string]any{
			"self": map[string]any{"href": selfHref},
		},
	}
}

func TestBitbucketProvider_GetLatestRelease_WithVersion(t *testing.T) {
	t.Parallel()

	ts := time.Now().UTC()
	downloads := []map[string]any{
		makeDownload("mytool_v1.2.3_Linux_x86_64.tar.gz", ts.Format(time.RFC3339), "https://example.com/mytool_Linux.tar.gz"),
		makeDownload("mytool_v1.2.3_Darwin_arm64.tar.gz", ts.Add(-time.Second).Format(time.RFC3339), "https://example.com/mytool_Darwin.tar.gz"),
	}

	srv := buildDownloadsServer(t, downloads)
	defer srv.Close()

	src := release.ReleaseSourceConfig{}

	p, err := newProviderWithBase(t, src, srv.URL)
	require.NoError(t, err)

	rel, err := p.GetLatestRelease(context.Background(), "workspace", "repo")
	require.NoError(t, err)
	assert.Equal(t, "v1.2.3", rel.GetTagName())
	assert.Len(t, rel.GetAssets(), 2)
}

func TestBitbucketProvider_GetLatestRelease_NoVersion_UsesTimestamp(t *testing.T) {
	t.Parallel()

	ts := "2026-03-29T12:00:00Z"
	downloads := []map[string]any{
		makeDownload("mytool_Linux_x86_64.tar.gz", ts, "https://example.com/asset.tar.gz"),
	}

	srv := buildDownloadsServer(t, downloads)
	defer srv.Close()

	src := release.ReleaseSourceConfig{}

	p, err := newProviderWithBase(t, src, srv.URL)
	require.NoError(t, err)

	rel, err := p.GetLatestRelease(context.Background(), "workspace", "repo")
	require.NoError(t, err)
	assert.NotEmpty(t, rel.GetTagName())
	// TagName should be an RFC3339 timestamp, not empty.
	assert.Contains(t, rel.GetTagName(), "2026-03-29")
}

func TestBitbucketProvider_GetLatestRelease_NoMatches(t *testing.T) {
	t.Parallel()

	downloads := []map[string]any{
		makeDownload("README.md", time.Now().UTC().Format(time.RFC3339), "https://example.com/README.md"),
	}

	srv := buildDownloadsServer(t, downloads)
	defer srv.Close()

	src := release.ReleaseSourceConfig{}

	p, err := newProviderWithBase(t, src, srv.URL)
	require.NoError(t, err)

	_, err = p.GetLatestRelease(context.Background(), "workspace", "repo")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no matching downloads")
}

func TestBitbucketProvider_GetLatestRelease_CustomPattern(t *testing.T) {
	t.Parallel()

	ts := time.Now().UTC()
	downloads := []map[string]any{
		makeDownload("custom-1.2.3-linux-amd64.tar.gz", ts.Format(time.RFC3339), "https://example.com/custom.tar.gz"),
		makeDownload("other_Linux_x86_64.tar.gz", ts.Add(-time.Second).Format(time.RFC3339), "https://example.com/other.tar.gz"),
	}

	srv := buildDownloadsServer(t, downloads)
	defer srv.Close()

	src := release.ReleaseSourceConfig{
		Params: map[string]string{
			"filename_pattern": `^custom-(\d+\.\d+\.\d+)-linux-amd64\.tar\.gz$`,
		},
	}

	p, err := newProviderWithBase(t, src, srv.URL)
	require.NoError(t, err)

	rel, err := p.GetLatestRelease(context.Background(), "workspace", "repo")
	require.NoError(t, err)
	assert.Equal(t, "1.2.3", rel.GetTagName())
	assert.Len(t, rel.GetAssets(), 1)
	assert.Equal(t, "custom-1.2.3-linux-amd64.tar.gz", rel.GetAssets()[0].GetName())
}

func TestBitbucketProvider_GetReleaseByTag_NotSupported(t *testing.T) {
	t.Parallel()

	src := release.ReleaseSourceConfig{}

	p, err := bitbucket.NewReleaseProvider(src, nil)
	require.NoError(t, err)

	_, err = p.GetReleaseByTag(context.Background(), "workspace", "repo", "v1.0.0")
	require.ErrorIs(t, err, release.ErrNotSupported)
}

func TestBitbucketProvider_ListReleases_NotSupported(t *testing.T) {
	t.Parallel()

	src := release.ReleaseSourceConfig{}

	p, err := bitbucket.NewReleaseProvider(src, nil)
	require.NoError(t, err)

	_, err = p.ListReleases(context.Background(), "workspace", "repo", 10)
	require.ErrorIs(t, err, release.ErrNotSupported)
}

func TestBitbucketProvider_DownloadReleaseAsset(t *testing.T) {
	t.Parallel()

	content := "binary data"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, content)
	}))
	defer srv.Close()

	src := release.ReleaseSourceConfig{}

	p, err := bitbucket.NewReleaseProvider(src, nil)
	require.NoError(t, err)

	asset := &stubAsset{url: srv.URL + "/asset"}
	rc, redirectURL, err := p.DownloadReleaseAsset(context.Background(), "", "", asset)
	require.NoError(t, err)
	defer func() { _ = rc.Close() }()

	assert.Empty(t, redirectURL)
	data, _ := io.ReadAll(rc)
	assert.Equal(t, content, string(data))
}

func TestBitbucketProvider_DownloadReleaseAsset_EmptyURL(t *testing.T) {
	t.Parallel()

	src := release.ReleaseSourceConfig{}

	p, err := bitbucket.NewReleaseProvider(src, nil)
	require.NoError(t, err)

	_, _, err = p.DownloadReleaseAsset(context.Background(), "", "", &stubAsset{url: ""})
	require.Error(t, err)
}

func TestBitbucketProvider_Auth_BasicAuthSent(t *testing.T) {
	var (
		receivedUser string
		receivedPass string
	)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedUser, receivedPass, _ = r.BasicAuth()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"values": []any{}, "next": ""})
	}))
	defer srv.Close()

	t.Setenv("BITBUCKET_USERNAME", "testuser")
	t.Setenv("BITBUCKET_APP_PASSWORD", "testpass")

	src := release.ReleaseSourceConfig{}

	p, err := newProviderWithBase(t, src, srv.URL)
	require.NoError(t, err)

	_, _ = p.GetLatestRelease(context.Background(), "workspace", "repo")
	assert.Equal(t, "testuser", receivedUser)
	assert.Equal(t, "testpass", receivedPass)
}

func TestBitbucketProvider_Private_MissingCredentials_Error(t *testing.T) {
	t.Parallel()

	src := release.ReleaseSourceConfig{Private: true}
	_, err := bitbucket.NewReleaseProvider(src, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "credentials required")
}

func TestBitbucketProvider_InvalidPattern_Error(t *testing.T) {
	t.Parallel()

	src := release.ReleaseSourceConfig{
		Params: map[string]string{"filename_pattern": "[invalid"},
	}
	_, err := bitbucket.NewReleaseProvider(src, nil)
	require.Error(t, err)
}

// newProviderWithBase creates a BitbucketReleaseProvider with the API base
// URL redirected to the given test server URL.
func newProviderWithBase(t *testing.T, src release.ReleaseSourceConfig, serverURL string) (*bitbucket.BitbucketReleaseProvider, error) {
	t.Helper()

	p, err := bitbucket.NewReleaseProvider(src, nil)
	if err != nil {
		return nil, err
	}

	p.SetAPIBase(serverURL)

	return p, nil
}

type stubAsset struct{ url string }

func (s *stubAsset) GetID() int64                  { return 0 }
func (s *stubAsset) GetName() string               { return "asset.tar.gz" }
func (s *stubAsset) GetBrowserDownloadURL() string { return s.url }

package direct_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"

	"github.com/phpboyscout/go-tool-base/pkg/vcs/direct"
	"github.com/phpboyscout/go-tool-base/pkg/vcs/release"
)

func TestDirectProvider_GetReleaseByTag(t *testing.T) {
	t.Parallel()

	src := release.ReleaseSourceConfig{
		Repo: "mytool",
		Params: map[string]string{
			"url_template": "https://releases.example.com/{tool}/{version}/{tool}_{os}_{arch}.{ext}",
		},
	}

	p, err := direct.NewReleaseProvider(src, nil)
	require.NoError(t, err)

	rel, err := p.GetReleaseByTag(context.Background(), "", "", "v1.2.3")
	require.NoError(t, err)
	assert.Equal(t, "v1.2.3", rel.GetTagName())

	assets := rel.GetAssets()
	require.Len(t, assets, 1)

	expectedOS := cases.Title(language.English).String(runtime.GOOS)
	arch := runtime.GOARCH
	if arch == "amd64" {
		arch = "x86_64"
	}

	assert.Contains(t, assets[0].GetBrowserDownloadURL(), "v1.2.3")
	assert.Contains(t, assets[0].GetBrowserDownloadURL(), expectedOS)
	assert.Contains(t, assets[0].GetBrowserDownloadURL(), arch)
}

func TestDirectProvider_URLTemplate_AllPlaceholders(t *testing.T) {
	t.Parallel()

	src := release.ReleaseSourceConfig{
		Repo: "mytool",
		Params: map[string]string{
			"url_template":   "{tool}/{version}/{version_bare}/{os}/{arch}/{ext}",
			"pinned_version": "v2.0.0",
		},
	}

	p, err := direct.NewReleaseProvider(src, nil)
	require.NoError(t, err)

	rel, err := p.GetLatestRelease(context.Background(), "", "")
	require.NoError(t, err)

	url := rel.GetAssets()[0].GetBrowserDownloadURL()
	assert.Contains(t, url, "mytool")
	assert.Contains(t, url, "v2.0.0")
	assert.Contains(t, url, "2.0.0") // version_bare
	assert.Contains(t, url, "tar.gz")
}

func TestDirectProvider_Pinned_NoNetworkCall(t *testing.T) {
	t.Parallel()

	var called bool

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		_, _ = io.WriteString(w, "v9.9.9")
	}))
	defer srv.Close()

	src := release.ReleaseSourceConfig{
		Params: map[string]string{
			"url_template":   "https://example.com/{version}.tar.gz",
			"version_url":    srv.URL,
			"pinned_version": "v1.0.0",
		},
	}

	p, err := direct.NewReleaseProvider(src, nil)
	require.NoError(t, err)

	rel, err := p.GetLatestRelease(context.Background(), "", "")
	require.NoError(t, err)
	assert.Equal(t, "v1.0.0", rel.GetTagName())
	assert.False(t, called, "pinned_version should prevent network call to version_url")
}

func TestDirectProvider_VersionUnknown(t *testing.T) {
	t.Parallel()

	src := release.ReleaseSourceConfig{
		Params: map[string]string{
			"url_template": "https://example.com/{version}.tar.gz",
			// no version_url and no pinned_version
		},
	}

	p, err := direct.NewReleaseProvider(src, nil)
	require.NoError(t, err)

	_, err = p.GetLatestRelease(context.Background(), "", "")
	require.ErrorIs(t, err, release.ErrVersionUnknown)
}

func TestDirectProvider_ListReleases_NotSupported(t *testing.T) {
	t.Parallel()

	src := release.ReleaseSourceConfig{
		Params: map[string]string{"url_template": "https://example.com/{version}.tar.gz"},
	}

	p, err := direct.NewReleaseProvider(src, nil)
	require.NoError(t, err)

	_, err = p.ListReleases(context.Background(), "", "", 10)
	require.ErrorIs(t, err, release.ErrNotSupported)
}

func TestDirectProvider_DownloadReleaseAsset(t *testing.T) {
	t.Parallel()

	content := "binary content here"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, content)
	}))
	defer srv.Close()

	src := release.ReleaseSourceConfig{
		Params: map[string]string{"url_template": "https://example.com/{version}.tar.gz"},
	}

	p, err := direct.NewReleaseProvider(src, nil)
	require.NoError(t, err)

	asset := &stubAsset{url: srv.URL + "/download"}
	rc, redirectURL, err := p.DownloadReleaseAsset(context.Background(), "", "", asset)
	require.NoError(t, err)
	defer func() { _ = rc.Close() }()

	assert.Empty(t, redirectURL)
	data, _ := io.ReadAll(rc)
	assert.Equal(t, content, string(data))
}

func TestDirectProvider_DownloadReleaseAsset_EmptyURL(t *testing.T) {
	t.Parallel()

	src := release.ReleaseSourceConfig{
		Params: map[string]string{"url_template": "https://example.com/{version}.tar.gz"},
	}

	p, err := direct.NewReleaseProvider(src, nil)
	require.NoError(t, err)

	_, _, err = p.DownloadReleaseAsset(context.Background(), "", "", &stubAsset{url: ""})
	require.Error(t, err)
}

func TestDirectProvider_MissingURLTemplate_Error(t *testing.T) {
	t.Parallel()

	src := release.ReleaseSourceConfig{Params: map[string]string{}}
	_, err := direct.NewReleaseProvider(src, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "url_template is required")
}

func TestDirectProvider_Auth_BearerTokenSent(t *testing.T) {
	var receivedAuth string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedAuth = r.Header.Get("Authorization")
		_, _ = io.WriteString(w, "data")
	}))
	defer srv.Close()

	t.Setenv("DIRECT_TOKEN", "my-bearer-token")

	src := release.ReleaseSourceConfig{
		Params: map[string]string{"url_template": "https://example.com/{version}.tar.gz"},
	}

	p, err := direct.NewReleaseProvider(src, nil)
	require.NoError(t, err)

	asset := &stubAsset{url: srv.URL + "/asset"}
	rc, _, err := p.DownloadReleaseAsset(context.Background(), "", "", asset)
	require.NoError(t, err)
	_ = rc.Close()

	assert.Equal(t, "Bearer my-bearer-token", receivedAuth)
}

type stubAsset struct{ url string }

func (s *stubAsset) GetID() int64                  { return 0 }
func (s *stubAsset) GetName() string               { return "asset.tar.gz" }
func (s *stubAsset) GetBrowserDownloadURL() string { return s.url }

// keepImport references strings to avoid unused import.
var _ = strings.NewReader

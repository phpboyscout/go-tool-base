package bitbucket_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/pkg/regexutil"
	"gitlab.com/phpboyscout/go-tool-base/pkg/vcs/bitbucket"
	"gitlab.com/phpboyscout/go-tool-base/pkg/vcs/release"
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

// TestBitbucketProvider_Auth_NotSentToForeignHost proves credentials are
// host-pinned: a download "self" link that the API response points at a
// *different* host must not receive the basic-auth credentials. The API
// (apiBase) host returns the downloads list; the foreign host serves the
// signature content and records whether Authorization was attached.
func TestBitbucketProvider_Auth_NotSentToForeignHost(t *testing.T) {
	var foreignAuthSeen bool

	foreign := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _, ok := r.BasicAuth()
		foreignAuthSeen = ok
		_, _ = w.Write([]byte("signature-bytes"))
	}))
	defer foreign.Close()

	mux := http.NewServeMux()
	api := httptest.NewServer(mux)
	defer api.Close()

	// The signature's self link points at the foreign host, not the API host.
	mux.HandleFunc("/repositories/myws/myrepo/downloads", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"values": []map[string]any{
				makeDownload("mytool_v1.2.3_Linux_x86_64.tar.gz", time.Now().UTC().Format(time.RFC3339), "https://bitbucket.org/myws/myrepo/downloads/mytool_v1.2.3_Linux_x86_64.tar.gz"),
				makeDownload(signatureFileName(), time.Now().UTC().Format(time.RFC3339), foreign.URL+"/sig-content"),
			},
			"next": "",
		})
	})

	t.Setenv("BITBUCKET_USERNAME", "testuser")
	t.Setenv("BITBUCKET_APP_PASSWORD", "testpass")

	p, err := newProviderWithBase(t, release.ReleaseSourceConfig{}, api.URL)
	require.NoError(t, err)

	rel := &stubRelease{assets: []release.ReleaseAsset{
		&stubAsset{url: "https://bitbucket.org/myws/myrepo/downloads/mytool_v1.2.3_Linux_x86_64.tar.gz"},
	}}

	_, err = p.DownloadSignature(context.Background(), rel, 8<<10)
	require.NoError(t, err)
	assert.False(t, foreignAuthSeen, "basic auth must not be sent to a non-apiBase host")
}

// TestBitbucketProvider_Auth_NotSentToForeignPaginationHost proves the
// host-pin also applies to the pagination "next" link: a next URL pointing
// at a foreign host must not carry credentials.
func TestBitbucketProvider_Auth_NotSentToForeignPaginationHost(t *testing.T) {
	var foreignAuthSeen bool

	foreign := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _, ok := r.BasicAuth()
		foreignAuthSeen = ok
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"values": []any{}, "next": ""})
	}))
	defer foreign.Close()

	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// First page points the "next" link at the foreign host.
		_ = json.NewEncoder(w).Encode(map[string]any{
			"values": []any{},
			"next":   foreign.URL + "/page2",
		})
	}))
	defer api.Close()

	t.Setenv("BITBUCKET_USERNAME", "testuser")
	t.Setenv("BITBUCKET_APP_PASSWORD", "testpass")

	p, err := newProviderWithBase(t, release.ReleaseSourceConfig{}, api.URL)
	require.NoError(t, err)

	_, _ = p.GetLatestRelease(context.Background(), "workspace", "repo")
	assert.False(t, foreignAuthSeen, "basic auth must not be sent to a foreign pagination host")
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
	require.ErrorIs(t, err, regexutil.ErrPatternInvalid)
}

func TestBitbucketProvider_OversizePattern_Error(t *testing.T) {
	t.Parallel()

	// A pattern larger than MaxPatternLength must be rejected by the
	// length cap before any compile work happens — closes H-2 from the
	// 2026-04-17 security audit.
	oversize := strings.Repeat("a", regexutil.MaxPatternLength+1)
	src := release.ReleaseSourceConfig{
		Params: map[string]string{"filename_pattern": oversize},
	}

	start := time.Now()
	_, err := bitbucket.NewReleaseProvider(src, nil)
	elapsed := time.Since(start)

	require.Error(t, err)
	require.ErrorIs(t, err, regexutil.ErrPatternTooLong)
	require.Less(t, elapsed, 100*time.Millisecond, "oversize pattern must fail fast, no compile attempted")
	// Error string must not leak the attacker-controlled pattern.
	require.NotContains(t, err.Error(), oversize)
}

func TestBitbucketProvider_DownloadSignature(t *testing.T) {
	t.Parallel()

	sig := []byte("-----BEGIN PGP SIGNATURE-----\nxyz\n-----END PGP SIGNATURE-----\n")

	mux := http.NewServeMux()

	srv := httptest.NewServer(mux)
	defer srv.Close()

	mux.HandleFunc("/repositories/myws/myrepo/downloads", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"values": []map[string]any{
				makeDownload("mytool_v1.2.3_Linux_x86_64.tar.gz", time.Now().UTC().Format(time.RFC3339), "https://bitbucket.org/myws/myrepo/downloads/mytool_v1.2.3_Linux_x86_64.tar.gz"),
				makeDownload(signatureFileName(), time.Now().UTC().Format(time.RFC3339), srv.URL+"/sig-content"),
			},
			"next": "",
		})
	})
	mux.HandleFunc("/sig-content", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(sig)
	})

	p, err := newProviderWithBase(t, release.ReleaseSourceConfig{}, srv.URL)
	require.NoError(t, err)

	rel := &stubRelease{assets: []release.ReleaseAsset{
		&stubAsset{url: "https://bitbucket.org/myws/myrepo/downloads/mytool_v1.2.3_Linux_x86_64.tar.gz"},
	}}

	got, err := p.DownloadSignature(context.Background(), rel, 8<<10)
	require.NoError(t, err)
	assert.Equal(t, sig, got)
}

func TestBitbucketProvider_DownloadSignature_NotFound(t *testing.T) {
	t.Parallel()

	srv := buildDownloadsServer(t, []map[string]any{
		makeDownload("mytool_v1.2.3_Linux_x86_64.tar.gz", time.Now().UTC().Format(time.RFC3339), "https://bitbucket.org/myws/myrepo/downloads/mytool_v1.2.3_Linux_x86_64.tar.gz"),
	})
	defer srv.Close()

	p, err := newProviderWithBase(t, release.ReleaseSourceConfig{}, srv.URL)
	require.NoError(t, err)

	rel := &stubRelease{assets: []release.ReleaseAsset{
		&stubAsset{url: "https://bitbucket.org/myws/myrepo/downloads/mytool_v1.2.3_Linux_x86_64.tar.gz"},
	}}

	_, err = p.DownloadSignature(context.Background(), rel, 8<<10)
	require.ErrorIs(t, err, release.ErrNotSupported,
		"absent checksums.txt.sig must surface as ErrNotSupported")
}

// signatureFileName returns the well-known signature filename the
// Bitbucket provider looks up. Kept as a helper so the test does not
// hard-code a constant that is private to the package.
func signatureFileName() string { return "checksums.txt.sig" }

type stubRelease struct{ assets []release.ReleaseAsset }

func (r *stubRelease) GetName() string                   { return "v1.2.3" }
func (r *stubRelease) GetTagName() string                { return "v1.2.3" }
func (r *stubRelease) GetBody() string                   { return "" }
func (r *stubRelease) GetDraft() bool                    { return false }
func (r *stubRelease) GetAssets() []release.ReleaseAsset { return r.assets }

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

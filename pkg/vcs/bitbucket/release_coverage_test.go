package bitbucket_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/pkg/vcs/bitbucket"
	"gitlab.com/phpboyscout/go-tool-base/pkg/vcs/release"
)

// checksumsFileName returns the well-known checksum manifest filename the
// Bitbucket provider looks up. Kept as a helper so the test does not
// hard-code a constant that is private to the package.
func checksumsFileName() string { return "checksums.txt" }

// TestBitbucketRelease_Getters exercises the trivial release.Release and
// release.ReleaseAsset getter methods the synthetic release exposes, which the
// higher-level happy-path tests never call directly.
func TestBitbucketRelease_Getters(t *testing.T) {
	t.Parallel()

	ts := "2026-03-29T12:00:00Z"
	downloads := []map[string]any{
		makeDownload(
			"mytool_v9.9.9_Linux_x86_64.tar.gz",
			ts,
			"https://bitbucket.org/ws/repo/downloads/mytool_v9.9.9_Linux_x86_64.tar.gz",
		),
	}

	srv := buildDownloadsServer(t, downloads)
	defer srv.Close()

	p, err := newProviderWithBase(t, release.ReleaseSourceConfig{}, srv.URL)
	require.NoError(t, err)

	rel, err := p.GetLatestRelease(context.Background(), "ws", "repo")
	require.NoError(t, err)

	// release.Release getters.
	assert.Equal(t, "v9.9.9", rel.GetName())
	assert.Equal(t, "v9.9.9", rel.GetTagName())
	assert.Empty(t, rel.GetBody())
	assert.False(t, rel.GetDraft())

	assets := rel.GetAssets()
	require.Len(t, assets, 1)

	// release.ReleaseAsset getters.
	asset := assets[0]
	assert.Equal(t, int64(0), asset.GetID())
	assert.Equal(t, "mytool_v9.9.9_Linux_x86_64.tar.gz", asset.GetName())
	assert.Equal(
		t,
		"https://bitbucket.org/ws/repo/downloads/mytool_v9.9.9_Linux_x86_64.tar.gz",
		asset.GetBrowserDownloadURL(),
	)
}

// TestBitbucketProvider_DownloadChecksumManifest covers the
// release.ChecksumProvider happy path: an uploaded checksums.txt is located by
// exact filename and its body returned.
func TestBitbucketProvider_DownloadChecksumManifest(t *testing.T) {
	t.Parallel()

	manifest := []byte("abc123  mytool_v1.2.3_Linux_x86_64.tar.gz\n")

	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	defer srv.Close()

	mux.HandleFunc("/repositories/myws/myrepo/downloads", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"values": []map[string]any{
				makeDownload(
					"mytool_v1.2.3_Linux_x86_64.tar.gz",
					time.Now().UTC().Format(time.RFC3339),
					"https://bitbucket.org/myws/myrepo/downloads/mytool_v1.2.3_Linux_x86_64.tar.gz",
				),
				makeDownload(
					checksumsFileName(),
					time.Now().UTC().Format(time.RFC3339),
					srv.URL+"/manifest-content",
				),
			},
			"next": "",
		})
	})
	mux.HandleFunc("/manifest-content", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(manifest)
	})

	p, err := newProviderWithBase(t, release.ReleaseSourceConfig{}, srv.URL)
	require.NoError(t, err)

	rel := &stubRelease{assets: []release.ReleaseAsset{
		&stubAsset{url: "https://bitbucket.org/myws/myrepo/downloads/mytool_v1.2.3_Linux_x86_64.tar.gz"},
	}}

	got, err := p.DownloadChecksumManifest(context.Background(), rel, 8<<10)
	require.NoError(t, err)
	assert.Equal(t, manifest, got)
}

// TestBitbucketProvider_DownloadChecksumManifest_NotFound covers the
// ErrNotSupported path when no checksums.txt was uploaded.
func TestBitbucketProvider_DownloadChecksumManifest_NotFound(t *testing.T) {
	t.Parallel()

	srv := buildDownloadsServer(t, []map[string]any{
		makeDownload(
			"mytool_v1.2.3_Linux_x86_64.tar.gz",
			time.Now().UTC().Format(time.RFC3339),
			"https://bitbucket.org/myws/myrepo/downloads/mytool_v1.2.3_Linux_x86_64.tar.gz",
		),
	})
	defer srv.Close()

	p, err := newProviderWithBase(t, release.ReleaseSourceConfig{}, srv.URL)
	require.NoError(t, err)

	rel := &stubRelease{assets: []release.ReleaseAsset{
		&stubAsset{url: "https://bitbucket.org/myws/myrepo/downloads/mytool_v1.2.3_Linux_x86_64.tar.gz"},
	}}

	_, err = p.DownloadChecksumManifest(context.Background(), rel, 8<<10)
	require.ErrorIs(t, err, release.ErrNotSupported)
}

// TestBitbucketProvider_DownloadChecksumManifest_NoAssets covers
// resolveDownloadURL's empty-assets guard, which returns ErrNotSupported.
func TestBitbucketProvider_DownloadChecksumManifest_NoAssets(t *testing.T) {
	t.Parallel()

	p, err := newProviderWithBase(t, release.ReleaseSourceConfig{}, "https://example.invalid")
	require.NoError(t, err)

	rel := &stubRelease{assets: nil}

	_, err = p.DownloadChecksumManifest(context.Background(), rel, 8<<10)
	require.ErrorIs(t, err, release.ErrNotSupported)
}

// TestBitbucketProvider_DownloadChecksumManifest_BadAssetURL covers
// resolveDownloadURL's parseDownloadURL error path: an asset URL that is not a
// well-formed Bitbucket downloads URL surfaces a wrapped error.
func TestBitbucketProvider_DownloadChecksumManifest_BadAssetURL(t *testing.T) {
	t.Parallel()

	p, err := newProviderWithBase(t, release.ReleaseSourceConfig{}, "https://example.invalid")
	require.NoError(t, err)

	// Path has too few segments / wrong shape for parseDownloadURL.
	rel := &stubRelease{assets: []release.ReleaseAsset{
		&stubAsset{url: "https://bitbucket.org/onlyone"},
	}}

	_, err = p.DownloadChecksumManifest(context.Background(), rel, 8<<10)
	require.Error(t, err)
	require.NotErrorIs(t, err, release.ErrNotSupported)
	assert.Contains(t, err.Error(), "checksums.txt")
}

// TestBitbucketProvider_DownloadChecksumManifest_OversizeBody covers the
// maxBytes overrun branch in fetchDownloadByURL.
func TestBitbucketProvider_DownloadChecksumManifest_OversizeBody(t *testing.T) {
	t.Parallel()

	big := strings.Repeat("x", 64)

	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	defer srv.Close()

	mux.HandleFunc("/repositories/myws/myrepo/downloads", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"values": []map[string]any{
				makeDownload(
					checksumsFileName(),
					time.Now().UTC().Format(time.RFC3339),
					srv.URL+"/manifest-content",
				),
			},
			"next": "",
		})
	})
	mux.HandleFunc("/manifest-content", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(big))
	})

	p, err := newProviderWithBase(t, release.ReleaseSourceConfig{}, srv.URL)
	require.NoError(t, err)

	rel := &stubRelease{assets: []release.ReleaseAsset{
		&stubAsset{url: "https://bitbucket.org/myws/myrepo/downloads/x.tar.gz"},
	}}

	_, err = p.DownloadChecksumManifest(context.Background(), rel, 8)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "exceeded")
}

// TestBitbucketProvider_DownloadSignature_HTTPError covers the non-200 status
// branch in fetchDownloadByURL.
func TestBitbucketProvider_DownloadSignature_HTTPError(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	defer srv.Close()

	mux.HandleFunc("/repositories/myws/myrepo/downloads", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"values": []map[string]any{
				makeDownload(
					signatureFileName(),
					time.Now().UTC().Format(time.RFC3339),
					srv.URL+"/sig-content",
				),
			},
			"next": "",
		})
	})
	mux.HandleFunc("/sig-content", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})

	p, err := newProviderWithBase(t, release.ReleaseSourceConfig{}, srv.URL)
	require.NoError(t, err)

	rel := &stubRelease{assets: []release.ReleaseAsset{
		&stubAsset{url: "https://bitbucket.org/myws/myrepo/downloads/x.tar.gz"},
	}}

	_, err = p.DownloadSignature(context.Background(), rel, 8<<10)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "HTTP 500")
}

// TestBitbucketProvider_GetLatestRelease_HTTPError covers the non-200 branch in
// fetchDownloadsPage propagating up through GetLatestRelease.
func TestBitbucketProvider_GetLatestRelease_HTTPError(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	p, err := newProviderWithBase(t, release.ReleaseSourceConfig{}, srv.URL)
	require.NoError(t, err)

	_, err = p.GetLatestRelease(context.Background(), "ws", "repo")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "HTTP 404")
}

// TestBitbucketProvider_GetLatestRelease_MalformedJSON covers the JSON decode
// error branch in fetchDownloadsPage.
func TestBitbucketProvider_GetLatestRelease_MalformedJSON(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("{not valid json"))
	}))
	defer srv.Close()

	p, err := newProviderWithBase(t, release.ReleaseSourceConfig{}, srv.URL)
	require.NoError(t, err)

	_, err = p.GetLatestRelease(context.Background(), "ws", "repo")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "decoding")
}

// TestBitbucketProvider_GetLatestRelease_Paginated covers the multi-page loop
// in fetchAllDownloads where the first page carries a same-host "next" link.
func TestBitbucketProvider_GetLatestRelease_Paginated(t *testing.T) {
	t.Parallel()

	ts := time.Now().UTC()

	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	defer srv.Close()

	mux.HandleFunc("/repositories/ws/repo/downloads", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"values": []map[string]any{
				makeDownload(
					"mytool_v2.0.0_Linux_x86_64.tar.gz",
					ts.Format(time.RFC3339),
					"https://bitbucket.org/ws/repo/downloads/mytool_v2.0.0_Linux_x86_64.tar.gz",
				),
			},
			"next": srv.URL + "/page2",
		})
	})
	mux.HandleFunc("/page2", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"values": []map[string]any{
				makeDownload(
					"mytool_v2.0.0_Darwin_arm64.tar.gz",
					ts.Add(-time.Second).Format(time.RFC3339),
					"https://bitbucket.org/ws/repo/downloads/mytool_v2.0.0_Darwin_arm64.tar.gz",
				),
			},
			"next": "",
		})
	})

	p, err := newProviderWithBase(t, release.ReleaseSourceConfig{}, srv.URL)
	require.NoError(t, err)

	rel, err := p.GetLatestRelease(context.Background(), "ws", "repo")
	require.NoError(t, err)
	assert.Equal(t, "v2.0.0", rel.GetTagName())
	assert.Len(t, rel.GetAssets(), 2, "assets from both pages should be collected")
}

// TestBitbucketProvider_DownloadReleaseAsset_HTTPError covers the non-200
// branch in DownloadReleaseAsset.
func TestBitbucketProvider_DownloadReleaseAsset_HTTPError(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()

	p, err := bitbucket.NewReleaseProvider(bitbucketSettings(t, release.ReleaseSourceConfig{}))
	require.NoError(t, err)

	_, _, err = p.DownloadReleaseAsset(
		context.Background(), "", "", &stubAsset{url: srv.URL + "/asset"},
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "HTTP 403")
}

// TestBitbucketProvider_DownloadReleaseAsset_BadURL covers the
// http.NewRequestWithContext error branch via a control character in the URL.
func TestBitbucketProvider_DownloadReleaseAsset_BadURL(t *testing.T) {
	t.Parallel()

	p, err := bitbucket.NewReleaseProvider(bitbucketSettings(t, release.ReleaseSourceConfig{}))
	require.NoError(t, err)

	_, _, err = p.DownloadReleaseAsset(
		context.Background(), "", "", &stubAsset{url: "http://exa\x7fmple.com/asset"},
	)
	require.Error(t, err)
}

// TestBitbucketProvider_DownloadReleaseAsset_TransportError covers the
// httpClient.Do error branch by pointing at a closed server.
func TestBitbucketProvider_DownloadReleaseAsset_TransportError(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	closedURL := srv.URL
	srv.Close()

	p, err := bitbucket.NewReleaseProvider(bitbucketSettings(t, release.ReleaseSourceConfig{}))
	require.NoError(t, err)

	_, _, err = p.DownloadReleaseAsset(
		context.Background(), "", "", &stubAsset{url: closedURL + "/asset"},
	)
	require.Error(t, err)
}

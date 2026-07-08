package direct_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/pkg/vcs/direct"
	"gitlab.com/phpboyscout/go-tool-base/pkg/vcs/release"
)

type testReleaseConfig struct {
	values map[string]string
	subs   map[string]testReleaseConfig
}

func (c testReleaseConfig) GetString(key string) string {
	return c.values[key]
}

func (c testReleaseConfig) Sub(key string) release.Config {
	sub, ok := c.subs[key]
	if !ok {
		return nil
	}

	return sub
}

// newProvider is a small helper that builds a DirectReleaseProvider and fails
// the test if construction errors.
func newProvider(t *testing.T, params map[string]string, cfg release.Config) *direct.DirectReleaseProvider {
	t.Helper()

	p, err := direct.NewReleaseProvider(direct.SettingsFromConfig(release.ReleaseSourceConfig{Params: params}, cfg))
	require.NoError(t, err)

	return p
}

// TestDirectRelease_Accessors exercises the directRelease/directAsset getters
// that are reached only through the synthetic release.
func TestDirectRelease_Accessors(t *testing.T) {
	t.Parallel()

	p := newProvider(t, map[string]string{
		"url_template": "https://example.com/{tool}/{version}.{ext}",
	}, nil)

	rel, err := p.GetReleaseByTag(context.Background(), "", "", "v1.2.3")
	require.NoError(t, err)

	// directRelease accessors.
	assert.Equal(t, "v1.2.3", rel.GetName())
	assert.Equal(t, "v1.2.3", rel.GetTagName())
	assert.Empty(t, rel.GetBody())
	assert.False(t, rel.GetDraft())

	assets := rel.GetAssets()
	require.Len(t, assets, 1)

	// directAsset accessors.
	asset := assets[0]
	assert.Equal(t, int64(0), asset.GetID())
	assert.Equal(t, "v1.2.3.tar.gz", asset.GetName())
	assert.Contains(t, asset.GetBrowserDownloadURL(), "v1.2.3")
}

// TestDirectProvider_SetToolName confirms SetToolName overrides the tool name
// used during template expansion.
func TestDirectProvider_SetToolName(t *testing.T) {
	t.Parallel()

	p := newProvider(t, map[string]string{
		"url_template":   "https://example.com/{tool}/{version}.{ext}",
		"pinned_version": "v1.0.0",
	}, nil)

	p.SetToolName("overridden")

	rel, err := p.GetLatestRelease(context.Background(), "", "")
	require.NoError(t, err)

	assert.Contains(t, rel.GetAssets()[0].GetBrowserDownloadURL(), "overridden")
}

// TestDirectProvider_DefaultToolName proves the "tool" default is used when no
// Repo and no SetToolName override are supplied.
func TestDirectProvider_DefaultToolName(t *testing.T) {
	t.Parallel()

	p := newProvider(t, map[string]string{
		"url_template":   "https://example.com/{tool}/{version}.{ext}",
		"pinned_version": "v1.0.0",
	}, nil)

	rel, err := p.GetLatestRelease(context.Background(), "", "")
	require.NoError(t, err)

	assert.Contains(t, rel.GetAssets()[0].GetBrowserDownloadURL(), "/tool/")
}

// TestResolveToken_FromConfig covers the cfg != nil + Sub("direct").token path.
func TestResolveToken_FromConfig(t *testing.T) {
	t.Parallel()

	var receivedAuth string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedAuth = r.Header.Get("Authorization")
		_, _ = io.WriteString(w, "data")
	}))
	defer srv.Close()

	cfg := testReleaseConfig{subs: map[string]testReleaseConfig{
		"direct": {values: map[string]string{"token": "config-token"}},
	}}

	p := newProvider(t, map[string]string{
		"url_template": "https://example.com/{version}.tar.gz",
	}, cfg)

	asset := &stubAsset{url: srv.URL + "/asset"}
	rc, _, err := p.DownloadReleaseAsset(context.Background(), "", "", asset)
	require.NoError(t, err)
	_ = rc.Close()

	assert.Equal(t, "Bearer config-token", receivedAuth)
}

// TestResolveToken_ConfigEmptyFallsBack proves an empty config token falls
// through to the DIRECT_TOKEN env var.
func TestResolveToken_ConfigEmptyFallsBack(t *testing.T) {
	// Not parallel: uses t.Setenv.
	t.Setenv("DIRECT_TOKEN", "env-token")

	var receivedAuth string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedAuth = r.Header.Get("Authorization")
		_, _ = io.WriteString(w, "data")
	}))
	defer srv.Close()

	p := newProvider(t, map[string]string{
		"url_template": "https://example.com/{version}.tar.gz",
	}, testReleaseConfig{})

	asset := &stubAsset{url: srv.URL + "/asset"}
	rc, _, err := p.DownloadReleaseAsset(context.Background(), "", "", asset)
	require.NoError(t, err)
	_ = rc.Close()

	assert.Equal(t, "Bearer env-token", receivedAuth)
}

// TestDirectProvider_DownloadReleaseAsset_HTTPError covers the non-200 branch.
func TestDirectProvider_DownloadReleaseAsset_HTTPError(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	p := newProvider(t, map[string]string{
		"url_template": "https://example.com/{version}.tar.gz",
	}, nil)

	_, _, err := p.DownloadReleaseAsset(context.Background(), "", "", &stubAsset{url: srv.URL + "/missing"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "HTTP 404")
}

// TestDirectProvider_DownloadReleaseAsset_BadURL covers the request-build
// error path (an invalid control character in the URL).
func TestDirectProvider_DownloadReleaseAsset_BadURL(t *testing.T) {
	t.Parallel()

	p := newProvider(t, map[string]string{
		"url_template": "https://example.com/{version}.tar.gz",
	}, nil)

	_, _, err := p.DownloadReleaseAsset(context.Background(), "", "", &stubAsset{url: "http://example.com/\x7f"})
	require.Error(t, err)
}

// TestDirectProvider_DownloadReleaseAsset_DialError covers the client.Do error
// path (connection refused to a closed server).
func TestDirectProvider_DownloadReleaseAsset_DialError(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	url := srv.URL
	srv.Close() // immediately closed → connection refused

	p := newProvider(t, map[string]string{
		"url_template": "https://example.com/{version}.tar.gz",
	}, nil)

	_, _, err := p.DownloadReleaseAsset(context.Background(), "", "", &stubAsset{url: url})
	require.Error(t, err)
}

// TestDirectProvider_DownloadChecksumManifest covers the checksum fetch happy
// path including {version} expansion.
func TestDirectProvider_DownloadChecksumManifest(t *testing.T) {
	t.Parallel()

	manifest := []byte("abc123  mytool_Linux_x86_64.tar.gz\n")

	var gotPath string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_, _ = w.Write(manifest)
	}))
	defer srv.Close()

	p := newProvider(t, map[string]string{
		"url_template":          "https://example.com/{version}.tar.gz",
		"checksum_url_template": srv.URL + "/{version}/checksums.txt",
	}, nil)

	rel, err := p.GetReleaseByTag(context.Background(), "", "", "v1.2.3")
	require.NoError(t, err)

	got, err := p.DownloadChecksumManifest(context.Background(), rel, 8<<10)
	require.NoError(t, err)
	assert.Equal(t, manifest, got)
	assert.Equal(t, "/v1.2.3/checksums.txt", gotPath)
}

// TestDirectProvider_DownloadChecksumManifest_NotSupported proves an absent
// checksum_url_template surfaces ErrNotSupported.
func TestDirectProvider_DownloadChecksumManifest_NotSupported(t *testing.T) {
	t.Parallel()

	p := newProvider(t, map[string]string{
		"url_template": "https://example.com/{version}.tar.gz",
	}, nil)

	rel, err := p.GetReleaseByTag(context.Background(), "", "", "v1.0.0")
	require.NoError(t, err)

	_, err = p.DownloadChecksumManifest(context.Background(), rel, 8<<10)
	require.ErrorIs(t, err, release.ErrNotSupported)
}

// TestDirectProvider_DownloadChecksumManifest_HTTPError covers the non-200
// branch of fetchTemplatedURL.
func TestDirectProvider_DownloadChecksumManifest_HTTPError(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	p := newProvider(t, map[string]string{
		"url_template":          "https://example.com/{version}.tar.gz",
		"checksum_url_template": srv.URL + "/{version}/checksums.txt",
	}, nil)

	rel, err := p.GetReleaseByTag(context.Background(), "", "", "v1.0.0")
	require.NoError(t, err)

	_, err = p.DownloadChecksumManifest(context.Background(), rel, 8<<10)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "HTTP 500")
	assert.Contains(t, err.Error(), "checksums manifest")
}

// TestDirectProvider_DownloadSignature_DialError covers the client.Do error
// branch of fetchTemplatedURL.
func TestDirectProvider_DownloadSignature_DialError(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	base := srv.URL
	srv.Close()

	p := newProvider(t, map[string]string{
		"url_template":           "https://example.com/{version}.tar.gz",
		"signature_url_template": base + "/{version}.sig",
	}, nil)

	rel, err := p.GetReleaseByTag(context.Background(), "", "", "v1.0.0")
	require.NoError(t, err)

	_, err = p.DownloadSignature(context.Background(), rel, 8<<10)
	require.Error(t, err)
}

// TestDirectProvider_DownloadSignature_AuthHeader confirms the bearer token is
// sent on templated fetches.
func TestDirectProvider_DownloadSignature_AuthHeader(t *testing.T) {
	// Not parallel: uses t.Setenv.
	t.Setenv("DIRECT_TOKEN", "sig-token")

	var receivedAuth string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedAuth = r.Header.Get("Authorization")
		_, _ = w.Write([]byte("sig"))
	}))
	defer srv.Close()

	p := newProvider(t, map[string]string{
		"url_template":           "https://example.com/{version}.tar.gz",
		"signature_url_template": srv.URL + "/{version}.sig",
	}, nil)

	rel, err := p.GetReleaseByTag(context.Background(), "", "", "v1.0.0")
	require.NoError(t, err)

	_, err = p.DownloadSignature(context.Background(), rel, 8<<10)
	require.NoError(t, err)
	assert.Equal(t, "Bearer sig-token", receivedAuth)
}

// TestDirectProvider_VersionURL_HTTPError covers the non-200 branch of
// fetchVersion.
func TestDirectProvider_VersionURL_HTTPError(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()

	p := newProvider(t, map[string]string{
		"url_template": "https://example.com/{version}.tar.gz",
		"version_url":  srv.URL,
	}, nil)

	_, err := p.GetLatestRelease(context.Background(), "", "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "HTTP 403")
}

// TestDirectProvider_VersionURL_DialError covers the client.Do error branch of
// fetchVersion.
func TestDirectProvider_VersionURL_DialError(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	url := srv.URL
	srv.Close()

	p := newProvider(t, map[string]string{
		"url_template": "https://example.com/{version}.tar.gz",
		"version_url":  url,
	}, nil)

	_, err := p.GetLatestRelease(context.Background(), "", "")
	require.Error(t, err)
}

// TestDirectProvider_VersionURL_ParseErrors covers the malformed-document and
// missing-key error branches across every parser.
func TestDirectProvider_VersionURL_ParseErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		contentType string
		format      string
		key         string
		body        string
		wantSubstr  string
	}{
		{
			name:        "json malformed",
			contentType: "application/json",
			body:        `{not json`,
			wantSubstr:  "parsing JSON",
		},
		{
			name:        "json key absent",
			contentType: "application/json",
			body:        `{"unrelated":"x"}`,
			wantSubstr:  "version key not found",
		},
		{
			name:        "json key wrong type ignored then not found",
			contentType: "application/json",
			key:         "version",
			body:        `{"version":123}`,
			wantSubstr:  "version key not found",
		},
		{
			name:        "yaml malformed",
			contentType: "application/yaml",
			body:        "key: : : bad",
			wantSubstr:  "parsing YAML",
		},
		{
			name:        "yaml key absent",
			contentType: "application/yaml",
			body:        "unrelated: x\n",
			wantSubstr:  "version key not found",
		},
		{
			name:        "yaml key non-string ignored",
			contentType: "application/yaml",
			key:         "version",
			body:        "version: 123\n",
			wantSubstr:  "version key not found",
		},
		{
			name:        "xml malformed",
			contentType: "application/xml",
			body:        "<release><version>oops",
			wantSubstr:  "parsing XML",
		},
		{
			name:        "xml key absent",
			contentType: "application/xml",
			body:        `<release><unrelated>x</unrelated></release>`,
			wantSubstr:  "version key not found",
		},
		{
			name:        "xml key present but empty",
			contentType: "application/xml",
			key:         "version",
			body:        `<release><version>   </version></release>`,
			wantSubstr:  "version key not found",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			srv := versionServer(t, tc.contentType, tc.body)
			defer srv.Close()

			params := map[string]string{
				"url_template": "https://example.com/{version}.tar.gz",
				"version_url":  srv.URL,
			}
			if tc.format != "" {
				params["version_format"] = tc.format
			}

			if tc.key != "" {
				params["version_key"] = tc.key
			}

			p := newProvider(t, params, nil)

			_, err := p.GetLatestRelease(context.Background(), "", "")
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.wantSubstr)
		})
	}
}

// TestDirectProvider_VersionURL_FormatAutoDetect covers each Content-Type
// auto-detection branch of detectFormat through the public surface.
func TestDirectProvider_VersionURL_FormatAutoDetect(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		contentType string
		body        string
		want        string
	}{
		{"text/json", "text/json", `{"tag_name":"v1.0.0"}`, "v1.0.0"},
		{"text/yaml", "text/yaml", "tag_name: v1.1.0\n", "v1.1.0"},
		{"x-yaml", "application/x-yaml", "tag_name: v1.2.0\n", "v1.2.0"},
		{"text/xml", "text/xml", `<r><tag_name>v1.3.0</tag_name></r>`, "v1.3.0"},
		{"empty content-type", "", "v1.4.0\n", "v1.4.0"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			srv := versionServer(t, tc.contentType, tc.body)
			defer srv.Close()

			p := newProvider(t, map[string]string{
				"url_template": "https://example.com/{version}.tar.gz",
				"version_url":  srv.URL,
			}, nil)

			rel, err := p.GetLatestRelease(context.Background(), "", "")
			require.NoError(t, err)
			assert.Equal(t, tc.want, rel.GetTagName())
		})
	}
}

// TestDirectProvider_VersionURL_BadURL covers the request-build error path in
// fetchVersion.
func TestDirectProvider_VersionURL_BadURL(t *testing.T) {
	t.Parallel()

	p := newProvider(t, map[string]string{
		"url_template": "https://example.com/{version}.tar.gz",
		"version_url":  "http://example.com/\x7f",
	}, nil)

	_, err := p.GetLatestRelease(context.Background(), "", "")
	require.Error(t, err)
}

// TestDirectProvider_RegisteredFactory exercises the init() registration so the
// factory closure is invoked through release.Register.
func TestDirectProvider_RegisteredFactory(t *testing.T) {
	t.Parallel()

	src := release.ReleaseSourceConfig{
		Repo: "mytool",
		Params: map[string]string{
			"url_template": "https://example.com/{version}.tar.gz",
		},
	}

	factory, err := release.Lookup(release.SourceTypeDirect)
	require.NoError(t, err)

	p, err := factory(src, nil)
	require.NoError(t, err)
	require.NotNil(t, p)

	rel, err := p.GetReleaseByTag(context.Background(), "", "", "v1.0.0")
	require.NoError(t, err)
	assert.Equal(t, "v1.0.0", rel.GetTagName())
}

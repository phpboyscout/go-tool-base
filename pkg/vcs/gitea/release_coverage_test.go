package gitea

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/pkg/config"
	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	"gitlab.com/phpboyscout/go-tool-base/pkg/vcs/release"
)

// jsonHandler returns a handler that always responds with the given status code
// and body. Used to drive error-path coverage.
func jsonHandler(status int, body string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}
}

func newCfgFromViper(t *testing.T, set map[string]any) config.Containable {
	t.Helper()

	v := viper.New()
	for k, val := range set {
		v.Set(k, val)
	}

	return config.NewContainerFromViper(logger.NewNoop(), v)
}

// TestNewReleaseProvider_ConfigURLAPIOverride covers the cfg "url.api" override
// branch in NewReleaseProvider, which takes precedence over src.Host.
func TestNewReleaseProvider_ConfigURLAPIOverride(t *testing.T) {
	t.Parallel()

	var requestedHost string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestedHost = r.Host
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{"id": float64(1), "name": "v1.0.0", "tag_name": "v1.0.0", "assets": []map[string]any{}},
		})
	}))
	defer srv.Close()

	cfg := newCfgFromViper(t, map[string]any{"url.api": srv.URL})

	// src.Host points elsewhere; the cfg override must win.
	src := release.ReleaseSourceConfig{Host: "http://should-be-ignored.invalid"}

	p, err := NewReleaseProvider(src, cfg, "")
	require.NoError(t, err)

	_, err = p.ListReleases(context.Background(), "owner", "repo", 5)
	require.NoError(t, err)
	assert.NotEmpty(t, requestedHost)
}

// TestNewReleaseProvider_TokenFromConfigSubtree covers token resolution via the
// "gitea" config subtree (not the fallback env var).
func TestNewReleaseProvider_TokenFromConfigSubtree(t *testing.T) {
	t.Parallel()

	var receivedAuth string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{"id": float64(1), "name": "v1.0.0", "tag_name": "v1.0.0", "assets": []map[string]any{}},
		})
	}))
	defer srv.Close()

	cfg := newCfgFromViper(t, map[string]any{"gitea.auth.value": "subtree-token"})

	src := release.ReleaseSourceConfig{Host: srv.URL}

	p, err := NewReleaseProvider(src, cfg, "")
	require.NoError(t, err)

	_, err = p.GetLatestRelease(context.Background(), "owner", "repo")
	require.NoError(t, err)
	assert.Equal(t, "token subtree-token", receivedAuth)
}

// TestGetLatestRelease_ServerError covers the error path of GetLatestRelease /
// listReleasesRaw / getJSON when the server returns a 5xx.
func TestGetLatestRelease_ServerError(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(jsonHandler(http.StatusInternalServerError, "boom"))
	defer srv.Close()

	p, err := NewReleaseProvider(release.ReleaseSourceConfig{Host: srv.URL}, nil, "")
	require.NoError(t, err)

	_, err = p.GetLatestRelease(context.Background(), "owner", "repo")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "HTTP 500")
}

// TestListReleases_ServerError covers ListReleases error propagation.
func TestListReleases_ServerError(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(jsonHandler(http.StatusBadGateway, "nope"))
	defer srv.Close()

	p, err := NewReleaseProvider(release.ReleaseSourceConfig{Host: srv.URL}, nil, "")
	require.NoError(t, err)

	_, err = p.ListReleases(context.Background(), "owner", "repo", 10)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "listing releases")
}

// TestGetReleaseByTag_NotFound covers the 4xx error path of GetReleaseByTag.
func TestGetReleaseByTag_NotFound(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(jsonHandler(http.StatusNotFound, ""))
	defer srv.Close()

	p, err := NewReleaseProvider(release.ReleaseSourceConfig{Host: srv.URL}, nil, "")
	require.NoError(t, err)

	_, err = p.GetReleaseByTag(context.Background(), "owner", "repo", "v9.9.9")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "getting release by tag")
}

// TestGetJSON_MalformedJSON covers the decode-error branch in getJSON.
func TestGetJSON_MalformedJSON(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(jsonHandler(http.StatusOK, "{not valid json"))
	defer srv.Close()

	p, err := NewReleaseProvider(release.ReleaseSourceConfig{Host: srv.URL}, nil, "")
	require.NoError(t, err)

	_, err = p.ListReleases(context.Background(), "owner", "repo", 10)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "decoding gitea API response")
}

// TestGetJSON_RequestError covers the http.NewRequestWithContext error branch
// (invalid endpoint host produced by a malformed configured host).
func TestGetJSON_RequestError(t *testing.T) {
	t.Parallel()

	// A control character in the host makes NewRequestWithContext fail when
	// building the request URL.
	p, err := NewReleaseProvider(release.ReleaseSourceConfig{Host: "http://exa\x7fmple"}, nil, "")
	require.NoError(t, err)

	_, err = p.ListReleases(context.Background(), "owner", "repo", 1)
	require.Error(t, err)
}

// TestGetJSON_TransportError covers the httpClient.Do error branch by pointing
// at a closed server (connection refused).
func TestGetJSON_TransportError(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	url := srv.URL
	srv.Close() // close immediately so the connection is refused.

	p, err := NewReleaseProvider(release.ReleaseSourceConfig{Host: url}, nil, "")
	require.NoError(t, err)

	_, err = p.ListReleases(context.Background(), "owner", "repo", 1)
	require.Error(t, err)
}

// TestDownloadReleaseAsset_HTTPError covers the non-200 response branch of
// DownloadReleaseAsset.
func TestDownloadReleaseAsset_HTTPError(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(jsonHandler(http.StatusForbidden, "denied"))
	defer srv.Close()

	p, err := NewReleaseProvider(release.ReleaseSourceConfig{Host: srv.URL}, nil, "")
	require.NoError(t, err)

	_, _, err = p.DownloadReleaseAsset(
		context.Background(), "owner", "repo",
		&giteaAsset{raw: giteaAssetJSON{BrowserDownloadURL: srv.URL + "/asset"}},
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "HTTP 403")
}

// TestDownloadReleaseAsset_RequestError covers the NewRequestWithContext error
// branch in DownloadReleaseAsset (malformed URL).
func TestDownloadReleaseAsset_RequestError(t *testing.T) {
	t.Parallel()

	p, err := NewReleaseProvider(release.ReleaseSourceConfig{Host: "http://unused"}, nil, "")
	require.NoError(t, err)

	_, _, err = p.DownloadReleaseAsset(
		context.Background(), "owner", "repo",
		&giteaAsset{raw: giteaAssetJSON{BrowserDownloadURL: "http://exa\x7fmple/asset"}},
	)
	require.Error(t, err)
}

// TestDownloadReleaseAsset_TransportError covers httpClient.Do failure.
func TestDownloadReleaseAsset_TransportError(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	assetURL := srv.URL + "/asset"
	srv.Close()

	p, err := NewReleaseProvider(release.ReleaseSourceConfig{Host: "http://unused"}, nil, "")
	require.NoError(t, err)

	_, _, err = p.DownloadReleaseAsset(
		context.Background(), "owner", "repo",
		&giteaAsset{raw: giteaAssetJSON{BrowserDownloadURL: assetURL}},
	)
	require.Error(t, err)
}

// TestAssetHostTrusted covers the branches of assetHostTrusted directly,
// including the malformed-target and host-mismatch fail-closed cases.
func TestAssetHostTrusted(t *testing.T) {
	t.Parallel()

	p := &GiteaReleaseProvider{baseURL: "https://git.example.com/api/v1"}

	assert.True(t, p.assetHostTrusted("https://git.example.com/attachments/x"))
	assert.False(t, p.assetHostTrusted("https://evil.example.org/x"))
	assert.False(t, p.assetHostTrusted("://malformed"))

	// A provider with an unparseable base URL fails closed.
	bad := &GiteaReleaseProvider{baseURL: "://bad-base"}
	assert.False(t, bad.assetHostTrusted("https://anything/x"))

	// A base URL with no host fails closed.
	noHost := &GiteaReleaseProvider{baseURL: "/api/v1"}
	assert.False(t, noHost.assetHostTrusted("https://anything/x"))
}

// TestGetID covers the giteaAsset.GetID accessor.
func TestGetID(t *testing.T) {
	t.Parallel()

	a := &giteaAsset{raw: giteaAssetJSON{ID: 42}}
	assert.Equal(t, int64(42), a.GetID())
}

// TestInit_CodebergFactory_DefaultsHost exercises the init()-registered Codeberg
// factory, including the empty-Host default-injection branch, and the Gitea
// factory. Not parallel because it relies on the package-global registry.
func TestInit_CodebergFactory_DefaultsHost(t *testing.T) {
	codebergFactory, err := release.Lookup(release.SourceTypeCodeberg)
	require.NoError(t, err)

	// Empty Host triggers the CodebergHost default-injection branch.
	prov, err := codebergFactory(release.ReleaseSourceConfig{}, nil)
	require.NoError(t, err)
	require.NotNil(t, prov)

	gp, ok := prov.(*GiteaReleaseProvider)
	require.True(t, ok)
	assert.Equal(t, CodebergHost+"/api/"+defaultAPIVersion, gp.baseURL)

	// A non-empty Host is preserved (the default-injection branch is skipped).
	prov2, err := codebergFactory(release.ReleaseSourceConfig{Host: "https://my.forgejo"}, nil)
	require.NoError(t, err)
	gp2, ok := prov2.(*GiteaReleaseProvider)
	require.True(t, ok)
	assert.Equal(t, "https://my.forgejo/api/"+defaultAPIVersion, gp2.baseURL)

	// The Gitea factory is also registered.
	giteaFactory, err := release.Lookup(release.SourceTypeGitea)
	require.NoError(t, err)

	prov3, err := giteaFactory(release.ReleaseSourceConfig{Host: "https://git.example.com"}, nil)
	require.NoError(t, err)
	require.NotNil(t, prov3)
}

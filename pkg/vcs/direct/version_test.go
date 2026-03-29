package direct_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/phpboyscout/go-tool-base/pkg/vcs/direct"
	"github.com/phpboyscout/go-tool-base/pkg/vcs/release"
)

func versionServer(t *testing.T, contentType, body string) *httptest.Server {
	t.Helper()

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", contentType)
		_, _ = w.Write([]byte(body))
	}))
}

func TestDirectProvider_VersionURL_PlainText(t *testing.T) {
	t.Parallel()

	srv := versionServer(t, "text/plain", "v1.2.3\n")
	defer srv.Close()

	src := release.ReleaseSourceConfig{
		Params: map[string]string{
			"url_template": "https://example.com/{version}.tar.gz",
			"version_url":  srv.URL,
		},
	}

	p, err := direct.NewReleaseProvider(src, nil)
	require.NoError(t, err)

	rel, err := p.GetLatestRelease(context.Background(), "", "")
	require.NoError(t, err)
	assert.Equal(t, "v1.2.3", rel.GetTagName())
}

func TestDirectProvider_VersionURL_JSON_TagName(t *testing.T) {
	t.Parallel()

	srv := versionServer(t, "application/json", `{"tag_name":"v2.0.0","prerelease":false}`)
	defer srv.Close()

	src := release.ReleaseSourceConfig{
		Params: map[string]string{
			"url_template": "https://example.com/{version}.tar.gz",
			"version_url":  srv.URL,
		},
	}

	p, err := direct.NewReleaseProvider(src, nil)
	require.NoError(t, err)

	rel, err := p.GetLatestRelease(context.Background(), "", "")
	require.NoError(t, err)
	assert.Equal(t, "v2.0.0", rel.GetTagName())
}

func TestDirectProvider_VersionURL_JSON_CustomKey(t *testing.T) {
	t.Parallel()

	srv := versionServer(t, "application/json", `{"latest":"v3.1.0"}`)
	defer srv.Close()

	src := release.ReleaseSourceConfig{
		Params: map[string]string{
			"url_template": "https://example.com/{version}.tar.gz",
			"version_url":  srv.URL,
			"version_key":  "latest",
		},
	}

	p, err := direct.NewReleaseProvider(src, nil)
	require.NoError(t, err)

	rel, err := p.GetLatestRelease(context.Background(), "", "")
	require.NoError(t, err)
	assert.Equal(t, "v3.1.0", rel.GetTagName())
}

func TestDirectProvider_VersionURL_YAML(t *testing.T) {
	t.Parallel()

	srv := versionServer(t, "application/yaml", "version: v1.5.0\nreleased_at: 2026-03-29\n")
	defer srv.Close()

	src := release.ReleaseSourceConfig{
		Params: map[string]string{
			"url_template": "https://example.com/{version}.tar.gz",
			"version_url":  srv.URL,
			"version_key":  "version",
		},
	}

	p, err := direct.NewReleaseProvider(src, nil)
	require.NoError(t, err)

	rel, err := p.GetLatestRelease(context.Background(), "", "")
	require.NoError(t, err)
	assert.Equal(t, "v1.5.0", rel.GetTagName())
}

func TestDirectProvider_VersionURL_XML(t *testing.T) {
	t.Parallel()

	srv := versionServer(t, "application/xml", `<release><version>v4.0.1</version></release>`)
	defer srv.Close()

	src := release.ReleaseSourceConfig{
		Params: map[string]string{
			"url_template": "https://example.com/{version}.tar.gz",
			"version_url":  srv.URL,
			"version_key":  "version",
		},
	}

	p, err := direct.NewReleaseProvider(src, nil)
	require.NoError(t, err)

	rel, err := p.GetLatestRelease(context.Background(), "", "")
	require.NoError(t, err)
	assert.Equal(t, "v4.0.1", rel.GetTagName())
}

func TestDirectProvider_VersionURL_FormatOverride(t *testing.T) {
	t.Parallel()

	// Server returns JSON but with wrong Content-Type; format param overrides.
	srv := versionServer(t, "text/plain", `{"version":"v5.0.0"}`)
	defer srv.Close()

	src := release.ReleaseSourceConfig{
		Params: map[string]string{
			"url_template":   "https://example.com/{version}.tar.gz",
			"version_url":    srv.URL,
			"version_format": "json",
			"version_key":    "version",
		},
	}

	p, err := direct.NewReleaseProvider(src, nil)
	require.NoError(t, err)

	rel, err := p.GetLatestRelease(context.Background(), "", "")
	require.NoError(t, err)
	assert.Equal(t, "v5.0.0", rel.GetTagName())
}

func TestDirectProvider_VersionURL_FallbackKey(t *testing.T) {
	t.Parallel()

	// No version_key set; provider should try "tag_name" then "version".
	srv := versionServer(t, "application/json", `{"version":"v6.0.0"}`)
	defer srv.Close()

	src := release.ReleaseSourceConfig{
		Params: map[string]string{
			"url_template": "https://example.com/{version}.tar.gz",
			"version_url":  srv.URL,
			// no version_key
		},
	}

	p, err := direct.NewReleaseProvider(src, nil)
	require.NoError(t, err)

	rel, err := p.GetLatestRelease(context.Background(), "", "")
	require.NoError(t, err)
	assert.Equal(t, "v6.0.0", rel.GetTagName())
}

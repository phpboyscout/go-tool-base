// Package direct provides a release.Provider implementation for tools
// distributed via arbitrary HTTP servers. Asset URLs are constructed from a
// configurable template; version detection is optional and supports plain text,
// JSON, YAML, and XML endpoints.
package direct

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"runtime"
	"strings"

	"github.com/cockroachdb/errors"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"

	"github.com/phpboyscout/go-tool-base/pkg/config"
	gtbhttp "github.com/phpboyscout/go-tool-base/pkg/http"
	"github.com/phpboyscout/go-tool-base/pkg/vcs/release"
)

const (
	directTokenEnv = "DIRECT_TOKEN"
)

// directRelease implements release.Release for a single direct-download release.
type directRelease struct {
	tagName     string
	downloadURL string
}

func (r *directRelease) GetName() string    { return r.tagName }
func (r *directRelease) GetTagName() string { return r.tagName }
func (r *directRelease) GetBody() string    { return "" }
func (r *directRelease) GetDraft() bool     { return false }

func (r *directRelease) GetAssets() []release.ReleaseAsset {
	return []release.ReleaseAsset{&directAsset{
		name: fmt.Sprintf("%s.tar.gz", r.tagName),
		url:  r.downloadURL,
	}}
}

// directAsset implements release.ReleaseAsset.
type directAsset struct {
	name string
	url  string
}

func (a *directAsset) GetID() int64                  { return 0 }
func (a *directAsset) GetName() string               { return a.name }
func (a *directAsset) GetBrowserDownloadURL() string { return a.url }

// DirectReleaseProvider implements release.Provider for direct HTTP downloads.
type DirectReleaseProvider struct {
	urlTemplate         string
	checksumURLTemplate string
	versionURL          string
	versionFormat       string
	versionKey          string
	pinnedVersion       string
	token               string
	toolName            string
	httpClient          *http.Client
}

// NewReleaseProvider constructs a DirectReleaseProvider from a ReleaseSourceConfig.
//
// Required Params key: url_template.
// Optional Params keys: version_url, version_format, version_key,
// pinned_version, checksum_url_template.
//
// Token resolution: cfg key "direct.token", then DIRECT_TOKEN env var.
func NewReleaseProvider(src release.ReleaseSourceConfig, cfg config.Containable) (*DirectReleaseProvider, error) {
	urlTemplate := src.Params["url_template"]
	if urlTemplate == "" {
		return nil, errors.WithHint(
			errors.New("url_template is required for the direct release provider"),
			"Set ReleaseSource.Params[\"url_template\"] to a URL template with placeholders "+
				"such as {version}, {os}, {arch}, {tool}, {ext}.",
		)
	}

	token := resolveToken(cfg)

	return &DirectReleaseProvider{
		urlTemplate:         urlTemplate,
		checksumURLTemplate: src.Params["checksum_url_template"],
		versionURL:          src.Params["version_url"],
		versionFormat:       src.Params["version_format"],
		versionKey:          src.Params["version_key"],
		pinnedVersion:       src.Params["pinned_version"],
		token:               token,
		toolName:            src.Repo, // use Repo as tool name fallback; callers can override via url_template
		httpClient:          gtbhttp.NewClient(),
	}, nil
}

// SetToolName sets the tool name used in URL template expansion. This is called
// by the setup package when the Props.Tool.Name is available.
func (p *DirectReleaseProvider) SetToolName(name string) {
	p.toolName = name
}

// GetLatestRelease fetches the latest version from the version endpoint and
// returns a synthetic release. Returns ErrVersionUnknown if no version source
// is configured.
func (p *DirectReleaseProvider) GetLatestRelease(ctx context.Context, _, _ string) (release.Release, error) {
	version, err := p.resolveVersion(ctx)
	if err != nil {
		return nil, err
	}

	return p.syntheticRelease(version), nil
}

// GetReleaseByTag constructs a synthetic release for the given tag without any
// network call.
func (p *DirectReleaseProvider) GetReleaseByTag(_ context.Context, _, _, tag string) (release.Release, error) {
	return p.syntheticRelease(tag), nil
}

// ListReleases is not supported for direct HTTP providers.
func (p *DirectReleaseProvider) ListReleases(_ context.Context, _, _ string, _ int) ([]release.Release, error) {
	return nil, errors.WithHint(
		release.ErrNotSupported,
		"The direct release provider does not support listing releases. Use GetLatestRelease or GetReleaseByTag.",
	)
}

// DownloadReleaseAsset downloads the asset at its BrowserDownloadURL.
func (p *DirectReleaseProvider) DownloadReleaseAsset(ctx context.Context, _, _ string, asset release.ReleaseAsset) (io.ReadCloser, string, error) {
	downloadURL := asset.GetBrowserDownloadURL()
	if downloadURL == "" {
		return nil, "", errors.New("asset has no download URL")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, downloadURL, nil)
	if err != nil {
		return nil, "", errors.WithStack(err)
	}

	if p.token != "" {
		req.Header.Set("Authorization", "Bearer "+p.token)
	}

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, "", errors.WithStack(err)
	}

	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()

		return nil, "", errors.Newf("download failed: HTTP %d", resp.StatusCode)
	}

	return resp.Body, "", nil
}

// resolveVersion returns the version string from whichever source is configured.
func (p *DirectReleaseProvider) resolveVersion(ctx context.Context) (string, error) {
	if p.pinnedVersion != "" {
		return p.pinnedVersion, nil
	}

	if p.versionURL != "" {
		return fetchVersion(ctx, p.httpClient, p.versionURL, p.versionFormat, p.versionKey)
	}

	return "", release.ErrVersionUnknown
}

// syntheticRelease builds a release.Release whose single asset URL is the
// expanded url_template for the current platform.
func (p *DirectReleaseProvider) syntheticRelease(version string) release.Release {
	url := p.expandTemplate(p.urlTemplate, version)

	return &directRelease{
		tagName:     version,
		downloadURL: url,
	}
}

// expandTemplate replaces all known placeholders in tmpl.
func (p *DirectReleaseProvider) expandTemplate(tmpl, version string) string {
	osTitle := cases.Title(language.English).String(runtime.GOOS)

	arch := runtime.GOARCH
	if arch == "amd64" {
		arch = "x86_64"
	}

	bare := strings.TrimPrefix(version, "v")

	toolName := p.toolName
	if toolName == "" {
		toolName = "tool"
	}

	r := strings.NewReplacer(
		"{version}", version,
		"{version_bare}", bare,
		"{os}", osTitle,
		"{arch}", arch,
		"{tool}", toolName,
		"{ext}", "tar.gz",
	)

	return r.Replace(tmpl)
}

func resolveToken(cfg config.Containable) string {
	if cfg != nil {
		sub := cfg.Sub("direct")
		if sub != nil {
			if t := sub.GetString("token"); t != "" {
				return t
			}
		}
	}

	return os.Getenv(directTokenEnv)
}

// Package gitea provides a release.Provider implementation for Gitea and
// Forgejo instances, including Codeberg (codeberg.org).
package gitea

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"

	"github.com/cockroachdb/errors"

	"github.com/phpboyscout/go-tool-base/pkg/config"
	gtbhttp "github.com/phpboyscout/go-tool-base/pkg/http"
	"github.com/phpboyscout/go-tool-base/pkg/vcs"
	"github.com/phpboyscout/go-tool-base/pkg/vcs/release"
)

const (
	defaultAPIVersion = "v1"
	// CodebergHost is the base URL for Codeberg, the public Forgejo instance.
	CodebergHost     = "https://codeberg.org"
	codebergTokenEnv = "CODEBERG_TOKEN"
	giteaTokenEnv    = "GITEA_TOKEN"
)

// giteaReleaseJSON is the JSON shape returned by the Gitea releases API.
type giteaReleaseJSON struct {
	ID      int64            `json:"id"`
	Name    string           `json:"name"`
	TagName string           `json:"tag_name"`
	Body    string           `json:"body"`
	Draft   bool             `json:"draft"`
	Assets  []giteaAssetJSON `json:"assets"`
}

type giteaAssetJSON struct {
	ID                 int64  `json:"id"`
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

// giteaRelease implements release.Release.
type giteaRelease struct {
	raw giteaReleaseJSON
}

func (r *giteaRelease) GetName() string    { return r.raw.Name }
func (r *giteaRelease) GetTagName() string { return r.raw.TagName }
func (r *giteaRelease) GetBody() string    { return r.raw.Body }
func (r *giteaRelease) GetDraft() bool     { return r.raw.Draft }

func (r *giteaRelease) GetAssets() []release.ReleaseAsset {
	assets := make([]release.ReleaseAsset, len(r.raw.Assets))
	for i := range r.raw.Assets {
		assets[i] = &giteaAsset{raw: r.raw.Assets[i]}
	}

	return assets
}

// giteaAsset implements release.ReleaseAsset.
type giteaAsset struct {
	raw giteaAssetJSON
}

func (a *giteaAsset) GetID() int64                  { return a.raw.ID }
func (a *giteaAsset) GetName() string               { return a.raw.Name }
func (a *giteaAsset) GetBrowserDownloadURL() string { return a.raw.BrowserDownloadURL }

// GiteaReleaseProvider implements release.Provider for Gitea/Forgejo/Codeberg.
type GiteaReleaseProvider struct {
	baseURL    string
	token      string
	httpClient *http.Client
}

// NewReleaseProvider constructs a GiteaReleaseProvider.
//
// The host is resolved in order:
//  1. cfg key "url.api" (allows runtime override)
//  2. src.Host
//
// tokenFallbackEnv is the well-known environment variable for this instance
// type (e.g. GITEA_TOKEN or CODEBERG_TOKEN).
func NewReleaseProvider(src release.ReleaseSourceConfig, cfg config.Containable, tokenFallbackEnv string) (*GiteaReleaseProvider, error) {
	host := src.Host
	if cfg != nil && cfg.GetString("url.api") != "" {
		host = cfg.GetString("url.api")
	}

	if host == "" {
		return nil, errors.WithHint(
			errors.New("gitea host is required"),
			"Set ReleaseSource.Host to the base URL of your Gitea/Forgejo instance (e.g. https://git.example.com).",
		)
	}

	apiVersion := defaultAPIVersion
	if v, ok := src.Params["api_version"]; ok && v != "" {
		apiVersion = v
	}

	baseURL := fmt.Sprintf("%s/api/%s", host, apiVersion)

	// Token resolution: config subtree first, then well-known env var.
	var cfgSub config.Containable
	if cfg != nil {
		cfgSub = cfg.Sub("gitea")
	}

	token := vcs.ResolveToken(cfgSub, tokenFallbackEnv)

	return &GiteaReleaseProvider{
		baseURL:    baseURL,
		token:      token,
		httpClient: gtbhttp.NewClient(),
	}, nil
}

func (p *GiteaReleaseProvider) GetLatestRelease(ctx context.Context, owner, repo string) (release.Release, error) {
	// Gitea has no dedicated "latest release" endpoint — take the first result
	// from the releases list (sorted newest-first by default).
	rels, err := p.listReleasesRaw(ctx, owner, repo, 1)
	if err != nil {
		return nil, err
	}

	if len(rels) == 0 {
		return nil, errors.New("no releases found")
	}

	return &giteaRelease{raw: rels[0]}, nil
}

func (p *GiteaReleaseProvider) GetReleaseByTag(ctx context.Context, owner, repo, tag string) (release.Release, error) {
	endpoint := fmt.Sprintf("%s/repos/%s/%s/releases/tags/%s",
		p.baseURL,
		url.PathEscape(owner),
		url.PathEscape(repo),
		url.PathEscape(tag),
	)

	var rel giteaReleaseJSON
	if err := p.getJSON(ctx, endpoint, &rel); err != nil {
		return nil, errors.Wrapf(err, "getting release by tag %q", tag)
	}

	return &giteaRelease{raw: rel}, nil
}

func (p *GiteaReleaseProvider) ListReleases(ctx context.Context, owner, repo string, limit int) ([]release.Release, error) {
	raw, err := p.listReleasesRaw(ctx, owner, repo, limit)
	if err != nil {
		return nil, err
	}

	result := make([]release.Release, len(raw))
	for i := range raw {
		result[i] = &giteaRelease{raw: raw[i]}
	}

	return result, nil
}

func (p *GiteaReleaseProvider) DownloadReleaseAsset(ctx context.Context, _, _ string, asset release.ReleaseAsset) (io.ReadCloser, string, error) {
	downloadURL := asset.GetBrowserDownloadURL()
	if downloadURL == "" {
		return nil, "", errors.New("asset has no download URL")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, downloadURL, nil)
	if err != nil {
		return nil, "", errors.WithStack(err)
	}

	if p.token != "" {
		req.Header.Set("Authorization", "token "+p.token)
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

// listReleasesRaw fetches up to limit releases from the Gitea API.
func (p *GiteaReleaseProvider) listReleasesRaw(ctx context.Context, owner, repo string, limit int) ([]giteaReleaseJSON, error) {
	endpoint := fmt.Sprintf("%s/repos/%s/%s/releases?limit=%d",
		p.baseURL,
		url.PathEscape(owner),
		url.PathEscape(repo),
		limit,
	)

	var rels []giteaReleaseJSON
	if err := p.getJSON(ctx, endpoint, &rels); err != nil {
		return nil, errors.Wrap(err, "listing releases")
	}

	return rels, nil
}

// getJSON performs an authenticated GET and JSON-decodes the response into dest.
func (p *GiteaReleaseProvider) getJSON(ctx context.Context, endpoint string, dest any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return errors.WithStack(err)
	}

	if p.token != "" {
		req.Header.Set("Authorization", "token "+p.token)
	}

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return errors.WithStack(err)
	}

	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return errors.Newf("gitea API returned HTTP %d for %s", resp.StatusCode, endpoint)
	}

	if err := json.NewDecoder(resp.Body).Decode(dest); err != nil {
		return errors.Wrap(err, "decoding gitea API response")
	}

	return nil
}

// Package bitbucket provides a release.Provider implementation for Bitbucket
// Cloud using the Downloads API. Bitbucket has no native "Releases" concept;
// version information is inferred from asset filenames using a configurable
// regular expression.
package bitbucket

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"sort"
	"time"

	"github.com/cockroachdb/errors"

	"github.com/phpboyscout/go-tool-base/pkg/config"
	gtbhttp "github.com/phpboyscout/go-tool-base/pkg/http"
	"github.com/phpboyscout/go-tool-base/pkg/vcs/release"
)

const (
	bitbucketAPIBase = "https://api.bitbucket.org/2.0"

	// defaultFilenamePattern matches GoReleaser-style asset names, optionally
	// containing a version segment: tool_v1.2.3_Linux_x86_64.tar.gz or
	// tool_Linux_x86_64.tar.gz.
	// Capture group 1: optional version (e.g. "v1.2.3").
	// Arch segment allows underscores to match x86_64.
	defaultFilenamePattern = `^.+?(?:_(v?\d+\.\d+\.\d+[^_]*))?_([A-Za-z]+)_([A-Za-z0-9_]+)\.tar\.gz$`
)

// bbDownloadJSON is the shape of a single item from the Bitbucket Downloads API.
type bbDownloadJSON struct {
	Name      string    `json:"name"`
	Links     bbLinks   `json:"links"`
	CreatedOn time.Time `json:"created_on"`
	Size      int64     `json:"size"`
}

type bbLinks struct {
	Self bbHref `json:"self"`
}

type bbHref struct {
	Href string `json:"href"`
}

// bbDownloadsPage is the paginated response from the downloads endpoint.
type bbDownloadsPage struct {
	Values []bbDownloadJSON `json:"values"`
	Next   string           `json:"next"`
}

// bitbucketRelease implements release.Release.
// Because Bitbucket Downloads has no release metadata, we synthesise it from
// the matched asset set.
type bitbucketRelease struct {
	tagName string
	assets  []release.ReleaseAsset
}

func (r *bitbucketRelease) GetName() string                   { return r.tagName }
func (r *bitbucketRelease) GetTagName() string                { return r.tagName }
func (r *bitbucketRelease) GetBody() string                   { return "" }
func (r *bitbucketRelease) GetDraft() bool                    { return false }
func (r *bitbucketRelease) GetAssets() []release.ReleaseAsset { return r.assets }

// bitbucketAsset implements release.ReleaseAsset.
type bitbucketAsset struct {
	name string
	url  string
}

func (a *bitbucketAsset) GetID() int64                  { return 0 }
func (a *bitbucketAsset) GetName() string               { return a.name }
func (a *bitbucketAsset) GetBrowserDownloadURL() string { return a.url }

// BitbucketReleaseProvider implements release.Provider for Bitbucket Cloud.
type BitbucketReleaseProvider struct {
	apiBase         string
	username        string
	appPassword     string
	filenamePattern *regexp.Regexp
	httpClient      *http.Client
}

// NewReleaseProvider constructs a BitbucketReleaseProvider.
//
// Credentials are resolved in order:
//  1. cfg keys "username" and "app_password"
//  2. BITBUCKET_USERNAME / BITBUCKET_APP_PASSWORD environment variables
//
// The filename regex can be overridden via src.Params["filename_pattern"].
func NewReleaseProvider(src release.ReleaseSourceConfig, cfg config.Containable) (*BitbucketReleaseProvider, error) {
	username, appPassword := resolveCredentials(cfg)

	if src.Private && (username == "" || appPassword == "") {
		return nil, errors.WithHint(
			errors.New("bitbucket credentials required for private repository"),
			"Set BITBUCKET_USERNAME and BITBUCKET_APP_PASSWORD environment variables, "+
				"or configure bitbucket.username and bitbucket.app_password.",
		)
	}

	patternStr := defaultFilenamePattern
	if p, ok := src.Params["filename_pattern"]; ok && p != "" {
		patternStr = p
	}

	re, err := regexp.Compile(patternStr)
	if err != nil {
		return nil, errors.WithHintf(err, "filename_pattern %q is not a valid regular expression", patternStr)
	}

	return &BitbucketReleaseProvider{
		apiBase:         bitbucketAPIBase,
		username:        username,
		appPassword:     appPassword,
		filenamePattern: re,
		httpClient:      gtbhttp.NewClient(),
	}, nil
}

// SetAPIBase overrides the Bitbucket API base URL. Intended for testing only.
func (p *BitbucketReleaseProvider) SetAPIBase(base string) {
	p.apiBase = base
}

// GetLatestRelease returns a synthetic release built from the most recently
// uploaded Downloads that match the filename pattern.
func (p *BitbucketReleaseProvider) GetLatestRelease(ctx context.Context, owner, repo string) (release.Release, error) {
	workspace := owner

	downloads, err := p.fetchAllDownloads(ctx, workspace, repo)
	if err != nil {
		return nil, err
	}

	// Sort by creation date descending — newest first.
	sort.Slice(downloads, func(i, j int) bool {
		return downloads[i].CreatedOn.After(downloads[j].CreatedOn)
	})

	// Find the version from the most recently uploaded matching file. All files
	// sharing that version string (or upload timestamp) form the release assets.
	version, assets := p.matchAssets(downloads)
	if len(assets) == 0 {
		return nil, errors.New("no matching downloads found in Bitbucket repository")
	}

	return &bitbucketRelease{tagName: version, assets: assets}, nil
}

// GetReleaseByTag is not supported for Bitbucket Downloads.
func (p *BitbucketReleaseProvider) GetReleaseByTag(_ context.Context, _, _, _ string) (release.Release, error) {
	return nil, errors.WithHint(
		release.ErrNotSupported,
		"Bitbucket Downloads has no versioned releases. Use GetLatestRelease instead.",
	)
}

// ListReleases is not supported for Bitbucket Downloads.
func (p *BitbucketReleaseProvider) ListReleases(_ context.Context, _, _ string, _ int) ([]release.Release, error) {
	return nil, errors.WithHint(
		release.ErrNotSupported,
		"Bitbucket Downloads has no versioned releases. Use GetLatestRelease instead.",
	)
}

// DownloadReleaseAsset streams the asset at its BrowserDownloadURL.
func (p *BitbucketReleaseProvider) DownloadReleaseAsset(ctx context.Context, _, _ string, asset release.ReleaseAsset) (io.ReadCloser, string, error) {
	downloadURL := asset.GetBrowserDownloadURL()
	if downloadURL == "" {
		return nil, "", errors.New("asset has no download URL")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, downloadURL, nil)
	if err != nil {
		return nil, "", errors.WithStack(err)
	}

	if p.username != "" {
		req.SetBasicAuth(p.username, p.appPassword)
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

// fetchAllDownloads pages through the Bitbucket Downloads API and returns all entries.
func (p *BitbucketReleaseProvider) fetchAllDownloads(ctx context.Context, workspace, repo string) ([]bbDownloadJSON, error) {
	endpoint := fmt.Sprintf("%s/repositories/%s/%s/downloads", p.apiBase, workspace, repo)

	var all []bbDownloadJSON

	for endpoint != "" {
		page, err := p.fetchDownloadsPage(ctx, endpoint)
		if err != nil {
			return nil, err
		}

		all = append(all, page.Values...)
		endpoint = page.Next
	}

	return all, nil
}

func (p *BitbucketReleaseProvider) fetchDownloadsPage(ctx context.Context, endpoint string) (*bbDownloadsPage, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, errors.WithStack(err)
	}

	if p.username != "" {
		req.SetBasicAuth(p.username, p.appPassword)
	}

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, errors.WithStack(err)
	}

	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, errors.Newf("bitbucket API returned HTTP %d", resp.StatusCode)
	}

	var page bbDownloadsPage
	if err := json.NewDecoder(resp.Body).Decode(&page); err != nil {
		return nil, errors.Wrap(err, "decoding bitbucket downloads response")
	}

	return &page, nil
}

// matchAssets applies the filename regex to the downloads list.
// It extracts the version from the first matching entry; subsequent entries
// that share the same version (or, when no version is in the name, the same
// timestamp bucket) are included as assets of that release.
func (p *BitbucketReleaseProvider) matchAssets(downloads []bbDownloadJSON) (string, []release.ReleaseAsset) {
	var (
		version    string
		versionSet bool
		assets     []release.ReleaseAsset
	)

	for _, dl := range downloads {
		m := p.filenamePattern.FindStringSubmatch(dl.Name)
		if m == nil {
			continue
		}

		// Capture group 1 is the optional version segment (may be empty string).
		extracted := ""
		if len(m) > 1 {
			extracted = m[1]
		}

		if !versionSet {
			// First match determines the version for this "release".
			if extracted != "" {
				version = extracted
			} else {
				// No version in filename — use ISO 8601 creation timestamp.
				version = dl.CreatedOn.UTC().Format(time.RFC3339)
			}

			versionSet = true
		}

		// Only include assets that belong to the same version.
		assetVersion := extracted
		if assetVersion == "" {
			assetVersion = dl.CreatedOn.UTC().Format(time.RFC3339)
		}

		if assetVersion == version {
			assets = append(assets, &bitbucketAsset{
				name: dl.Name,
				url:  dl.Links.Self.Href,
			})
		}
	}

	return version, assets
}

// resolveCredentials reads Bitbucket credentials from config then env vars.
func resolveCredentials(cfg config.Containable) (username, appPassword string) {
	if cfg != nil {
		sub := cfg.Sub("bitbucket")
		if sub != nil {
			username = sub.GetString("username")
			appPassword = sub.GetString("app_password")
		}
	}

	if username == "" {
		username = os.Getenv("BITBUCKET_USERNAME")
	}

	if appPassword == "" {
		appPassword = os.Getenv("BITBUCKET_APP_PASSWORD")
	}

	return username, appPassword
}

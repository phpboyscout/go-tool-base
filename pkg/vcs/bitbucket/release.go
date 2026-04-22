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
	"strings"
	"time"

	"github.com/cockroachdb/errors"

	"github.com/phpboyscout/go-tool-base/pkg/config"
	"github.com/phpboyscout/go-tool-base/pkg/credentials"
	gtbhttp "github.com/phpboyscout/go-tool-base/pkg/http"
	"github.com/phpboyscout/go-tool-base/pkg/regexutil"
	"github.com/phpboyscout/go-tool-base/pkg/vcs/release"
)

// bitbucketKeychainTimeout is the upper bound on a single keychain
// retrieval during provider construction. The release.Register
// factory interface does not propagate a caller context, so we
// apply a local guard here — a misbehaving remote-store backend
// (Vault, SSM) cannot stall startup indefinitely. OS-keychain
// backends return well under this bound.
const bitbucketKeychainTimeout = 5 * time.Second

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
// Credentials are resolved by [resolveCredentials]; see its doc for the
// full precedence chain. A corrupt keychain blob aborts construction
// rather than silently falling through to the legacy literal step.
//
// The filename regex can be overridden via src.Params["filename_pattern"].
func NewReleaseProvider(src release.ReleaseSourceConfig, cfg config.Containable) (*BitbucketReleaseProvider, error) {
	ctx, cancel := context.WithTimeout(context.Background(), bitbucketKeychainTimeout)
	defer cancel()

	username, appPassword, err := resolveCredentials(ctx, cfg)
	if err != nil {
		return nil, err
	}

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

	// Config-supplied pattern — bound compile time against ReDoS. Closes
	// H-2 from docs/development/reports/security-audit-2026-04-17.md.
	re, err := regexutil.CompileBoundedTimeout(patternStr, regexutil.DefaultCompileTimeout)
	if err != nil {
		return nil, errors.WithHintf(err, "filename_pattern is not a valid regular expression (length=%d)", len(patternStr))
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

// bitbucketKeychainBlob is the JSON shape stored in the OS keychain
// when credentials are persisted via `bitbucket.keychain`. A single
// entry carries the dual-credential pair so the user configures one
// keychain item instead of two.
type bitbucketKeychainBlob struct {
	Username    string `json:"username"`
	AppPassword string `json:"app_password"`
}

// resolveCredentials reads Bitbucket credentials from config then
// env vars. Credential-storage precedence for each field:
//
//  1. bitbucket.<field>.env — NAME of an env var holding the value
//     (preferred; keeps the secret out of the config file)
//  2. bitbucket.keychain    — shared "<service>/<account>" reference
//     to an OS-keychain entry whose value is a JSON blob carrying
//     both fields. Only active when pkg/credentials/keychain is
//     imported (or a custom Backend is registered). A corrupt or
//     incomplete blob aborts resolution rather than falling through
//     (per R3 of the hardening spec: a broken keychain item must be
//     surfaced, not silently masked by a stale literal).
//  3. bitbucket.<field>     — literal value in config (legacy).
//     Viper's AutomaticEnv + tool prefix makes this step also
//     pick up <PREFIX>_BITBUCKET_<FIELD> style env vars — so
//     `MYTOOL_BITBUCKET_USERNAME=alice` works without any YAML.
//  4. BITBUCKET_<FIELD>     — well-known unprefixed ecosystem env
//     var; final fallback when the tool's prefix does not match and
//     the user still wants the upstream convention.
//
// Bitbucket's dual-credential model means each field (username,
// app_password) is resolved independently. Partial configuration
// (e.g. username via env-var, app_password from keychain) is
// supported and occasionally useful during rotation.
//
// See docs/development/specs/2026-04-02-credential-storage-hardening.md.
func resolveCredentials(ctx context.Context, cfg config.Containable) (username, appPassword string, err error) {
	blob, err := loadBitbucketKeychain(ctx, cfg)
	if err != nil {
		return "", "", err
	}

	username = resolveBitbucketField(cfg, "username", "BITBUCKET_USERNAME", blob.Username)
	appPassword = resolveBitbucketField(cfg, "app_password", "BITBUCKET_APP_PASSWORD", blob.AppPassword)

	return username, appPassword, nil
}

// resolveBitbucketField implements the four-step precedence for a
// single Bitbucket credential field. keychainValue is the field as
// decoded from the shared keychain blob (already loaded once per
// [resolveCredentials] invocation); pass "" when the blob is absent
// or the field was empty in it.
func resolveBitbucketField(cfg config.Containable, field, fallbackEnv, keychainValue string) string {
	if v := bitbucketFieldFromConfig(cfg, field, keychainValue); v != "" {
		return v
	}

	return strings.TrimSpace(os.Getenv(fallbackEnv))
}

// bitbucketFieldFromConfig returns the configured value for a single
// Bitbucket credential field. cfg.Sub("bitbucket") preserves the
// root's env-binding configuration (see pkg/config.Container.Sub),
// so sub-scoped lookups pick up prefixed env vars like
// <TOOL>_BITBUCKET_USERNAME without a round-trip through the full
// dot-path. keychainValue is consulted between the env-ref step and
// the literal step. Returns empty string when nothing is configured.
func bitbucketFieldFromConfig(cfg config.Containable, field, keychainValue string) string {
	if cfg == nil {
		return strings.TrimSpace(keychainValue)
	}

	sub := cfg.Sub("bitbucket")
	if sub == nil {
		return strings.TrimSpace(keychainValue)
	}

	if name := strings.TrimSpace(sub.GetString(field + ".env")); name != "" {
		if v := strings.TrimSpace(os.Getenv(name)); v != "" {
			return v
		}
	}

	if v := strings.TrimSpace(keychainValue); v != "" {
		return v
	}

	return strings.TrimSpace(sub.GetString(field))
}

// loadBitbucketKeychain retrieves and decodes the shared JSON blob
// referenced by bitbucket.keychain, returning a zero blob when the
// key is unset or the keychain backend is compiled out / empty.
//
// Only JSON-decode failures and incomplete blobs abort — generic
// retrieval errors (unavailable backend, missing entry) fall through
// silently so a configured-but-unreachable keychain cannot mask a
// valid literal or env-var fallback further down the chain.
func loadBitbucketKeychain(ctx context.Context, cfg config.Containable) (bitbucketKeychainBlob, error) {
	var blob bitbucketKeychainBlob

	if cfg == nil {
		return blob, nil
	}

	sub := cfg.Sub("bitbucket")
	if sub == nil {
		return blob, nil
	}

	ref := strings.TrimSpace(sub.GetString("keychain"))
	if ref == "" {
		return blob, nil
	}

	i := strings.Index(ref, "/")
	if i <= 0 || i == len(ref)-1 {
		return blob, nil
	}

	service, account := ref[:i], ref[i+1:]

	// Any retrieval error (stub backend, missing entry, backend
	// unreachable) falls through to the next resolution step rather
	// than aborting — only JSON-structure failures below abort, per
	// R3 in the hardening spec.
	secret, _ := credentials.Retrieve(ctx, service, account)
	if secret == "" {
		return blob, nil
	}

	if err := json.Unmarshal([]byte(secret), &blob); err != nil {
		return bitbucketKeychainBlob{}, errors.WithHint(
			errors.New("bitbucket keychain entry is not valid JSON"),
			"re-run the setup wizard for bitbucket to repair the keychain entry",
		)
	}

	if strings.TrimSpace(blob.Username) == "" || strings.TrimSpace(blob.AppPassword) == "" {
		return bitbucketKeychainBlob{}, errors.WithHint(
			errors.New("bitbucket keychain entry is missing username or app_password"),
			"re-run the setup wizard for bitbucket to repair the keychain entry",
		)
	}

	return blob, nil
}

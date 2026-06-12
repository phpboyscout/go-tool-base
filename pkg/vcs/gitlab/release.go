package gitlab

import (
	"context"
	"io"
	"net/http"
	neturl "net/url"

	"github.com/cockroachdb/errors"
	gitlab "gitlab.com/gitlab-org/api/client-go"

	"gitlab.com/phpboyscout/go-tool-base/pkg/config"
	gtbhttp "gitlab.com/phpboyscout/go-tool-base/pkg/http"
	"gitlab.com/phpboyscout/go-tool-base/pkg/vcs"
	"gitlab.com/phpboyscout/go-tool-base/pkg/vcs/release"
)

// gitlabRelease implements release.Release.
type gitlabRelease struct {
	release *gitlab.Release
}

func (r *gitlabRelease) GetName() string {
	return r.release.Name
}

func (r *gitlabRelease) GetTagName() string {
	return r.release.TagName
}

func (r *gitlabRelease) GetBody() string {
	return r.release.Description
}

func (r *gitlabRelease) GetDraft() bool {
	// Gitlab doesn't treat draft releases the same way, assume false
	return false
}

func (r *gitlabRelease) GetAssets() []release.ReleaseAsset {
	if len(r.release.Assets.Links) == 0 {
		return nil
	}

	assets := make([]release.ReleaseAsset, len(r.release.Assets.Links))
	for i, a := range r.release.Assets.Links {
		assets[i] = &gitlabAsset{link: a}
	}

	return assets
}

// gitlabAsset implements release.ReleaseAsset.
type gitlabAsset struct {
	link *gitlab.ReleaseLink
}

func (a *gitlabAsset) GetID() int64 {
	// Let's use the DB ID or an extracted int from the link
	return a.link.ID
}

func (a *gitlabAsset) GetName() string {
	return a.link.Name
}

func (a *gitlabAsset) GetBrowserDownloadURL() string {
	return a.link.URL
}

// GitLabReleaseProvider implements release.Provider.
type GitLabReleaseProvider struct {
	client *gitlab.Client
	token  string
	// apiHost is the host (host:port) of the configured GitLab instance. The
	// PRIVATE-TOKEN credential is only attached to asset downloads whose
	// URL host matches it, so a release author cannot exfiltrate the token
	// by pointing an asset link at a host they control.
	apiHost string
}

// NewReleaseProvider creates a new release provider for GitLab.
// defaultGitLabAPI is the API base URL for the public gitlab.com host.
const defaultGitLabAPI = "https://gitlab.com/api/v4"

// NewReleaseProvider builds a GitLab release provider. It is config-less
// by design: a public repository needs only the ReleaseSource (Host
// selects the instance; empty means gitlab.com). cfg is the optional
// `gitlab` config subtree (nil when no such section exists); it supplies
// a token (for private repos) and an explicit `url.api` override. When
// absent, the token falls back to the GITLAB_TOKEN env var and the base
// URL is derived from src.Host.
func NewReleaseProvider(src release.ReleaseSourceConfig, cfg config.Containable) (release.Provider, error) {
	baseURL := defaultGitLabAPI
	if src.Host != "" {
		baseURL = "https://" + src.Host + "/api/v4"
	}

	if cfg != nil {
		if override := cfg.GetString("url.api"); override != "" {
			baseURL = override
		}
	}

	// Token is optional: public repositories work unauthenticated. A nil
	// cfg resolves the token from the GITLAB_TOKEN env var only.
	token := vcs.ResolveToken(cfg, "GITLAB_TOKEN")

	client, err := gitlab.NewClient(token,
		gitlab.WithBaseURL(baseURL),
		gitlab.WithHTTPClient(gtbhttp.NewClient()),
	)
	if err != nil {
		return nil, errors.WithStack(err)
	}

	var apiHost string
	if u, perr := neturl.Parse(baseURL); perr == nil {
		apiHost = u.Host
	}

	return &GitLabReleaseProvider{
		client:  client,
		token:   token,
		apiHost: apiHost,
	}, nil
}

func (p *GitLabReleaseProvider) GetLatestRelease(ctx context.Context, owner, repo string) (release.Release, error) {
	projectPath := owner + "/" + repo

	rels, resp, err := p.client.Releases.ListReleases(projectPath, &gitlab.ListReleasesOptions{
		ListOptions: gitlab.ListOptions{PerPage: 1},
	}, gitlab.WithContext(ctx))
	if err != nil {
		return nil, errors.WithStack(err)
	}

	if resp.StatusCode == http.StatusNotFound || len(rels) == 0 {
		return nil, errors.New("no releases found")
	}

	return &gitlabRelease{release: rels[0]}, nil
}

func (p *GitLabReleaseProvider) GetReleaseByTag(ctx context.Context, owner, repo, tag string) (release.Release, error) {
	projectPath := owner + "/" + repo

	rel, _, err := p.client.Releases.GetRelease(projectPath, tag, gitlab.WithContext(ctx))
	if err != nil {
		return nil, errors.WithStack(err)
	}

	return &gitlabRelease{release: rel}, nil
}

func (p *GitLabReleaseProvider) ListReleases(ctx context.Context, owner, repo string, limit int) ([]release.Release, error) {
	projectPath := owner + "/" + repo

	rels, _, err := p.client.Releases.ListReleases(projectPath, &gitlab.ListReleasesOptions{
		ListOptions: gitlab.ListOptions{PerPage: int64(limit)},
	}, gitlab.WithContext(ctx))
	if err != nil {
		return nil, errors.WithStack(err)
	}

	result := make([]release.Release, len(rels))
	for i, r := range rels {
		result[i] = &gitlabRelease{release: r}
	}

	return result, nil
}

// assetHostTrusted reports whether rawURL's host matches the configured
// GitLab instance host. It fails closed: an unparseable URL or an
// unconfigured instance host is never trusted.
func (p *GitLabReleaseProvider) assetHostTrusted(rawURL string) bool {
	if p.apiHost == "" {
		return false
	}

	u, err := neturl.Parse(rawURL)
	if err != nil {
		return false
	}

	return u.Host == p.apiHost
}

// DownloadReleaseAsset is more complex for GitLab.
func (p *GitLabReleaseProvider) DownloadReleaseAsset(ctx context.Context, owner, repo string, asset release.ReleaseAsset) (io.ReadCloser, string, error) {
	url := asset.GetBrowserDownloadURL()
	if url == "" {
		return nil, "", errors.New("asset URL is empty")
	}

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, "", errors.WithStack(err)
	}

	// Only attach the credential when the asset is hosted on the configured
	// GitLab instance. Asset link URLs are release-author-controlled, so
	// sending the token to an arbitrary host would leak it off-instance.
	if p.token != "" && p.assetHostTrusted(url) {
		req.Header.Set("PRIVATE-TOKEN", p.token)
	}

	resp, err := gtbhttp.NewClient().Do(req)
	if err != nil {
		return nil, "", errors.WithStack(err)
	}

	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()

		return nil, "", errors.Newf("failed to download asset: status %d", resp.StatusCode)
	}

	return resp.Body, "", nil
}

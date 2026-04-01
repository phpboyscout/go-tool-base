package github

import (
	"context"
	"io"

	"github.com/cockroachdb/errors"
	"github.com/google/go-github/v80/github"

	gtbhttp "github.com/phpboyscout/go-tool-base/pkg/http"
	"github.com/phpboyscout/go-tool-base/pkg/vcs/release"
)

// githubRelease implements release.Release.
type githubRelease struct {
	release *github.RepositoryRelease
}

func (r *githubRelease) GetName() string {
	if r.release.Name == nil {
		return ""
	}

	return *r.release.Name
}

func (r *githubRelease) GetTagName() string {
	if r.release.TagName == nil {
		return ""
	}

	return *r.release.TagName
}

func (r *githubRelease) GetBody() string {
	if r.release.Body == nil {
		return ""
	}

	return *r.release.Body
}

func (r *githubRelease) GetDraft() bool {
	if r.release.Draft == nil {
		return false
	}

	return *r.release.Draft
}

func (r *githubRelease) GetAssets() []release.ReleaseAsset {
	assets := make([]release.ReleaseAsset, len(r.release.Assets))
	for i, a := range r.release.Assets {
		assets[i] = &githubAsset{asset: a}
	}

	return assets
}

// githubAsset implements release.ReleaseAsset.
type githubAsset struct {
	asset *github.ReleaseAsset
}

func (a *githubAsset) GetID() int64 {
	if a.asset.ID == nil {
		return 0
	}

	return *a.asset.ID
}

func (a *githubAsset) GetName() string {
	if a.asset.Name == nil {
		return ""
	}

	return *a.asset.Name
}

func (a *githubAsset) GetBrowserDownloadURL() string {
	if a.asset.BrowserDownloadURL == nil {
		return ""
	}

	return *a.asset.BrowserDownloadURL
}

// GitHubReleaseProvider implements release.Provider.
type GitHubReleaseProvider struct {
	client *github.Client
}

// NewReleaseProvider creates a release.Provider backed by the GitHub Releases API.
func NewReleaseProvider(client GitHubClient) release.Provider {
	return &GitHubReleaseProvider{
		client: client.GetClient(),
	}
}

func (p *GitHubReleaseProvider) GetLatestRelease(ctx context.Context, owner, repo string) (release.Release, error) {
	rel, _, err := p.client.Repositories.GetLatestRelease(ctx, owner, repo)
	if err != nil {
		return nil, errors.WithStack(err)
	}

	return &githubRelease{release: rel}, nil
}

func (p *GitHubReleaseProvider) GetReleaseByTag(ctx context.Context, owner, repo, tag string) (release.Release, error) {
	rel, _, err := p.client.Repositories.GetReleaseByTag(ctx, owner, repo, tag)
	if err != nil {
		return nil, errors.WithStack(err)
	}

	return &githubRelease{release: rel}, nil
}

func (p *GitHubReleaseProvider) ListReleases(ctx context.Context, owner, repo string, limit int) ([]release.Release, error) {
	opts := &github.ListOptions{PerPage: limit}

	rels, _, err := p.client.Repositories.ListReleases(ctx, owner, repo, opts)
	if err != nil {
		return nil, errors.WithStack(err)
	}

	result := make([]release.Release, len(rels))
	for i, r := range rels {
		result[i] = &githubRelease{release: r}
	}

	return result, nil
}

func (p *GitHubReleaseProvider) DownloadReleaseAsset(ctx context.Context, owner, repo string, asset release.ReleaseAsset) (io.ReadCloser, string, error) {
	rc, redirectURL, err := p.client.Repositories.DownloadReleaseAsset(ctx, owner, repo, asset.GetID(), gtbhttp.NewClient())
	if err != nil {
		return nil, "", errors.WithStack(err)
	}

	return rc, redirectURL, nil
}

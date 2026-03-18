package github

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"

	"github.com/cockroachdb/errors"
	"github.com/google/go-github/v80/github"
	"github.com/spf13/afero"

	"github.com/phpboyscout/gtb/pkg/config"
)

const (
	// pullRequestsPerPage is the number of pull requests to fetch per API page.
	pullRequestsPerPage = 300
	// releasesPerPage is the number of releases to fetch per API page.
	releasesPerPage = 100
)

var (
	ErrNoPullRequestFound = errors.New("no pull request found")
	ErrRepoExists         = errors.New("repository already exists")
)

type GitHubClient interface {
	GetClient() *github.Client
	CreatePullRequest(ctx context.Context, owner string, repo string, pull *github.NewPullRequest) (*github.PullRequest, error)
	GetPullRequestByBranch(ctx context.Context, owner, repo, branch, state string) (*github.PullRequest, error)
	AddLabelsToPullRequest(ctx context.Context, owner, repo string, number int, labels []string) error
	UpdatePullRequest(ctx context.Context, owner, repo string, number int, pull *github.PullRequest) (*github.PullRequest, *github.Response, error)
	CreateRepo(ctx context.Context, owner, slug string) (*github.Repository, error)
	UploadKey(ctx context.Context, name string, key []byte) error
	ListReleases(ctx context.Context, owner, repo string) ([]string, error)
	GetReleaseAssets(ctx context.Context, owner, repo, tag string) ([]*github.ReleaseAsset, error)
	GetReleaseAssetID(ctx context.Context, owner, repo, tag, assetName string) (int64, error)
	DownloadAsset(ctx context.Context, owner, repo string, assetID int64) (io.ReadCloser, error)
	DownloadAssetTo(ctx context.Context, fs afero.Fs, owner, repo string, assetID int64, filePath string) error
	GetFileContents(ctx context.Context, owner, repo, path, ref string) (string, error)
}

type GHClient struct {
	Client *github.Client
	cfg    config.Containable
}

func (c *GHClient) GetClient() *github.Client {
	return c.Client
}

func (c *GHClient) CreatePullRequest(ctx context.Context, owner string, repo string, pr *github.NewPullRequest) (*github.PullRequest, error) {
	// return &github.PullRequest{}, &github.Response{}, nil
	req, _, err := c.Client.PullRequests.Create(ctx, owner, repo, pr)

	return req, err
}

func (c *GHClient) GetPullRequestByBranch(ctx context.Context, owner, repo, branch, state string) (*github.PullRequest, error) {
	listOpts := github.ListOptions{
		PerPage: pullRequestsPerPage,
	}

	prs, _, err := c.Client.PullRequests.List(ctx, owner, repo, &github.PullRequestListOptions{
		State:       state,
		Head:        fmt.Sprintf("%s:%s", owner, branch),
		Sort:        "created",
		Direction:   "desc",
		ListOptions: listOpts,
	})
	if err != nil {
		return nil, err
	}

	for _, pr := range prs {
		if pr.GetHead().GetRef() == branch {
			return pr, nil
		}
	}

	return nil, errors.WithStack(ErrNoPullRequestFound)
}

func (c *GHClient) UpdatePullRequest(ctx context.Context, owner, repo string, number int, pr *github.PullRequest) (*github.PullRequest, *github.Response, error) {
	return c.Client.PullRequests.Edit(ctx, owner, repo, number, pr)
}

func (c *GHClient) AddLabelsToPullRequest(ctx context.Context, owner, repo string, number int, labels []string) error {
	_, _, err := c.Client.Issues.AddLabelsToIssue(ctx, owner, repo, number, labels)

	return err
}

func (c *GHClient) CreateRepo(ctx context.Context, owner, slug string) (*github.Repository, error) {
	// c.Client.Repositories.Create(ctx, owner, repo)
	repo, _, err := c.Client.Repositories.Create(ctx, owner, &github.Repository{
		Name:       new(slug),
		Visibility: new("internal"),
	})

	return repo, err
}

func (c *GHClient) UploadKey(ctx context.Context, name string, key []byte) error {
	_, _, err := c.Client.Users.CreateKey(ctx, &github.Key{
		Title: new(name),
		Key:   new(string(key)),
	})

	return err
}

func (c *GHClient) ListReleases(ctx context.Context, owner, repo string) ([]string, error) {
	opt := &github.ListOptions{PerPage: releasesPerPage}

	var allReleaseTags []string

	for {
		releases, resp, err := c.Client.Repositories.ListReleases(ctx, owner, repo, opt)
		if err != nil {
			return nil, errors.WithStack(err)
		}

		for _, r := range releases {
			if r != nil && r.TagName != nil && *r.TagName != "" {
				allReleaseTags = append(allReleaseTags, *r.TagName)
			}
		}

		if resp.NextPage == 0 {
			break
		}

		opt.Page = resp.NextPage
	}

	return allReleaseTags, nil
}

func (c *GHClient) GetReleaseAssets(ctx context.Context, owner, repo, tag string) ([]*github.ReleaseAsset, error) {
	release, _, err := c.Client.Repositories.GetReleaseByTag(ctx, owner, repo, tag)
	if err != nil {
		return nil, errors.Newf("failed to get release by tag %s for repo %s/%s: %w", tag, owner, repo, err)
	}

	if release == nil {
		return nil, errors.Newf("release %s not found in repo %s/%s", tag, owner, repo)
	}

	return release.Assets, nil
}

func (c *GHClient) GetReleaseAssetID(ctx context.Context, owner, repo, tag, assetName string) (int64, error) {
	assets, err := c.GetReleaseAssets(ctx, owner, repo, tag)
	if err != nil {
		return 0, errors.Newf("failed to get release assets for %s/%s tag %s: %w", owner, repo, tag, err)
	}

	for _, asset := range assets {
		if asset.GetName() == assetName {
			return asset.GetID(), nil
		}
	}

	return 0, errors.Newf("asset named '%s' not found in release %s for repo %s/%s", assetName, tag, owner, repo)
}

func (c *GHClient) DownloadAsset(ctx context.Context, owner, repo string, assetID int64) (io.ReadCloser, error) {
	rc, _, err := c.Client.Repositories.DownloadReleaseAsset(ctx, owner, repo, assetID, http.DefaultClient)
	if err != nil {
		return nil, errors.Newf("failed to download asset %d from repo %s/%s: %w", assetID, owner, repo, err)
	}

	if rc == nil {
		// This case should ideally be covered by an error from DownloadReleaseAsset,
		// but good to have a safeguard.
		return nil, errors.Newf("received nil ReadCloser for asset %d from repo %s/%s without an error", assetID, owner, repo)
	}

	return rc, nil
}

func (c *GHClient) DownloadAssetTo(ctx context.Context, fs afero.Fs, owner, repo string, assetID int64, filePath string) error {
	rc, err := c.DownloadAsset(ctx, owner, repo, assetID)
	if err != nil {
		return err // Already wrapped by DownloadAsset
	}

	defer func() { _ = rc.Close() }()

	outFile, err := fs.Create(filePath)
	if err != nil {
		return errors.Newf("failed to create file %s on the provided filesystem: %w", filePath, err)
	}

	defer func() { _ = outFile.Close() }()

	_, err = io.Copy(outFile, rc)
	if err != nil {
		return errors.Newf("failed to write asset %d to file %s: %w", assetID, filePath, err)
	}

	return nil
}

func NewGitHubClient(cfg config.Containable) (*GHClient, error) {
	if cfg == nil {
		return nil, errors.New("github configuration is missing")
	}

	client, err := github.NewClient(nil).WithEnterpriseURLs(
		cfg.GetString("url.api"),
		cfg.GetString("url.upload"),
	)
	if err != nil {
		return nil, err
	}

	token, err := GetGitHubToken(cfg)
	if err != nil {
		return nil, err
	}

	return &GHClient{client.WithAuthToken(token), cfg}, nil
}

func GetGitHubToken(cfg config.Containable) (string, error) {
	var token string
	if cfg.Has("auth.env") {
		token = os.Getenv(cfg.GetString("auth.env"))
	}

	if cfg.Has("auth.value") {
		token = cfg.GetString("auth.value")
	}

	if token == "" {
		return token, errors.New("could not find a valid GITHUB_TOKEN, please check your configuration ")
	}

	return token, nil
}

func (c *GHClient) GetFileContents(ctx context.Context, owner, repo, path, ref string) (string, error) {
	content, _, _, err := c.Client.Repositories.GetContents(ctx, owner, repo, path, &github.RepositoryContentGetOptions{
		Ref: ref,
	})
	if err != nil {
		return "", errors.Wrap(err, "failed to get file contents from github")
	}

	if content == nil {
		return "", errors.New("received nil content from github")
	}

	return content.GetContent()
}

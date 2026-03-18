package release

import (
	"context"
	"io"
)

// Release defines the common abstraction for a software release.
type Release interface {
	GetName() string
	GetTagName() string
	GetBody() string
	GetDraft() bool
	GetAssets() []ReleaseAsset
}

// ReleaseAsset defines the common abstraction for a release asset.
type ReleaseAsset interface {
	GetID() int64
	GetName() string
	GetBrowserDownloadURL() string
}

// Provider defines the operations a release backend must support.
type Provider interface {
	GetLatestRelease(ctx context.Context, owner, repo string) (Release, error)
	GetReleaseByTag(ctx context.Context, owner, repo, tag string) (Release, error)
	ListReleases(ctx context.Context, owner, repo string, limit int) ([]Release, error)
	DownloadReleaseAsset(ctx context.Context, owner, repo string, asset ReleaseAsset) (io.ReadCloser, string, error)
}

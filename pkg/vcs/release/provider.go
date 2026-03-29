package release

import (
	"context"
	"io"

	"github.com/cockroachdb/errors"
)

var (
	// ErrProviderNotFound is returned by Lookup when no factory has been
	// registered for the requested source type.
	ErrProviderNotFound = errors.New("no release provider registered for source type")

	// ErrNotSupported is returned by provider methods that are not applicable
	// for the underlying platform (e.g. ListReleases on Bitbucket).
	ErrNotSupported = errors.New("operation not supported by this release provider")

	// ErrVersionUnknown is returned by the direct provider when neither
	// version_url nor pinned_version is configured and a version check is
	// requested.
	ErrVersionUnknown = errors.New("cannot determine latest version: configure version_url or pinned_version in Params")
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

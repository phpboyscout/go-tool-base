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

// ChecksumProvider is an OPTIONAL interface implemented by release
// providers that can fetch a checksums manifest by means other than
// a standard release-asset download. The canonical case is the
// Direct provider's `checksum_url_template` param, which composes a
// URL from a template that may point outside the release-asset
// listing entirely.
//
// The update flow does a type assertion at runtime — providers that
// do not implement this interface fall back to the default
// behaviour of locating `checksums.txt` by filename within the
// release's asset list. This keeps third-party Provider
// implementations source-compatible: they gain the feature by
// opting in, not by implementing a new required method.
type ChecksumProvider interface {
	// DownloadChecksumManifest returns the raw bytes of the checksums
	// manifest for the given release. maxBytes caps the response so
	// a hostile server cannot stream indefinitely; implementations
	// return an error wrapping [ErrNotSupported] or a provider-
	// specific size error when the response exceeds the bound.
	//
	// Returns [ErrNotSupported] when the provider is configured in a
	// way that disables checksum retrieval (e.g. the Direct
	// provider's `checksum_url_template` param is empty). The caller
	// treats [ErrNotSupported] exactly like "provider does not
	// implement this interface" — fall back to asset-by-name lookup
	// if one is available, otherwise respect the require_checksum
	// policy.
	DownloadChecksumManifest(ctx context.Context, rel Release, maxBytes int64) ([]byte, error)
}

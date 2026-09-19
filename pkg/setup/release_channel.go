package setup

import (
	"context"
	"io"

	"gitlab.com/phpboyscout/go/errors"
	"gitlab.com/phpboyscout/go/forge"
)

// ReleaseChannel is what SelfUpdater needs from wherever releases come from
// (spec 0203 D4): a forge's release objects today, a static location's
// pointer and manifests once that branch lands. It is deliberately the exact
// set of calls the updater makes and no more, with the release and asset
// values kept as forge's small interfaces so the updater's verification path
// is untouched; a non-forge branch satisfies those interfaces with its own
// values.
type ReleaseChannel interface {
	// Latest is the newest release the channel offers.
	Latest(ctx context.Context) (forge.Release, error)
	// ByTag is one named release.
	ByTag(ctx context.Context, tag string) (forge.Release, error)
	// List is up to limit releases, newest first; limit <= 0 is the channel's
	// natural first page (the forge.Provider contract).
	List(ctx context.Context, limit int) ([]forge.Release, error)
	// Download opens an asset's bytes. The string is a redirect the caller
	// refuses to follow, as the forge providers report one.
	Download(ctx context.Context, asset forge.ReleaseAsset) (io.ReadCloser, string, error)
	// Checksums is the checksums manifest for rel, bounded by maxBytes, or an
	// error wrapping forge.ErrNotSupported when the channel has no direct
	// route to it and the caller should look among the assets.
	Checksums(ctx context.Context, rel forge.Release, maxBytes int64) ([]byte, error)
	// Signature is the detached signature over the checksums manifest, with
	// the same not-supported contract.
	Signature(ctx context.Context, rel forge.Release, maxBytes int64) ([]byte, error)
}

// forgeChannel is the forge branch: a forge.Provider and the repository it
// speaks about. Every method is the call SelfUpdater made directly before
// the seam existed.
type forgeChannel struct {
	provider    forge.Provider
	owner, repo string
}

func newForgeChannel(p forge.Provider, owner, repo string) *forgeChannel {
	return &forgeChannel{provider: p, owner: owner, repo: repo}
}

func (c *forgeChannel) Latest(ctx context.Context) (forge.Release, error) {
	return c.provider.GetLatestRelease(ctx, c.owner, c.repo)
}

func (c *forgeChannel) ByTag(ctx context.Context, tag string) (forge.Release, error) {
	return c.provider.GetReleaseByTag(ctx, c.owner, c.repo, tag)
}

func (c *forgeChannel) List(ctx context.Context, limit int) ([]forge.Release, error) {
	return c.provider.ListReleases(ctx, c.owner, c.repo, limit)
}

func (c *forgeChannel) Download(ctx context.Context, asset forge.ReleaseAsset) (io.ReadCloser, string, error) {
	return c.provider.DownloadReleaseAsset(ctx, c.owner, c.repo, asset)
}

func (c *forgeChannel) Checksums(ctx context.Context, rel forge.Release, maxBytes int64) ([]byte, error) {
	var cp forge.ChecksumProvider
	if !forge.As(c.provider, &cp) {
		return nil, errors.Wrap(forge.ErrNotSupported, "release source has no checksum capability")
	}

	return cp.DownloadChecksumManifest(ctx, rel, maxBytes)
}

func (c *forgeChannel) Signature(ctx context.Context, rel forge.Release, maxBytes int64) ([]byte, error) {
	var sp forge.SignatureProvider
	if !forge.As(c.provider, &sp) {
		return nil, errors.Wrap(forge.ErrNotSupported, "release source has no signature capability")
	}

	return sp.DownloadSignature(ctx, rel, maxBytes)
}

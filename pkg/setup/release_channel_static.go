package setup

import (
	"context"
	"io"
	"net/http"
	"time"

	"gitlab.com/phpboyscout/go/errors"
	"gitlab.com/phpboyscout/go/forge"

	"gitlab.com/phpboyscout/go-tool-base/pkg/release/static"
)

// staticChannel is the static branch of ReleaseChannel (spec 0203 D4): a
// static.Channel over the tool's base URL, presenting its manifests as
// forge.Release values so the updater's selection and verification path is
// the one the forge branch uses. Nothing here imports a forge adapter.
type staticChannel struct {
	ch *static.Channel
}

func newStaticChannel(baseURL string, client *http.Client) (*staticChannel, error) {
	ch, err := static.New(baseURL, client)
	if err != nil {
		return nil, err
	}

	return &staticChannel{ch: ch}, nil
}

func (c *staticChannel) Latest(ctx context.Context) (forge.Release, error) {
	m, err := c.ch.Latest(ctx)
	if err != nil {
		return nil, err
	}

	return newStaticRelease(m), nil
}

func (c *staticChannel) ByTag(ctx context.Context, tag string) (forge.Release, error) {
	m, err := c.ch.ByTag(ctx, tag)
	if err != nil {
		return nil, err
	}

	return newStaticRelease(m), nil
}

// List walks the chain. A chain that breaks part-way still yields what was
// read before the break: the caller wanted releases, and those are real.
func (c *staticChannel) List(ctx context.Context, limit int) ([]forge.Release, error) {
	manifests, err := c.ch.List(ctx, limit)
	if err != nil && len(manifests) == 0 {
		return nil, err
	}

	out := make([]forge.Release, 0, len(manifests))
	for _, m := range manifests {
		out = append(out, newStaticRelease(m))
	}

	return out, nil
}

// Download streams an asset the manifest named. The redirect string is
// always empty: the reader refuses redirects off the base itself.
func (c *staticChannel) Download(ctx context.Context, asset forge.ReleaseAsset) (io.ReadCloser, string, error) {
	a, ok := asset.(*staticAsset)
	if !ok {
		return nil, "", errors.Newf("asset %q did not come from this channel", asset.GetName())
	}

	rc, err := c.ch.Open(ctx, a.url)
	if err != nil {
		return nil, "", err
	}

	return rc, "", nil
}

func (c *staticChannel) Checksums(ctx context.Context, rel forge.Release, maxBytes int64) ([]byte, error) {
	r, ok := rel.(*staticRelease)
	if !ok {
		return nil, errors.Wrap(forge.ErrNotSupported, "release did not come from this channel")
	}

	return c.ch.Fetch(ctx, r.manifest.Checksums, maxBytes)
}

// Signature is the detached signature the manifest names, or not-supported
// for an unsigned release, which the updater treats exactly as a forge
// release without a signature asset.
func (c *staticChannel) Signature(ctx context.Context, rel forge.Release, maxBytes int64) ([]byte, error) {
	r, ok := rel.(*staticRelease)
	if !ok || r.manifest.Signature == "" {
		return nil, errors.Wrap(forge.ErrNotSupported, "release has no signature")
	}

	return c.ch.Fetch(ctx, r.manifest.Signature, maxBytes)
}

// staticRelease is one manifest as a forge.Release. Its assets are the
// downloads, then the checksums file, then the signature when there is one,
// so the updater's name-based lookups find what they look for today.
type staticRelease struct {
	manifest static.Manifest
	assets   []forge.ReleaseAsset
}

func newStaticRelease(m static.Manifest) *staticRelease {
	r := &staticRelease{manifest: m}

	for _, d := range m.Downloads {
		r.assets = append(r.assets, &staticAsset{id: int64(len(r.assets) + 1), name: d.Name, url: d.URL, goos: d.OS, goarch: d.Arch})
	}

	r.assets = append(r.assets, &staticAsset{id: int64(len(r.assets) + 1), name: static.ChecksumsFile, url: m.Checksums})

	if m.Signature != "" {
		r.assets = append(r.assets, &staticAsset{id: int64(len(r.assets) + 1), name: static.SignatureFile, url: m.Signature})
	}

	return r
}

func (r *staticRelease) GetName() string                 { return r.manifest.Tag }
func (r *staticRelease) GetTagName() string              { return r.manifest.Tag }
func (r *staticRelease) GetBody() string                 { return r.manifest.Notes }
func (r *staticRelease) GetDraft() bool                  { return false }
func (r *staticRelease) GetAssets() []forge.ReleaseAsset { return r.assets }

func (r *staticRelease) GetReleasedAt() time.Time {
	t, err := time.Parse(time.RFC3339, r.manifest.ReleasedAt)
	if err != nil {
		return time.Time{}
	}

	return t
}

// AssetFor is the download built for a platform, by the manifest's own os
// and arch rather than by the archive's name.
func (r *staticRelease) AssetFor(goos, goarch string) (forge.ReleaseAsset, bool) {
	for _, a := range r.assets {
		sa, ok := a.(*staticAsset)
		if ok && sa.goos == goos && sa.goarch == goarch {
			return a, true
		}
	}

	return nil, false
}

// staticAsset is one file a manifest names. The URL is the file's; a
// download for a platform also records which.
type staticAsset struct {
	id           int64
	name, url    string
	goos, goarch string
}

func (a *staticAsset) GetID() int64                  { return a.id }
func (a *staticAsset) GetName() string               { return a.name }
func (a *staticAsset) GetBrowserDownloadURL() string { return a.url }

// platformReleases pick their own asset for a platform; a release that does
// not (every forge release) is searched by the goreleaser name convention.
type platformRelease interface {
	AssetFor(goos, goarch string) (forge.ReleaseAsset, bool)
}

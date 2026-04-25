package setup

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/cockroachdb/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/phpboyscout/go-tool-base/pkg/logger"
	"github.com/phpboyscout/go-tool-base/pkg/props"
	"github.com/phpboyscout/go-tool-base/pkg/vcs/release"
)

// fakeAsset implements [release.ReleaseAsset] for tests. Kept local
// so tests don't need to touch the mockery-generated mocks; the
// update flow only reads name/URL via the interface.
type fakeAsset struct {
	id   int64
	name string
	url  string
}

func (a *fakeAsset) GetID() int64                  { return a.id }
func (a *fakeAsset) GetName() string               { return a.name }
func (a *fakeAsset) GetBrowserDownloadURL() string { return a.url }

// fakeRelease holds a list of fake assets.
type fakeRelease struct {
	name   string
	assets []release.ReleaseAsset
}

func (r *fakeRelease) GetName() string                   { return r.name }
func (r *fakeRelease) GetTagName() string                { return r.name }
func (r *fakeRelease) GetBody() string                   { return "" }
func (r *fakeRelease) GetDraft() bool                    { return false }
func (r *fakeRelease) GetAssets() []release.ReleaseAsset { return r.assets }

// fakeProvider implements [release.Provider] only — no
// [release.ChecksumProvider]. Used to exercise the asset-list
// fallback path. assetBodies maps asset-name → bytes served by
// DownloadReleaseAsset.
type fakeProvider struct {
	rel         release.Release
	assetBodies map[string][]byte
	downloadErr error
}

func (p *fakeProvider) GetLatestRelease(_ context.Context, _, _ string) (release.Release, error) {
	return p.rel, nil
}

func (p *fakeProvider) GetReleaseByTag(_ context.Context, _, _, _ string) (release.Release, error) {
	return p.rel, nil
}

func (p *fakeProvider) ListReleases(_ context.Context, _, _ string, _ int) ([]release.Release, error) {
	return []release.Release{p.rel}, nil
}

func (p *fakeProvider) DownloadReleaseAsset(_ context.Context, _, _ string, asset release.ReleaseAsset) (io.ReadCloser, string, error) {
	if p.downloadErr != nil {
		return nil, "", p.downloadErr
	}

	body, ok := p.assetBodies[asset.GetName()]
	if !ok {
		return nil, "", errors.Newf("fake provider: no body for %q", asset.GetName())
	}

	return io.NopCloser(strings.NewReader(string(body))), "", nil
}

// checksumFakeProvider additionally implements [release.ChecksumProvider]
// — used to verify the preferred path is taken.
type checksumFakeProvider struct {
	fakeProvider
	manifest      []byte
	err           error
	callsManifest int
}

func (p *checksumFakeProvider) DownloadChecksumManifest(_ context.Context, _ release.Release, _ int64) ([]byte, error) {
	p.callsManifest++

	if p.err != nil {
		return nil, p.err
	}

	return p.manifest, nil
}

// manifestFor builds a GoReleaser-style manifest with a single entry
// for filename over body.
func manifestFor(filename string, body []byte) []byte {
	sum := sha256.Sum256(body)

	return fmt.Appendf(nil, "%s  %s\n", hex.EncodeToString(sum[:]), filename)
}

// newTestUpdater wires a minimal SelfUpdater around the given provider
// with an in-process logger. requireChecksum is configurable.
func newTestUpdater(t *testing.T, p release.Provider, require bool) *SelfUpdater {
	t.Helper()

	return &SelfUpdater{
		Tool:            props.Tool{Name: "testtool"},
		logger:          logger.NewNoop(),
		releaseClient:   p,
		requireChecksum: require,
	}
}

func TestVerifyAssetChecksum_HappyPath_AssetList(t *testing.T) {
	t.Parallel()

	binary := []byte("binary-body")
	manifest := manifestFor("testtool_Linux_x86_64.tar.gz", binary)

	rel := &fakeRelease{
		name: "v1.0.0",
		assets: []release.ReleaseAsset{
			&fakeAsset{name: "testtool_Linux_x86_64.tar.gz"},
			&fakeAsset{name: "checksums.txt"},
		},
	}

	provider := &fakeProvider{
		rel: rel,
		assetBodies: map[string][]byte{
			"checksums.txt": manifest,
		},
	}

	s := newTestUpdater(t, provider, false)

	err := s.verifyAssetChecksum(t.Context(), rel, rel.assets[0], binary)
	require.NoError(t, err)
}

func TestVerifyAssetChecksum_Tampered(t *testing.T) {
	t.Parallel()

	genuine := []byte("genuine")
	tampered := []byte("tampered")
	manifest := manifestFor("bin.tar.gz", genuine)

	rel := &fakeRelease{
		name: "v1",
		assets: []release.ReleaseAsset{
			&fakeAsset{name: "bin.tar.gz"},
			&fakeAsset{name: "checksums.txt"},
		},
	}

	provider := &fakeProvider{
		rel: rel,
		assetBodies: map[string][]byte{
			"checksums.txt": manifest,
		},
	}

	s := newTestUpdater(t, provider, false)

	err := s.verifyAssetChecksum(t.Context(), rel, rel.assets[0], tampered)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "checksum mismatch")
}

func TestVerifyAssetChecksum_NoManifest_FailOpen(t *testing.T) {
	t.Parallel()

	// No checksums.txt in the assets list — fail-open mode should
	// log a warning and return nil so the update proceeds (preserves
	// behaviour for legacy releases that predate this feature).
	rel := &fakeRelease{
		name: "v1",
		assets: []release.ReleaseAsset{
			&fakeAsset{name: "bin.tar.gz"},
		},
	}

	provider := &fakeProvider{rel: rel}

	s := newTestUpdater(t, provider, false)

	err := s.verifyAssetChecksum(t.Context(), rel, rel.assets[0], []byte("binary"))
	require.NoError(t, err)
}

func TestVerifyAssetChecksum_NoManifest_FailClosed(t *testing.T) {
	t.Parallel()

	// No checksums.txt + requireChecksum=true must abort.
	rel := &fakeRelease{
		name: "v1",
		assets: []release.ReleaseAsset{
			&fakeAsset{name: "bin.tar.gz"},
		},
	}

	provider := &fakeProvider{rel: rel}

	s := newTestUpdater(t, provider, true)

	err := s.verifyAssetChecksum(t.Context(), rel, rel.assets[0], []byte("binary"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no checksums manifest found")
}

func TestVerifyAssetChecksum_ChecksumProviderPreferred(t *testing.T) {
	t.Parallel()

	// When the provider implements ChecksumProvider, that path is
	// used before the asset-list fallback — even if a checksums.txt
	// asset is also present. This matters for Direct, where the
	// manifest lives at a URL not in the asset list.
	binary := []byte("binary")
	manifest := manifestFor("bin.tar.gz", binary)

	rel := &fakeRelease{
		name: "v1",
		assets: []release.ReleaseAsset{
			&fakeAsset{name: "bin.tar.gz"},
		},
	}

	provider := &checksumFakeProvider{
		fakeProvider: fakeProvider{rel: rel},
		manifest:     manifest,
	}

	s := newTestUpdater(t, provider, false)

	err := s.verifyAssetChecksum(t.Context(), rel, rel.assets[0], binary)
	require.NoError(t, err)
	assert.Equal(t, 1, provider.callsManifest,
		"ChecksumProvider.DownloadChecksumManifest must be called in preference to asset-list lookup")
}

func TestVerifyAssetChecksum_ChecksumProviderErrNotSupportedFallsBack(t *testing.T) {
	t.Parallel()

	// Provider implements ChecksumProvider but returns ErrNotSupported
	// for this release (e.g. Direct with no checksum_url_template).
	// The caller should fall back to asset-list lookup.
	binary := []byte("binary")
	manifest := manifestFor("bin.tar.gz", binary)

	rel := &fakeRelease{
		name: "v1",
		assets: []release.ReleaseAsset{
			&fakeAsset{name: "bin.tar.gz"},
			&fakeAsset{name: "checksums.txt"},
		},
	}

	provider := &checksumFakeProvider{
		fakeProvider: fakeProvider{
			rel: rel,
			assetBodies: map[string][]byte{
				"checksums.txt": manifest,
			},
		},
		err: release.ErrNotSupported,
	}

	s := newTestUpdater(t, provider, false)

	err := s.verifyAssetChecksum(t.Context(), rel, rel.assets[0], binary)
	require.NoError(t, err)
	assert.Equal(t, 1, provider.callsManifest,
		"ChecksumProvider must be tried first")
}

func TestVerifyAssetChecksum_ChecksumProviderOtherErrorAborts(t *testing.T) {
	t.Parallel()

	// A non-ErrNotSupported failure from the provider must NOT fall
	// back — that would let an operator-configured Direct URL
	// masquerade as "manifest not published" on a transient HTTP
	// error. The caller should respect the fail-open / fail-closed
	// policy based on requireChecksum.
	rel := &fakeRelease{
		name:   "v1",
		assets: []release.ReleaseAsset{&fakeAsset{name: "bin.tar.gz"}},
	}

	provider := &checksumFakeProvider{
		fakeProvider: fakeProvider{rel: rel},
		err:          errors.New("transient HTTP 500"),
	}

	// fail-open: the error is logged but the update still proceeds
	s := newTestUpdater(t, provider, false)

	err := s.verifyAssetChecksum(t.Context(), rel, rel.assets[0], []byte("body"))
	require.NoError(t, err, "fail-open mode should not escalate a transient provider error")

	// fail-closed: the error is surfaced as fatal
	s = newTestUpdater(t, provider, true)

	err = s.verifyAssetChecksum(t.Context(), rel, rel.assets[0], []byte("body"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "transient HTTP 500")
}

// fakeBoolConfig implements the narrow boolConfig interface used by
// resolveRequireChecksum, without pulling in the full
// config.Containable surface.
type fakeBoolConfig struct {
	set  map[string]bool
	vals map[string]bool
}

func (c *fakeBoolConfig) IsSet(key string) bool   { return c.set[key] }
func (c *fakeBoolConfig) GetBool(key string) bool { return c.vals[key] }

// TestResolveRequireChecksum_Precedence intentionally does NOT call
// t.Parallel() — every subtest mutates the package-level
// [DefaultRequireChecksum] sentinel, and parallel mutation of that
// global races against itself and against any concurrently-running
// SelfUpdater test that constructs via NewUpdater. Per CLAUDE.md's
// race-avoidance guidance, tests that touch shared package state
// must serialise; the test runs in microseconds so there's no
// throughput cost.
func TestResolveRequireChecksum_Precedence(t *testing.T) {
	// Save-and-restore the compile-time default once so the test
	// can't leave the package in an unexpected state.
	old := DefaultRequireChecksum
	t.Cleanup(func() { DefaultRequireChecksum = old })

	t.Run("nil_config_returns_default", func(t *testing.T) {
		DefaultRequireChecksum = true
		assert.True(t, resolveRequireChecksum(nil))

		DefaultRequireChecksum = false
		assert.False(t, resolveRequireChecksum(nil))
	})

	t.Run("interface_typed_nil_pointer_returns_default", func(t *testing.T) {
		// An interface containing a typed nil (e.g. `var c *fakeBoolConfig;
		// resolveRequireChecksum(c)`) must not panic on method calls.
		// This is the case where a plain `cfg == nil` check fails
		// because the interface itself is non-nil.
		DefaultRequireChecksum = true

		var typedNil *fakeBoolConfig

		assert.True(t, resolveRequireChecksum(typedNil),
			"typed-nil interface must fall through to the compile-time default, not panic")
	})

	t.Run("config_unset_falls_back_to_default", func(t *testing.T) {
		cfg := &fakeBoolConfig{}

		DefaultRequireChecksum = true
		assert.True(t, resolveRequireChecksum(cfg))

		DefaultRequireChecksum = false
		assert.False(t, resolveRequireChecksum(cfg))
	})

	t.Run("config_set_wins_over_default", func(t *testing.T) {
		// Default true, config explicitly false.
		DefaultRequireChecksum = true
		cfg := &fakeBoolConfig{
			set:  map[string]bool{"update.require_checksum": true},
			vals: map[string]bool{"update.require_checksum": false},
		}
		assert.False(t, resolveRequireChecksum(cfg))

		// Default false, config explicitly true.
		DefaultRequireChecksum = false
		cfg = &fakeBoolConfig{
			set:  map[string]bool{"update.require_checksum": true},
			vals: map[string]bool{"update.require_checksum": true},
		}
		assert.True(t, resolveRequireChecksum(cfg))
	})
}

func TestDownloadChecksumManifest_RefusesRedirect(t *testing.T) {
	t.Parallel()

	// A provider that returns a non-empty redirectURL must abort the
	// manifest download — the update flow has no cross-host-redirect
	// policy and silently following would defeat same-origin
	// assumptions.
	provider := &redirectingProvider{redirectURL: "https://elsewhere.example/checksums.txt"}

	s := newTestUpdater(t, provider, false)

	_, err := s.downloadChecksumManifest(t.Context(), &fakeAsset{name: "checksums.txt"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "redirected")
}

func TestDownloadChecksumManifest_RejectsOversizedResponse(t *testing.T) {
	// Mutates the package-level [MaxChecksumsSize] tunable, so
	// cannot run with t.Parallel — see [TestResolveRequireChecksum_Precedence].
	oldMax := MaxChecksumsSize
	t.Cleanup(func() { MaxChecksumsSize = oldMax })

	MaxChecksumsSize = 16

	bigManifest := make([]byte, int(MaxChecksumsSize)+32)

	provider := &fakeProvider{
		rel: &fakeRelease{assets: []release.ReleaseAsset{&fakeAsset{name: "checksums.txt"}}},
		assetBodies: map[string][]byte{
			"checksums.txt": bigManifest,
		},
	}

	s := newTestUpdater(t, provider, false)

	_, err := s.downloadChecksumManifest(t.Context(), &fakeAsset{name: "checksums.txt"})
	require.Error(t, err)
	require.ErrorIs(t, err, ErrChecksumTooLarge)
}

// redirectingProvider always returns a non-empty redirect URL from
// DownloadReleaseAsset so we can exercise the redirect-refusal path
// without standing up an HTTP server.
type redirectingProvider struct {
	fakeProvider
	redirectURL string
}

func (p *redirectingProvider) DownloadReleaseAsset(_ context.Context, _, _ string, _ release.ReleaseAsset) (io.ReadCloser, string, error) {
	return nil, p.redirectURL, nil
}

func TestFindChecksumsAsset_HonoursConfiguredName(t *testing.T) {
	t.Parallel()

	rel := &fakeRelease{
		name: "v1",
		assets: []release.ReleaseAsset{
			&fakeAsset{name: "bin.tar.gz"},
			&fakeAsset{name: "checksums.sha256"},
			&fakeAsset{name: "checksums.txt"},
		},
	}

	s := newTestUpdater(t, &fakeProvider{rel: rel}, false)

	// Default lookup picks "checksums.txt".
	got, ok := s.findChecksumsAsset(rel)
	require.True(t, ok)
	assert.Equal(t, "checksums.txt", got.GetName())

	// Override via s.checksumAssetName picks the configured name.
	s.checksumAssetName = "checksums.sha256"

	got, ok = s.findChecksumsAsset(rel)
	require.True(t, ok)
	assert.Equal(t, "checksums.sha256", got.GetName())
}

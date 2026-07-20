package setup

import (
	"bytes"
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

	"gitlab.com/phpboyscout/go/forge"

	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

// fakeAsset implements [forge.ReleaseAsset] for tests. Kept local
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
	assets []forge.ReleaseAsset
}

func (r *fakeRelease) GetName() string                 { return r.name }
func (r *fakeRelease) GetTagName() string              { return r.name }
func (r *fakeRelease) GetBody() string                 { return "" }
func (r *fakeRelease) GetDraft() bool                  { return false }
func (r *fakeRelease) GetAssets() []forge.ReleaseAsset { return r.assets }

// fakeProvider implements [forge.Provider] only — no
// [forge.ChecksumProvider]. Used to exercise the asset-list
// fallback path. assetBodies maps asset-name → bytes served by
// DownloadReleaseAsset.
type fakeProvider struct {
	rel         forge.Release
	assetBodies map[string][]byte
	downloadErr error
}

func (p *fakeProvider) GetLatestRelease(_ context.Context, _, _ string) (forge.Release, error) {
	return p.rel, nil
}

func (p *fakeProvider) GetReleaseByTag(_ context.Context, _, _, _ string) (forge.Release, error) {
	return p.rel, nil
}

func (p *fakeProvider) ListReleases(_ context.Context, _, _ string, _ int) ([]forge.Release, error) {
	return []forge.Release{p.rel}, nil
}

func (p *fakeProvider) DownloadReleaseAsset(_ context.Context, _, _ string, asset forge.ReleaseAsset) (io.ReadCloser, string, error) {
	if p.downloadErr != nil {
		return nil, "", p.downloadErr
	}

	body, ok := p.assetBodies[asset.GetName()]
	if !ok {
		return nil, "", errors.Newf("fake provider: no body for %q", asset.GetName())
	}

	return io.NopCloser(strings.NewReader(string(body))), "", nil
}

// checksumFakeProvider additionally implements [forge.ChecksumProvider]
// — used to verify the preferred path is taken.
type checksumFakeProvider struct {
	fakeProvider
	manifest      []byte
	err           error
	callsManifest int
}

func (p *checksumFakeProvider) DownloadChecksumManifest(_ context.Context, _ forge.Release, _ int64) ([]byte, error) {
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
func newTestUpdater(t *testing.T, p forge.Provider, require bool) *SelfUpdater {
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
		assets: []forge.ReleaseAsset{
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
		assets: []forge.ReleaseAsset{
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
		assets: []forge.ReleaseAsset{
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
		assets: []forge.ReleaseAsset{
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
		assets: []forge.ReleaseAsset{
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
		assets: []forge.ReleaseAsset{
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
		err: forge.ErrNotSupported,
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
		assets: []forge.ReleaseAsset{&fakeAsset{name: "bin.tar.gz"}},
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

// TestResolveRequireChecksum_Precedence runs in parallel.
//
// It could not before: every subtest mutated a package-level
// DefaultRequireChecksum sentinel, which raced against itself and against any
// concurrent SelfUpdater test. The tool-author baseline is now a value passed
// in — from props.Tool.Signing.RequireChecksum — so each case is independent.
func TestResolveRequireChecksum_Precedence(t *testing.T) {
	t.Parallel()

	yes, no := true, false

	t.Run("nil_config_returns_tool_default", func(t *testing.T) {
		t.Parallel()

		assert.True(t, resolveRequireChecksum(nil, &yes))
		assert.False(t, resolveRequireChecksum(nil, &no))
	})

	t.Run("unset_tool_default_returns_framework_default", func(t *testing.T) {
		t.Parallel()

		assert.Equal(t, requireChecksumDefault, resolveRequireChecksum(nil, nil),
			"a nil tool baseline must fall through to the framework default")
	})

	t.Run("interface_typed_nil_pointer_returns_default", func(t *testing.T) {
		t.Parallel()

		// An interface containing a typed nil must not panic on method calls.
		// A plain `cfg == nil` check fails here because the interface itself
		// is non-nil.
		var typedNil *fakeBoolConfig

		assert.True(t, resolveRequireChecksum(typedNil, &yes),
			"typed-nil interface must fall through to the default, not panic")
	})

	t.Run("config_unset_falls_back_to_tool_default", func(t *testing.T) {
		t.Parallel()

		cfg := &fakeBoolConfig{}

		assert.True(t, resolveRequireChecksum(cfg, &yes))
		assert.False(t, resolveRequireChecksum(cfg, &no))
	})

	t.Run("config_set_wins_over_tool_default", func(t *testing.T) {
		t.Parallel()

		// Tool says require, config explicitly disables.
		cfg := &fakeBoolConfig{
			set:  map[string]bool{"update.require_checksum": true},
			vals: map[string]bool{"update.require_checksum": false},
		}
		assert.False(t, resolveRequireChecksum(cfg, &yes))

		// Tool says permissive, config explicitly requires.
		cfg = &fakeBoolConfig{
			set:  map[string]bool{"update.require_checksum": true},
			vals: map[string]bool{"update.require_checksum": true},
		}
		assert.True(t, resolveRequireChecksum(cfg, &no))
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

// TestDownloadChecksumManifest_RejectsOversizedResponse runs in parallel.
//
// It could not before: it shrank a package-level MaxChecksumsSize tunable to
// force the rejection, which raced with every other test in the package. The
// bound is now per-updater, so this case sets its own and stays independent.
func TestDownloadChecksumManifest_RejectsOversizedResponse(t *testing.T) {
	t.Parallel()

	const bound int64 = 16

	bigManifest := make([]byte, bound+32)

	provider := &fakeProvider{
		rel: &fakeRelease{assets: []forge.ReleaseAsset{&fakeAsset{name: "checksums.txt"}}},
		assetBodies: map[string][]byte{
			"checksums.txt": bigManifest,
		},
	}

	s := newTestUpdater(t, provider, false)
	s.maxChecksumsSize = bound

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

func (p *redirectingProvider) DownloadReleaseAsset(_ context.Context, _, _ string, _ forge.ReleaseAsset) (io.ReadCloser, string, error) {
	return nil, p.redirectURL, nil
}

func TestFindChecksumsAsset_HonoursConfiguredName(t *testing.T) {
	t.Parallel()

	rel := &fakeRelease{
		name: "v1",
		assets: []forge.ReleaseAsset{
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

// TestDownloadAsset_BoundedBySize proves the binary download is capped: a body
// exceeding the configured limit is refused rather than read into memory
// unbounded.
func TestDownloadAsset_BoundedBySize(t *testing.T) {
	t.Parallel()

	rel := &fakeRelease{name: "v1.0.0", assets: []forge.ReleaseAsset{
		&fakeAsset{name: "testtool_Linux_x86_64.tar.gz"},
	}}
	provider := &fakeProvider{rel: rel, assetBodies: map[string][]byte{
		"testtool_Linux_x86_64.tar.gz": bytes.Repeat([]byte("A"), 200),
	}}

	u := newTestUpdater(t, provider, false)
	u.maxBinaryDownloadSize = 100

	_, err := u.DownloadAsset(context.Background(), rel.assets[0])
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrBinaryTooLarge)
}

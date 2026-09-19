package setup

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go/errors"
	"gitlab.com/phpboyscout/go/forge"
	forgetest "gitlab.com/phpboyscout/go/forge/test"
)

// The forge branch of ReleaseChannel (spec 0203 D4) closes over the owner and
// repository and delegates every call to the forge.Provider the updater used
// directly before the seam existed; nothing about the forge path changes.
func TestForgeChannel_DelegatesToTheProvider(t *testing.T) {
	t.Parallel()

	asset := forgetest.TarGzAsset("tool", "tool", "binary")
	provider := forgetest.New(
		forgetest.WithRelease("v1.1.0", asset, forgetest.ChecksumsAsset(false, asset)),
		forgetest.WithRelease("v1.0.0", forgetest.ChecksumsAsset(false, asset)),
		forgetest.WithLatestTag("v1.1.0"),
	)

	ch := newForgeChannel(provider, "acme", "tool")
	ctx := context.Background()

	latest, err := ch.Latest(ctx)
	require.NoError(t, err)
	assert.Equal(t, "v1.1.0", latest.GetTagName())

	byTag, err := ch.ByTag(ctx, "v1.0.0")
	require.NoError(t, err)
	assert.Equal(t, "v1.0.0", byTag.GetTagName())

	list, err := ch.List(ctx, 10)
	require.NoError(t, err)
	require.Len(t, list, 2)
	assert.Equal(t, "v1.1.0", list[0].GetTagName(), "the provider's order, untouched by the seam")

	var binary forge.ReleaseAsset
	for _, a := range latest.GetAssets() {
		if !strings.HasPrefix(a.GetName(), "checksums") {
			binary = a
		}
	}

	require.NotNil(t, binary)

	rc, redirect, err := ch.Download(ctx, binary)
	require.NoError(t, err)
	assert.Empty(t, redirect)

	got, err := io.ReadAll(rc)
	require.NoError(t, err)
	require.NoError(t, rc.Close())
	assert.NotEmpty(t, got)
}

// A provider that does not implement the checksum or signature capability
// answers ErrNotSupported through the seam, which is what the updater's
// asset-list fallback keys on; a provider that does is called.
func TestForgeChannel_CapabilitiesAreOptional(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	asset := forgetest.TarGzAsset("tool", "tool", "binary")
	provider := forgetest.New(forgetest.WithRelease("v1.0.0", asset, forgetest.ChecksumsAsset(false, asset)))

	ch := newForgeChannel(provider, "acme", "tool")

	rel, err := ch.Latest(ctx)
	require.NoError(t, err)

	// The double implements the capability but opts out per release unless
	// told otherwise: the seam passes that answer through unchanged.
	_, err = ch.Checksums(ctx, rel, forge.DefaultMaxChecksumsSize)
	require.ErrorIs(t, err, forge.ErrNotSupported)

	withManifest := forgetest.New(
		forgetest.WithRelease("v1.0.0", asset, forgetest.ChecksumsAsset(false, asset)),
		forgetest.WithChecksumManifest([]byte("abc  tool.tar.gz\n")),
	)

	sums, err := newForgeChannel(withManifest, "acme", "tool").Checksums(ctx, rel, forge.DefaultMaxChecksumsSize)
	require.NoError(t, err)
	assert.Equal(t, "abc  tool.tar.gz\n", string(sums))

	_, err = newForgeChannel(bareProvider{provider}, "acme", "tool").Signature(ctx, rel, forge.DefaultMaxSignatureSize)
	require.True(t, errors.Is(err, forge.ErrNotSupported), "a provider without the capability is a not-supported, not a failure: %v", err)
}

// bareProvider hides every optional capability of the provider it wraps.
type bareProvider struct{ forge.Provider }

// recordingChannel is a ReleaseChannel double that wraps the forge branch
// and records which seam methods the updater called, so the update path can
// be shown to go through the seam and nothing else.
type recordingChannel struct {
	ReleaseChannel
	calls []string
}

func (r *recordingChannel) Latest(ctx context.Context) (forge.Release, error) {
	r.calls = append(r.calls, "Latest")

	return r.ReleaseChannel.Latest(ctx)
}

func (r *recordingChannel) Download(ctx context.Context, a forge.ReleaseAsset) (io.ReadCloser, string, error) {
	r.calls = append(r.calls, "Download")

	return r.ReleaseChannel.Download(ctx, a)
}

func (r *recordingChannel) Checksums(ctx context.Context, rel forge.Release, n int64) ([]byte, error) {
	r.calls = append(r.calls, "Checksums")

	return r.ReleaseChannel.Checksums(ctx, rel, n)
}

// A whole update runs through an injected channel and touches no provider of
// its own: the route the static branch takes in phase 3.
func TestUpdate_RunsThroughAnInjectedChannel(t *testing.T) {
	t.Parallel()

	asset := forgetest.TarGzAsset(e2eToolName, e2eToolName, "new-binary")
	provider := forgetest.New(forgetest.WithRelease("v1.1.0", asset, forgetest.ChecksumsAsset(false, asset)))

	rec := &recordingChannel{ReleaseChannel: newForgeChannel(provider, "acme", e2eToolName)}

	s, currentBin := newE2EUpdater(t, nil)
	withReleaseChannel(rec)(s)
	s.requireChecksum = true

	_, err := s.Update(context.Background())
	require.NoError(t, err)

	got, err := afero.ReadFile(s.Fs, currentBin)
	require.NoError(t, err)
	assert.Equal(t, "new-binary", string(got))
	assert.Subset(t, rec.calls, []string{"Latest", "Download", "Checksums"}, "every release call went through the seam: %v", rec.calls)
}

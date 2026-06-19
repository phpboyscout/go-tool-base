package releasetest_test

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"strings"
	"testing"

	"github.com/ProtonMail/go-crypto/openpgp"
	"github.com/cockroachdb/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/pkg/vcs/release"
	"gitlab.com/phpboyscout/go-tool-base/pkg/vcs/release/releasetest"
)

func TestSource_ImplementsProviderInterfaces(t *testing.T) {
	t.Parallel()

	var s any = releasetest.New()

	_, ok := s.(release.Provider)
	assert.True(t, ok, "Source must implement release.Provider")
	_, ok = s.(release.ChecksumProvider)
	assert.True(t, ok, "Source must implement release.ChecksumProvider")
	_, ok = s.(release.SignatureProvider)
	assert.True(t, ok, "Source must implement release.SignatureProvider")
}

func TestSource_GetLatestRelease(t *testing.T) {
	t.Parallel()

	t.Run("first registered is latest by default", func(t *testing.T) {
		t.Parallel()

		s := releasetest.New(
			releasetest.WithRelease("v1.0.0"),
			releasetest.WithRelease("v1.1.0"),
		)

		rel, err := s.GetLatestRelease(context.Background(), "o", "r")
		require.NoError(t, err)
		assert.Equal(t, "v1.0.0", rel.GetTagName())
	})

	t.Run("WithLatestTag overrides", func(t *testing.T) {
		t.Parallel()

		s := releasetest.New(
			releasetest.WithRelease("v1.0.0"),
			releasetest.WithRelease("v1.1.0"),
			releasetest.WithLatestTag("v1.1.0"),
		)

		rel, err := s.GetLatestRelease(context.Background(), "o", "r")
		require.NoError(t, err)
		assert.Equal(t, "v1.1.0", rel.GetTagName())
	})

	t.Run("no releases yields not-found", func(t *testing.T) {
		t.Parallel()

		_, err := releasetest.New().GetLatestRelease(context.Background(), "o", "r")
		require.ErrorIs(t, err, release.ErrReleaseNotFound)
	})
}

func TestSource_ReleaseAndAssetAccessors(t *testing.T) {
	t.Parallel()

	s := releasetest.New(releasetest.WithRelease("v1.0.0", releasetest.Asset{Name: "a.tar.gz", Body: []byte("x")}))

	rel, err := s.GetLatestRelease(context.Background(), "o", "r")
	require.NoError(t, err)
	assert.Equal(t, "v1.0.0", rel.GetName())
	assert.Empty(t, rel.GetBody())
	assert.False(t, rel.GetDraft())

	require.Len(t, rel.GetAssets(), 1)
	a := rel.GetAssets()[0]
	assert.Equal(t, "a.tar.gz", a.GetName())
	assert.Zero(t, a.GetID())
	assert.Empty(t, a.GetBrowserDownloadURL())
}

func TestSource_GetLatestRelease_LatestTagMissing(t *testing.T) {
	t.Parallel()

	// latest tag points at an unregistered release.
	s := releasetest.New(
		releasetest.WithRelease("v1.0.0"),
		releasetest.WithLatestTag("v9.9.9"),
	)

	_, err := s.GetLatestRelease(context.Background(), "o", "r")
	require.ErrorIs(t, err, release.ErrReleaseNotFound)
}

func TestSource_GetReleaseByTag(t *testing.T) {
	t.Parallel()

	s := releasetest.New(
		releasetest.WithRelease("v1.0.0"),
		releasetest.WithMissingTag("v9.9.9"),
	)

	rel, err := s.GetReleaseByTag(context.Background(), "o", "r", "v1.0.0")
	require.NoError(t, err)
	assert.Equal(t, "v1.0.0", rel.GetTagName())

	_, err = s.GetReleaseByTag(context.Background(), "o", "r", "v9.9.9")
	require.ErrorIs(t, err, release.ErrReleaseNotFound, "explicitly missing tag")

	_, err = s.GetReleaseByTag(context.Background(), "o", "r", "v2.0.0")
	require.ErrorIs(t, err, release.ErrReleaseNotFound, "unregistered tag")
}

func TestSource_ListReleases(t *testing.T) {
	t.Parallel()

	s := releasetest.New(
		releasetest.WithRelease("v1.0.0"),
		releasetest.WithRelease("v1.1.0"),
		releasetest.WithRelease("v1.2.0"),
	)

	all, err := s.ListReleases(context.Background(), "o", "r", 0)
	require.NoError(t, err)
	require.Len(t, all, 3)
	assert.Equal(t, "v1.0.0", all[0].GetTagName(), "registration order preserved")

	limited, err := s.ListReleases(context.Background(), "o", "r", 2)
	require.NoError(t, err)
	assert.Len(t, limited, 2, "limit honoured")
}

func TestSource_DownloadReleaseAsset(t *testing.T) {
	t.Parallel()

	binAsset := releasetest.TarGzAsset("mytool", "mytool", "the-binary")
	s := releasetest.New(releasetest.WithRelease("v1.0.0", binAsset))

	rel, err := s.GetLatestRelease(context.Background(), "o", "r")
	require.NoError(t, err)
	require.Len(t, rel.GetAssets(), 1)

	rc, _, err := s.DownloadReleaseAsset(context.Background(), "o", "r", rel.GetAssets()[0])
	require.NoError(t, err)

	body, err := io.ReadAll(rc)
	require.NoError(t, err)
	require.NoError(t, rc.Close())
	assert.Equal(t, "mytool", extractSingleTarGz(t, body).name)
	assert.Equal(t, "the-binary", extractSingleTarGz(t, body).body)
}

func TestSource_DownloadReleaseAsset_Errors(t *testing.T) {
	t.Parallel()

	t.Run("unknown asset", func(t *testing.T) {
		t.Parallel()

		s := releasetest.New(releasetest.WithRelease("v1.0.0"))
		_, _, err := s.DownloadReleaseAsset(context.Background(), "o", "r", &fakeAsset{name: "nope"})
		require.Error(t, err)
	})

	t.Run("WithDownloadError", func(t *testing.T) {
		t.Parallel()

		sentinel := errors.New("boom")
		s := releasetest.New(
			releasetest.WithRelease("v1.0.0", releasetest.TarGzAsset("mytool", "mytool", "x")),
			releasetest.WithDownloadError(sentinel),
		)
		_, _, err := s.DownloadReleaseAsset(context.Background(), "o", "r", &fakeAsset{name: releasetest.AssetName("mytool")})
		require.ErrorIs(t, err, sentinel)
	})
}

func TestSource_ChecksumAndSignatureProviderModes(t *testing.T) {
	t.Parallel()

	t.Run("default returns ErrNotSupported (asset-by-name fallback)", func(t *testing.T) {
		t.Parallel()

		s := releasetest.New(releasetest.WithRelease("v1.0.0"))

		_, err := s.DownloadChecksumManifest(context.Background(), nil, 1<<20)
		require.ErrorIs(t, err, release.ErrNotSupported)

		_, err = s.DownloadSignature(context.Background(), nil, 1<<20)
		require.ErrorIs(t, err, release.ErrNotSupported)
	})

	t.Run("manifest mode returns configured bytes", func(t *testing.T) {
		t.Parallel()

		s := releasetest.New(
			releasetest.WithChecksumManifest([]byte("manifest")),
			releasetest.WithSignatureManifest([]byte("signature")),
		)

		m, err := s.DownloadChecksumManifest(context.Background(), nil, 1<<20)
		require.NoError(t, err)
		assert.Equal(t, "manifest", string(m))

		sig, err := s.DownloadSignature(context.Background(), nil, 1<<20)
		require.NoError(t, err)
		assert.Equal(t, "signature", string(sig))
	})
}

func TestAssetName(t *testing.T) {
	t.Parallel()

	name := releasetest.AssetName("mytool")
	assert.True(t, strings.HasPrefix(name, "mytool_"), "name starts with tool: %s", name)
	assert.True(t, strings.HasSuffix(name, ".tar.gz"), "name ends with .tar.gz: %s", name)
}

func TestManifest(t *testing.T) {
	t.Parallel()

	a := releasetest.Asset{Name: "mytool_bin.tar.gz", Body: []byte("payload")}

	good := releasetest.Manifest(false, a)
	wantSum := sha256.Sum256(a.Body)
	assert.Contains(t, string(good), hex.EncodeToString(wantSum[:]), "good manifest hashes the real body")
	assert.Contains(t, string(good), a.Name)

	corrupt := releasetest.Manifest(true, a)
	assert.NotContains(t, string(corrupt), hex.EncodeToString(wantSum[:]),
		"corrupt manifest must not carry the real body hash")
}

func TestChecksumsAsset(t *testing.T) {
	t.Parallel()

	c := releasetest.ChecksumsAsset(false, releasetest.Asset{Name: "a", Body: []byte("b")})
	assert.Equal(t, "checksums.txt", c.Name)
}

func TestSignatureAsset(t *testing.T) {
	t.Parallel()

	entity, err := openpgp.NewEntity("Test", "releasetest", "test@example.org", nil)
	require.NoError(t, err)

	manifest := releasetest.Manifest(false, releasetest.Asset{Name: "a", Body: []byte("b")})

	good := releasetest.SignatureAsset(entity, manifest, false)
	assert.Equal(t, "checksums.txt.sig", good.Name)
	assert.Contains(t, string(good.Body), "-----BEGIN PGP SIGNATURE-----")

	// Good signature verifies against the manifest it signed.
	_, err = openpgp.CheckArmoredDetachedSignature(
		openpgp.EntityList{entity}, bytes.NewReader(manifest), bytes.NewReader(good.Body), nil)
	require.NoError(t, err, "good signature must verify against the served manifest")

	// Bad signature does NOT verify against the served manifest.
	bad := releasetest.SignatureAsset(entity, manifest, true)
	_, err = openpgp.CheckArmoredDetachedSignature(
		openpgp.EntityList{entity}, bytes.NewReader(manifest), bytes.NewReader(bad.Body), nil)
	require.Error(t, err, "bad signature must not verify against the served manifest")
}

// --- helpers ---

type fakeAsset struct{ name string }

func (a *fakeAsset) GetID() int64                  { return 0 }
func (a *fakeAsset) GetName() string               { return a.name }
func (a *fakeAsset) GetBrowserDownloadURL() string { return "" }

type tarEntry struct {
	name string
	body string
}

func extractSingleTarGz(t *testing.T, b []byte) tarEntry {
	t.Helper()

	gr, err := gzip.NewReader(bytes.NewReader(b))
	require.NoError(t, err)

	tr := tar.NewReader(gr)

	hdr, err := tr.Next()
	require.NoError(t, err)

	body, err := io.ReadAll(tr)
	require.NoError(t, err)

	return tarEntry{name: hdr.Name, body: string(body)}
}

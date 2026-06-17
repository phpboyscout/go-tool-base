package setup

import (
	"fmt"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"

	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/vcs/release"
)

// These exercise the full Update() pipeline (download → checksum →
// signature → extract → replace) with verification ENABLED, on an in-memory
// filesystem and the local fake release provider — no network. The constituent
// verifiers are unit-tested elsewhere; these guard that Update() actually wires
// them in and aborts (without replacing the binary) when verification fails.

const e2eToolName = "testtool"

// e2eAssetName mirrors findReleaseAsset's OS/arch naming so the served asset is
// the one Update will look for.
func e2eAssetName() string {
	arch := runtime.GOARCH
	if arch == "amd64" {
		arch = "x86_64"
	}

	return fmt.Sprintf("%s_%s_%s.tar.gz", e2eToolName, cases.Title(language.Und).String(runtime.GOOS), arch)
}

// newE2EUpdater builds a SelfUpdater wired for a full Update over memfs: the
// current binary already exists and os/exec resolution points at it.
func newE2EUpdater(t *testing.T, provider release.Provider) (*SelfUpdater, string) {
	t.Helper()

	memFS := afero.NewMemMapFs()
	currentBin := "/usr/local/bin/" + e2eToolName
	require.NoError(t, memFS.MkdirAll(filepath.Dir(currentBin), 0o755))
	require.NoError(t, afero.WriteFile(memFS, currentBin, []byte("old-binary"), 0o755))
	require.NoError(t, memFS.MkdirAll(GetDefaultConfigDir(memFS, e2eToolName), 0o755))

	s := &SelfUpdater{
		Tool:           props.Tool{Name: e2eToolName},
		logger:         logger.NewNoop(),
		releaseClient:  provider,
		CurrentVersion: "v1.0.0",
		Fs:             memFS,
		osExecutable:   func() (string, error) { return currentBin, nil },
		execLookPath:   func(string) (string, error) { return currentBin, nil },
	}

	return s, currentBin
}

func TestUpdate_VerifiesChecksum_HappyPath(t *testing.T) {
	t.Parallel()

	asset := e2eAssetName()
	tarGz := createTarGz(t, e2eToolName, "new-binary")

	provider := &fakeProvider{
		rel: &fakeRelease{name: "v1.1.0", assets: []release.ReleaseAsset{
			&fakeAsset{name: asset}, &fakeAsset{name: "checksums.txt"},
		}},
		assetBodies: map[string][]byte{
			asset:           tarGz,
			"checksums.txt": manifestFor(asset, tarGz),
		},
	}

	s, currentBin := newE2EUpdater(t, provider)
	s.requireChecksum = true

	path, err := s.Update(t.Context())
	require.NoError(t, err)
	assert.Equal(t, currentBin, path)

	got, err := afero.ReadFile(s.Fs, currentBin)
	require.NoError(t, err)
	assert.Equal(t, "new-binary", string(got), "verified update must replace the binary")
}

func TestUpdate_AbortsOnChecksumMismatch(t *testing.T) {
	t.Parallel()

	asset := e2eAssetName()
	tarGz := createTarGz(t, e2eToolName, "new-binary")

	provider := &fakeProvider{
		rel: &fakeRelease{name: "v1.1.0", assets: []release.ReleaseAsset{
			&fakeAsset{name: asset}, &fakeAsset{name: "checksums.txt"},
		}},
		assetBodies: map[string][]byte{
			asset: tarGz,
			// Manifest hashes a different payload → mismatch.
			"checksums.txt": manifestFor(asset, []byte("not-the-downloaded-binary")),
		},
	}

	s, currentBin := newE2EUpdater(t, provider)
	s.requireChecksum = true

	_, err := s.Update(t.Context())
	require.Error(t, err, "Update must abort when the asset fails checksum verification")

	got, err := afero.ReadFile(s.Fs, currentBin)
	require.NoError(t, err)
	assert.Equal(t, "old-binary", string(got), "a failed checksum must not replace the binary")
}

func TestUpdate_VerifiesSignedChecksum_HappyPath(t *testing.T) {
	t.Parallel()
	mustInitTestSigningKeys(t)

	asset := e2eAssetName()
	tarGz := createTarGz(t, e2eToolName, "new-binary")
	manifest := manifestFor(asset, tarGz)
	sig := detachSign(t, testEd25519.entity, manifest)

	provider := &fakeProvider{
		rel: &fakeRelease{name: "v1.1.0", assets: []release.ReleaseAsset{
			&fakeAsset{name: asset}, &fakeAsset{name: "checksums.txt"}, &fakeAsset{name: "checksums.txt.sig"},
		}},
		assetBodies: map[string][]byte{
			asset:               tarGz,
			"checksums.txt":     manifest,
			"checksums.txt.sig": sig,
		},
	}

	s, currentBin := newE2EUpdater(t, provider)
	s.requireChecksum = true
	s.requireSignature = true
	s.embeddedKeys = [][]byte{testEd25519.armoredPub}
	s.keySource = DefaultKeySource
	require.NoError(t, s.buildDefaultKeyResolver())

	path, err := s.Update(t.Context())
	require.NoError(t, err)
	assert.Equal(t, currentBin, path)

	got, err := afero.ReadFile(s.Fs, currentBin)
	require.NoError(t, err)
	assert.Equal(t, "new-binary", string(got))
}

func TestUpdate_AbortsOnBadSignature(t *testing.T) {
	t.Parallel()
	mustInitTestSigningKeys(t)

	asset := e2eAssetName()
	tarGz := createTarGz(t, e2eToolName, "new-binary")
	manifest := manifestFor(asset, tarGz)
	// Signature is over a DIFFERENT manifest, so it will not verify against the
	// one actually served.
	badSig := detachSign(t, testEd25519.entity, []byte("a different manifest entirely\n"))

	provider := &fakeProvider{
		rel: &fakeRelease{name: "v1.1.0", assets: []release.ReleaseAsset{
			&fakeAsset{name: asset}, &fakeAsset{name: "checksums.txt"}, &fakeAsset{name: "checksums.txt.sig"},
		}},
		assetBodies: map[string][]byte{
			asset:               tarGz,
			"checksums.txt":     manifest,
			"checksums.txt.sig": badSig,
		},
	}

	s, currentBin := newE2EUpdater(t, provider)
	s.requireChecksum = true
	s.requireSignature = true
	s.embeddedKeys = [][]byte{testEd25519.armoredPub}
	s.keySource = DefaultKeySource
	require.NoError(t, s.buildDefaultKeyResolver())

	_, err := s.Update(t.Context())
	require.Error(t, err, "Update must abort when the checksums manifest signature does not verify")

	got, err := afero.ReadFile(s.Fs, currentBin)
	require.NoError(t, err)
	assert.Equal(t, "old-binary", string(got), "a bad signature must not replace the binary")
}

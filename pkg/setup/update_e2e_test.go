package setup

import (
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go/signing/verify"

	"gitlab.com/phpboyscout/go/forge"
	forgetest "gitlab.com/phpboyscout/go/forge/test"

	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

// These exercise the full Update() pipeline (download → checksum →
// signature → extract → replace) with verification ENABLED, on an in-memory
// filesystem and the shared forge.Provider test double — no network. The
// constituent verifiers are unit-tested elsewhere; these guard that Update()
// actually wires them in and aborts (without replacing the binary) when
// verification fails. They double as the worked example for driving the real
// pipeline from gitlab.com/phpboyscout/go/forge/test.

const e2eToolName = "testtool"

// newE2EUpdater builds a SelfUpdater wired for a full Update over memfs: the
// current binary already exists and os/exec resolution points at it.
func newE2EUpdater(t *testing.T, provider forge.Provider) (*SelfUpdater, string) {
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

	asset := forgetest.TarGzAsset(e2eToolName, e2eToolName, "new-binary")
	provider := forgetest.New(forgetest.WithRelease("v1.1.0",
		asset, forgetest.ChecksumsAsset(false, asset)))

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

	asset := forgetest.TarGzAsset(e2eToolName, e2eToolName, "new-binary")
	// Corrupt manifest hashes a different payload than the served asset → mismatch.
	provider := forgetest.New(forgetest.WithRelease("v1.1.0",
		asset, forgetest.ChecksumsAsset(true, asset)))

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

	asset := forgetest.TarGzAsset(e2eToolName, e2eToolName, "new-binary")
	manifest := forgetest.Manifest(false, asset)

	provider := forgetest.New(forgetest.WithRelease("v1.1.0",
		asset,
		forgetest.Asset{Name: "checksums.txt", Body: manifest},
		forgetest.SignatureAsset(testEd25519.entity, manifest, false),
	))

	s, currentBin := newE2EUpdater(t, provider)
	s.requireChecksum = true
	s.requireSignature = true
	s.embeddedKeys = [][]byte{testEd25519.armoredPub}
	s.keySource = verify.DefaultKeySource
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

	asset := forgetest.TarGzAsset(e2eToolName, e2eToolName, "new-binary")
	manifest := forgetest.Manifest(false, asset)

	provider := forgetest.New(forgetest.WithRelease("v1.1.0",
		asset,
		forgetest.Asset{Name: "checksums.txt", Body: manifest},
		// Signature over a different payload, so it will not verify against the
		// served manifest.
		forgetest.SignatureAsset(testEd25519.entity, manifest, true),
	))

	s, currentBin := newE2EUpdater(t, provider)
	s.requireChecksum = true
	s.requireSignature = true
	s.embeddedKeys = [][]byte{testEd25519.armoredPub}
	s.keySource = verify.DefaultKeySource
	require.NoError(t, s.buildDefaultKeyResolver())

	_, err := s.Update(t.Context())
	require.Error(t, err, "Update must abort when the checksums manifest signature does not verify")

	got, err := afero.ReadFile(s.Fs, currentBin)
	require.NoError(t, err)
	assert.Equal(t, "old-binary", string(got), "a bad signature must not replace the binary")
}

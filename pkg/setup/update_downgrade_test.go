package setup

import (
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go/errors"
	forgetest "gitlab.com/phpboyscout/go/forge/test"
)

// These guard the self-update downgrade policy (spec
// 2026-07-23-self-update-downgrade-guard): signature and checksum
// verification authenticate an artefact, not its recency, so a stale or
// rolled-back release listing serving an older "latest" must not silently
// downgrade the tool. The implicit (no --version) path refuses a downgrade
// without --force; an explicit --version is sufficient intent on its own.

// newStaleLatestUpdater builds an updater whose running binary (v1.2.0) is
// NEWER than the fully valid v1.1.0 release the source reports as latest —
// absent a guard, the pipeline verifies and installs the older binary.
func newStaleLatestUpdater(t *testing.T) (*SelfUpdater, string) {
	t.Helper()

	asset := forgetest.TarGzAsset(e2eToolName, e2eToolName, "older-binary")
	provider := forgetest.New(forgetest.WithRelease("v1.1.0",
		asset, forgetest.ChecksumsAsset(false, asset)))

	s, currentBin := newE2EUpdater(t, provider)
	s.CurrentVersion = "v1.2.0"
	s.requireChecksum = true

	return s, currentBin
}

func TestUpdate_RefusesImplicitStaleLatestDowngrade(t *testing.T) {
	t.Parallel()

	s, currentBin := newStaleLatestUpdater(t)

	_, err := s.Update(t.Context())
	require.Error(t, err,
		"Update must refuse to install an older 'latest' release without --force")
	require.ErrorIs(t, err, ErrDowngradeRefused)

	got, readErr := afero.ReadFile(s.Fs, currentBin)
	require.NoError(t, readErr)
	assert.Equal(t, "old-binary", string(got),
		"a refused downgrade must not replace the binary")
}

func TestUpdate_ImplicitDowngradeRefusal_NamesVersionsAndCarriesHint(t *testing.T) {
	t.Parallel()

	s, _ := newStaleLatestUpdater(t)

	_, err := s.Update(t.Context())
	require.ErrorIs(t, err, ErrDowngradeRefused)

	assert.Contains(t, err.Error(), "v1.2.0", "refusal must name the running version")
	assert.Contains(t, err.Error(), "v1.1.0", "refusal must name the reported latest version")

	hints := errors.GetAllHints(err)
	require.NotEmpty(t, hints, "refusal must carry a remediation hint")
	assert.Contains(t, hints[0], "--force")
	assert.Contains(t, hints[0], "--version")
}

func TestUpdate_ImplicitDowngrade_ForceProceeds(t *testing.T) {
	t.Parallel()

	s, currentBin := newStaleLatestUpdater(t)
	s.force = true

	path, err := s.Update(t.Context())
	require.NoError(t, err, "--force must permit the downgrade")
	assert.Equal(t, currentBin, path)

	got, readErr := afero.ReadFile(s.Fs, currentBin)
	require.NoError(t, readErr)
	assert.Equal(t, "older-binary", string(got),
		"a forced downgrade must install the older release")
}

func TestUpdate_ExplicitVersionDowngrade_ProceedsWithoutForce(t *testing.T) {
	t.Parallel()

	s, currentBin := newStaleLatestUpdater(t)
	s.version = "v1.1.0"

	path, err := s.Update(t.Context())
	require.NoError(t, err,
		"an explicit --version naming an older release is sufficient intent on its own")
	assert.Equal(t, currentBin, path)

	got, readErr := afero.ReadFile(s.Fs, currentBin)
	require.NoError(t, readErr)
	assert.Equal(t, "older-binary", string(got),
		"an explicit --version downgrade must install the requested release")
}

func TestUpdate_UpgradePath_Unaffected(t *testing.T) {
	t.Parallel()

	asset := forgetest.TarGzAsset(e2eToolName, e2eToolName, "new-binary")
	provider := forgetest.New(forgetest.WithRelease("v1.1.0",
		asset, forgetest.ChecksumsAsset(false, asset)))

	s, currentBin := newE2EUpdater(t, provider) // current v1.0.0 < latest v1.1.0
	s.requireChecksum = true

	_, err := s.Update(t.Context())
	require.NoError(t, err)

	got, readErr := afero.ReadFile(s.Fs, currentBin)
	require.NoError(t, readErr)
	assert.Equal(t, "new-binary", string(got), "the upgrade path must still install")
}

func TestUpdate_AlreadyLatest_SkipsWithoutError(t *testing.T) {
	t.Parallel()

	provider := forgetest.New(forgetest.WithRelease("v1.0.0"))

	s, currentBin := newE2EUpdater(t, provider) // current v1.0.0 == latest

	path, err := s.Update(t.Context())
	require.NoError(t, err, "already-latest must remain a silent skip, not an error")
	assert.Equal(t, currentBin, path)

	got, readErr := afero.ReadFile(s.Fs, currentBin)
	require.NoError(t, readErr)
	assert.Equal(t, "old-binary", string(got))
}

func TestUpdate_ForceReinstallCurrentVersion_Unaffected(t *testing.T) {
	t.Parallel()

	asset := forgetest.TarGzAsset(e2eToolName, e2eToolName, "same-version-binary")
	provider := forgetest.New(forgetest.WithRelease("v1.0.0",
		asset, forgetest.ChecksumsAsset(false, asset)))

	s, currentBin := newE2EUpdater(t, provider) // current v1.0.0 == latest
	s.requireChecksum = true
	s.force = true

	_, err := s.Update(t.Context())
	require.NoError(t, err, "--force reinstall of the current version must still work")

	got, readErr := afero.ReadFile(s.Fs, currentBin)
	require.NoError(t, readErr)
	assert.Equal(t, "same-version-binary", string(got))
}

func TestRefuseImplicitDowngrade_VersionCheckErrorSurfaces(t *testing.T) {
	t.Parallel()

	// A source with no releases makes the latest-version lookup fail; the
	// guard must surface that error rather than treating it as "no downgrade".
	s, _ := newE2EUpdater(t, forgetest.New())

	err := s.refuseImplicitDowngrade(t.Context())
	require.Error(t, err)
	require.NotErrorIs(t, err, ErrDowngradeRefused)
}

func TestUpdate_DevelopmentVersion_StillRequiresForce(t *testing.T) {
	t.Parallel()

	asset := forgetest.TarGzAsset(e2eToolName, e2eToolName, "release-binary")
	provider := forgetest.New(forgetest.WithRelease("v1.1.0",
		asset, forgetest.ChecksumsAsset(false, asset)))

	s, currentBin := newE2EUpdater(t, provider)
	s.CurrentVersion = "v0.0.0"
	s.requireChecksum = true

	// Without --force a development build never updates (silent skip).
	_, err := s.Update(t.Context())
	require.NoError(t, err)

	got, readErr := afero.ReadFile(s.Fs, currentBin)
	require.NoError(t, readErr)
	assert.Equal(t, "old-binary", string(got),
		"a development build must not update without --force")

	// With --force it updates as before — the guard must not interfere.
	s.force = true
	_, err = s.Update(t.Context())
	require.NoError(t, err)

	got, readErr = afero.ReadFile(s.Fs, currentBin)
	require.NoError(t, readErr)
	assert.Equal(t, "release-binary", string(got))
}

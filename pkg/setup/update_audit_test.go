package setup

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	mockRelease "gitlab.com/phpboyscout/go-tool-base/mocks/pkg/vcs/release"
	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

// TestIsLatestVersion_EmptyTagYieldsRealError proves the empty-version
// path returns a non-nil error instead of errors.WithStack(nil). A release
// source returning a blank/unparseable tag must not be treated as "up to
// date" (which would silently skip the update).
func TestIsLatestVersion_EmptyTagYieldsRealError(t *testing.T) {
	t.Parallel()

	rel := mockRelease.NewMockRelease(t)
	rel.EXPECT().GetTagName().Return("") // FormatVersionString("") -> ""

	updater := &SelfUpdater{
		Tool:           props.Tool{Name: "test-tool"},
		logger:         logger.NewNoop(),
		CurrentVersion: "v1.0.0", // not the dev sentinel v0.0.0
		NextRelease:    rel,
		Fs:             afero.NewMemMapFs(),
	}

	isLatest, _, err := updater.IsLatestVersion(context.Background())
	require.Error(t, err, "empty version tag must surface a real error")
	assert.False(t, isLatest, "must not report the binary as up to date")
}

// TestUpdateFromFile_FailedExtractDoesNotStampTimestamps proves the
// freshness markers are written only after a successful extract. A bogus
// (non-gzip) archive forces extract() to fail; the offline path reaches the
// same stamp-on-success logic as the online Update.
func TestUpdateFromFile_FailedExtractDoesNotStampTimestamps(t *testing.T) {
	t.Parallel()

	memFS := afero.NewMemMapFs()
	toolName := "test-tool"
	currentBin := "/usr/local/bin/" + toolName

	require.NoError(t, memFS.MkdirAll(filepath.Dir(currentBin), 0o755))

	configDir := GetDefaultConfigDir(memFS, toolName)
	require.NotEmpty(t, configDir, "test host must have a resolvable HOME")

	// A non-gzip archive forces extract() to fail.
	const badArchive = "/tmp/update.tar.gz"
	require.NoError(t, afero.WriteFile(memFS, badArchive, []byte("not a gzip archive"), 0o644))

	updater := &SelfUpdater{
		Tool:           props.Tool{Name: toolName},
		logger:         logger.NewNoop(),
		CurrentVersion: "v1.0.0",
		Fs:             memFS,
		osExecutable:   func() (string, error) { return currentBin, nil },
		execLookPath:   func(_ string) (string, error) { return currentBin, nil },
	}

	_, err := updater.UpdateFromFile(badArchive)
	require.Error(t, err, "a non-gzip archive must fail extraction")

	updatedMarker := filepath.Join(configDir, "last_updated")
	exists, statErr := afero.Exists(memFS, updatedMarker)
	require.NoError(t, statErr)
	assert.False(t, exists, "last_updated must NOT be stamped when extract fails")
}

// recordingFs records Create calls so a test can assert nothing was written
// (e.g. into the current working directory) on the empty-config-dir path.
type recordingFs struct {
	afero.Fs
	created []string
}

func (r *recordingFs) Create(name string) (afero.File, error) {
	r.created = append(r.created, name)

	return r.Fs.Create(name)
}

// TestSetTimeSinceLast_EmptyConfigDirIsNoOp proves that when the config
// directory cannot be resolved (empty HOME), the timestamp write is skipped
// entirely rather than landing a relative "last_*" file in the cwd.
func TestSetTimeSinceLast_EmptyConfigDirIsNoOp(t *testing.T) {
	t.Parallel()

	rec := &recordingFs{Fs: afero.NewMemMapFs()}

	err := setTimeSinceLastIn(rec, "", CheckedKey, "")
	require.NoError(t, err, "empty config dir must be a no-op")
	assert.Empty(t, rec.created,
		"no timestamp file may be created when the config dir is empty")
}

// TestGetDefaultConfigDir_EmptyHomeReturnsEmpty pins the contract that an
// unresolved/empty HOME yields an empty path (so callers skip writes).
func TestGetDefaultConfigDir_EmptyHomeReturnsEmpty(t *testing.T) {
	t.Setenv("HOME", "")

	got := GetDefaultConfigDir(afero.NewMemMapFs(), "test-tool")
	assert.Empty(t, got,
		"empty HOME must yield an empty config dir, not a relative path")
}

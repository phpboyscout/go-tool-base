package setup

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go/errors"
	"gitlab.com/phpboyscout/go/signing/verify"
)

const rotationTestDir = "/home/user/.config/testtool"

func TestRotationStore_MissingFileIsTheZeroRecord(t *testing.T) {
	t.Parallel()

	rec, err := newRotationStore(afero.NewMemMapFs(), rotationTestDir).Load(context.Background())
	require.NoError(t, err)
	assert.Equal(t, verify.RotationRecord{}, rec)
}

func TestRotationStore_RoundTripsWithOwnerOnlyPermissions(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	want := verify.RotationRecord{Epoch: 3, Added: []string{string(newReleaseKey(t, "Added Key").armoredPub)}, Revoked: []string{"ABCD"}}

	require.NoError(t, newRotationStore(fs, rotationTestDir).Save(context.Background(), want))

	got, err := newRotationStore(fs, rotationTestDir).Load(context.Background())
	require.NoError(t, err)
	assert.Equal(t, want, got)

	info, err := fs.Stat(filepath.Join(rotationTestDir, rotationStoreFilename))
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(filePermTrust), info.Mode().Perm())
}

// A record that cannot be read is an error: the zero record would reset the
// replay guard and drop every revoke.
func TestRotationStore_CorruptRecordIsAnError(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	require.NoError(t, afero.WriteFile(fs, filepath.Join(rotationTestDir, rotationStoreFilename), []byte("{not json"), filePermTrust))

	_, err := newRotationStore(fs, rotationTestDir).Load(context.Background())
	require.ErrorIs(t, err, ErrRotationStoreCorrupt)

	err = newRotationStore(fs, rotationTestDir).Save(context.Background(), verify.RotationRecord{Epoch: 1})
	require.ErrorIs(t, err, ErrRotationStoreCorrupt, "a corrupt record is never overwritten as if it were empty")
}

// Save keeps the library's contract, because ApplyRotation does not serialise
// Load and Save: a lower epoch, or other keys at the stored epoch, is refused.
func TestRotationStore_SaveRefusesARollback(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	store := newRotationStore(fs, rotationTestDir)
	ctx := context.Background()
	a, b, c := string(newReleaseKey(t, "A").armoredPub), string(newReleaseKey(t, "B").armoredPub), string(newReleaseKey(t, "C").armoredPub)
	stored := verify.RotationRecord{Epoch: 5, Added: []string{b, a}}
	require.NoError(t, store.Save(ctx, stored))

	require.ErrorIs(t, store.Save(ctx, verify.RotationRecord{Epoch: 4}), verify.ErrRotationReplayed)
	require.ErrorIs(t, store.Save(ctx, verify.RotationRecord{Epoch: 5, Added: []string{c}}), verify.ErrRotationRefused)
	require.NoError(t, store.Save(ctx, verify.RotationRecord{Epoch: 5, Added: []string{a, b}}), "the same keys in another order are the same record")
	require.NoError(t, store.Save(ctx, verify.RotationRecord{Epoch: 6}))
}

// A config directory inside a git checkout could have been supplied by the
// repository, so the record there is neither read nor written (go-tool-base#179).
// A manifest still applies for the run, from memory.
func TestRotationStore_InCheckoutIsNeverReadOrWritten(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	require.NoError(t, fs.MkdirAll("/repo/.git", 0o755))
	dir := "/repo/.config/testtool"
	path := filepath.Join(dir, rotationStoreFilename)
	require.NoError(t, afero.WriteFile(fs, path, []byte(`{"epoch":9}`), filePermTrust))

	store := newRotationStore(fs, dir)

	rec, err := store.Load(context.Background())
	require.NoError(t, err)
	assert.Equal(t, verify.RotationRecord{}, rec, "a record the repository could have written is not read")

	require.NoError(t, store.Save(context.Background(), verify.RotationRecord{Epoch: 1}))

	rec, err = store.Load(context.Background())
	require.NoError(t, err)
	assert.Equal(t, uint64(1), rec.Epoch, "the run keeps what it applied")

	data, err := afero.ReadFile(fs, path)
	require.NoError(t, err)
	assert.JSONEq(t, `{"epoch":9}`, string(data), "nothing is written into the checkout")
}

func TestCanKeepRotationRecordIn(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	require.NoError(t, fs.MkdirAll("/repo/.git", 0o755))

	require.NoError(t, canKeepRotationRecordIn(fs, rotationTestDir))

	for dir, hint := range map[string]string{"": "Set HOME", "/repo/.config/testtool": "outside any git checkout"} {
		err := canKeepRotationRecordIn(fs, dir)
		require.ErrorIs(t, err, ErrRotationRecordNotKept)
		assert.Contains(t, errors.FlattenHints(err), hint)
	}
}

func TestRotationStore_NoConfigDirKeepsTheRecordInMemory(t *testing.T) {
	t.Parallel()

	store := newRotationStore(afero.NewMemMapFs(), "")
	require.NoError(t, store.Save(context.Background(), verify.RotationRecord{Epoch: 2}))

	rec, err := store.Load(context.Background())
	require.NoError(t, err)
	assert.Equal(t, uint64(2), rec.Epoch)
}

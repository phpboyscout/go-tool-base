package setup

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

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

// Revocation is permanent (spec 0101 D20). ApplyRotation does not serialise
// Load and Save, so a record built from an older load could drop a revoke
// another apply stored, and Save is the last place to refuse it.
func TestRotationStore_SaveRefusesARecordThatDropsARevoke(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	store := newRotationStore(fs, rotationTestDir)
	ctx := context.Background()
	require.NoError(t, store.Save(ctx, verify.RotationRecord{Epoch: 2, Revoked: []string{"ABCD", "EF01"}}))

	err := store.Save(ctx, verify.RotationRecord{Epoch: 3, Revoked: []string{"EF01"}})
	require.ErrorIs(t, err, verify.ErrRotationRefused)
	require.ErrorContains(t, err, "ABCD")

	got, err := store.Load(ctx)
	require.NoError(t, err)
	assert.Equal(t, verify.RotationRecord{Epoch: 2, Revoked: []string{"ABCD", "EF01"}}, got, "nothing is written")

	require.NoError(t, store.Save(ctx, verify.RotationRecord{Epoch: 3, Revoked: []string{"ef 01", "abcd", "1234"}}),
		"a record keeping every stored revoke, in any case or spacing, may add one")
	require.NoError(t, store.Save(ctx, verify.RotationRecord{Epoch: 4, Revoked: []string{"EF01", "ABCD", "1234"}}),
		"a stored revoke in another case or spacing is still kept")
}

func TestRotationStore_SaveNamesEveryRevokeItWouldDrop(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		epoch   uint64
		next    []string
		dropped []string
	}{
		"the first":   {2, []string{"BBBB", "CCCC"}, []string{"AAAA"}},
		"a later":     {2, []string{"AAAA", "CCCC"}, []string{"BBBB"}},
		"the last":    {2, []string{"AAAA", "BBBB"}, []string{"CCCC"}},
		"two of them": {2, []string{"BBBB"}, []string{"AAAA", "CCCC"}},
		"all of them": {2, nil, []string{"AAAA", "BBBB", "CCCC"}},
		// An install that skipped releases meets a manifest several epochs on
		// (spec 0101 D12).
		"several epochs on": {5, []string{"AAAA", "CCCC"}, []string{"BBBB"}},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			store := newRotationStore(afero.NewMemMapFs(), rotationTestDir)
			require.NoError(t, store.Save(t.Context(), verify.RotationRecord{Epoch: 1, Revoked: []string{"AAAA", "BBBB", "CCCC"}}))

			err := store.Save(t.Context(), verify.RotationRecord{Epoch: tc.epoch, Revoked: tc.next})
			require.ErrorIs(t, err, verify.ErrRotationRefused)
			require.ErrorContains(t, err, "drops the stored revoke of "+strings.Join(tc.dropped, ", "))
		})
	}
}

// slowReadFs pauses each open of the rotation record, so unserialised Saves
// all read it before any of them writes it.
type slowReadFs struct{ afero.Fs }

func (f slowReadFs) Open(name string) (afero.File, error) {
	if filepath.Base(name) == rotationStoreFilename {
		time.Sleep(20 * time.Millisecond)
	}

	return f.Fs.Open(name)
}

// Saves racing in one process each read the stored record, so without the
// store's lock two of them could both succeed, the later one dropping the
// earlier one's revoke. Exactly one of these records can follow the stored one.
func TestRotationStore_RacingSavesKeepEveryRevoke(t *testing.T) {
	t.Parallel()

	fs, dir := slowReadFs{afero.NewOsFs()}, t.TempDir()
	ctx := t.Context()
	require.NoError(t, newRotationStore(fs, dir).Save(ctx, verify.RotationRecord{Epoch: 1, Revoked: []string{"BASE"}}))

	const racers = 16

	var (
		wg    sync.WaitGroup
		mu    sync.Mutex
		wins  []verify.RotationRecord
		start = make(chan struct{})
	)

	for i := range uint64(racers) {
		store := newRotationStore(fs, dir)

		wg.Add(1)

		go func() {
			defer wg.Done()
			<-start

			rec := verify.RotationRecord{Epoch: i + 2, Revoked: []string{"BASE", fmt.Sprintf("R%02d", i)}}
			if store.Save(ctx, rec) == nil {
				mu.Lock()
				wins = append(wins, rec)
				mu.Unlock()
			}
		}()
	}

	close(start)
	wg.Wait()

	got, err := newRotationStore(fs, dir).Load(ctx)
	require.NoError(t, err)
	require.NotEmpty(t, wins)
	assert.Contains(t, wins, got)

	for _, won := range wins {
		for _, fp := range won.Revoked {
			assert.Contains(t, got.Revoked, fp, "a saved revoke was lost")
		}
	}
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

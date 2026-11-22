package setup

import (
	"context"
	"encoding/json"
	iofs "io/fs"
	"path/filepath"
	"slices"
	"sync"

	"github.com/spf13/afero"

	"gitlab.com/phpboyscout/go/errors"
	"gitlab.com/phpboyscout/go/signing/verify"
)

// rotationStoreFilename is the applied rotation record, beside the trust store
// in the tool's config directory (spec 0101 D11).
const rotationStoreFilename = "rotation.json"

// ErrRotationStoreCorrupt is a rotation record that cannot be read, or that
// holds a key that does not parse. It is never treated as empty, because the
// empty record forgets every revoke.
var ErrRotationStoreCorrupt = errors.NewSentinel("gtb.setup.rotation_store_corrupt",
	"the rotation record cannot be read, or holds a key that does not parse")

// rotationStoreMu serialises every file store's read-compare-write, since two
// updaters in one process each build their own store over the same file.
var rotationStoreMu sync.Mutex

// newRotationStore is the record kept in dir. A directory that cannot be
// resolved, or one inside a git checkout, gets a store held in memory: a
// manifest still applies for the run, and nothing the repository could have
// written is read (go-tool-base#179).
func newRotationStore(fs afero.Fs, dir string) verify.RotationStore {
	if dir == "" || DirInCheckout(fs, dir) {
		return verify.NewMemoryRotationStore()
	}

	return &fileRotationStore{fs: fs, path: filepath.Join(dir, rotationStoreFilename)}
}

// CheckRotationRecord loads the applied rotation record the updater would load
// for tool, so doctor reports a corrupt one before an update refuses on it.
func CheckRotationRecord(ctx context.Context, fs afero.Fs, tool string) error {
	_, err := newRotationStore(fs, GetDefaultConfigDir(fs, tool)).Load(ctx)

	return err
}

type fileRotationStore struct {
	fs   afero.Fs
	path string
}

func (s *fileRotationStore) Load(context.Context) (verify.RotationRecord, error) {
	rotationStoreMu.Lock()
	defer rotationStoreMu.Unlock()

	return s.read()
}

// Save refuses a lower epoch, and other keys at the stored epoch, as the
// RotationStore contract requires. The record is written to a temporary file
// and renamed over the old one, so a crash leaves one record or the other.
func (s *fileRotationStore) Save(_ context.Context, rec verify.RotationRecord) error {
	rotationStoreMu.Lock()
	defer rotationStoreMu.Unlock()

	stored, err := s.read()
	if err != nil {
		return err
	}

	if rec.Epoch < stored.Epoch {
		return errors.Wrapf(verify.ErrRotationReplayed, "epoch %d, stored epoch is %d", rec.Epoch, stored.Epoch)
	}

	if rec.Epoch == stored.Epoch && !sameRotationKeys(rec, stored) {
		return errors.Wrapf(verify.ErrRotationRefused, "epoch %d is already stored with different keys", rec.Epoch)
	}

	data, err := json.Marshal(rec)
	if err != nil {
		return errors.Wrap(err, "encoding the rotation record")
	}

	if err := s.fs.MkdirAll(filepath.Dir(s.path), dirPermTrust); err != nil {
		return errors.Wrap(err, "creating the config directory for the rotation record")
	}

	tmp := s.path + ".tmp"
	if err := afero.WriteFile(s.fs, tmp, data, filePermTrust); err != nil {
		return errors.Wrap(err, "writing the rotation record")
	}

	return errors.Wrap(s.fs.Rename(tmp, s.path), "replacing the rotation record")
}

func (s *fileRotationStore) read() (verify.RotationRecord, error) {
	data, err := afero.ReadFile(s.fs, s.path)
	if errors.Is(err, iofs.ErrNotExist) {
		return verify.RotationRecord{}, nil
	}

	if err != nil {
		return verify.RotationRecord{}, s.corrupt(err)
	}

	var rec verify.RotationRecord
	if err := json.Unmarshal(data, &rec); err != nil {
		return verify.RotationRecord{}, s.corrupt(err)
	}

	// A key that does not parse fails rotate() at resolve time, where it would
	// read as an ordinary resolver error and be gated by require_signature.
	for i, key := range rec.Added {
		if _, err := verify.LoadTrustSet([]byte(key)); err != nil {
			return verify.RotationRecord{}, s.corrupt(errors.Wrapf(err, "stored rotation key #%d", i))
		}
	}

	return rec, nil
}

func (s *fileRotationStore) corrupt(cause error) error {
	return errors.WithHintf(errors.Wrapf(ErrRotationStoreCorrupt, "%s: %v", s.path, cause),
		"Fix or delete %s and run update again. Until the next release that carries a rotation manifest, "+
			"the keys it revoked are trusted again, so ask the tool's maintainers first.", s.path)
}

func sameRotationKeys(a, b verify.RotationRecord) bool {
	return sameSet(a.Added, b.Added) && sameSet(a.Revoked, b.Revoked)
}

func sameSet(a, b []string) bool {
	a, b = slices.Clone(a), slices.Clone(b)
	slices.Sort(a)
	slices.Sort(b)

	return slices.Equal(a, b)
}

package setup

import (
	"context"
	"encoding/json"
	iofs "io/fs"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"github.com/spf13/afero"

	"gitlab.com/phpboyscout/go/errors"
	"gitlab.com/phpboyscout/go/signing/verify"

	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

// rotationStoreFilename is the applied rotation record, beside the trust store
// in the tool's config directory (spec 0101 D11).
const rotationStoreFilename = "rotation.json"

// ErrRotationStoreCorrupt is a rotation record that cannot be read, or that
// holds a key that does not parse. It is never treated as empty, because the
// empty record forgets every revoke.
var ErrRotationStoreCorrupt = errors.NewSentinel("gtb.setup.rotation_store_corrupt",
	"the rotation record cannot be read, or holds a key that does not parse")

// ErrRotationRecordNotKept is a config directory the applied rotation record
// cannot be kept in, so a key a manifest revoked is trusted again when a release carries no manifest.
var ErrRotationRecordNotKept = errors.NewSentinel("gtb.setup.rotation_record_not_kept",
	"the rotation record is not kept")

// rotationStoreMu serialises every file store's read-compare-write, since two
// updaters in one process each build their own store over the same file. It
// does not reach another process.
var rotationStoreMu sync.Mutex

// CanKeepRotationRecord reports why the updater cannot keep or read tool's
// applied rotation record, or nil when it can.
func CanKeepRotationRecord(fs afero.Fs, tool props.Tool) error {
	return canKeepRotationRecordIn(fs, ConfigDir(tool))
}

func canKeepRotationRecordIn(fs afero.Fs, dir string) error {
	if dir == "" {
		return errors.WithHint(errors.Wrap(ErrRotationRecordNotKept, "the config directory cannot be resolved"),
			"Set HOME to a directory that exists.")
	}

	if DirInCheckout(fs, dir) {
		return errors.WithHint(errors.Wrapf(ErrRotationRecordNotKept, "%s is inside a git checkout", dir),
			"A record there could have come from the repository. Keep the config directory outside any git checkout.")
	}

	return nil
}

// newRotationStore is the record kept in dir. A directory that cannot keep one
// gets a store held in memory: a manifest still applies for the run, and
// nothing a repository could have written is read (go-tool-base#179).
func newRotationStore(fs afero.Fs, dir string) verify.RotationStore {
	if canKeepRotationRecordIn(fs, dir) != nil {
		return verify.NewMemoryRotationStore()
	}

	return &fileRotationStore{fs: fs, path: filepath.Join(dir, rotationStoreFilename)}
}

// CheckRotationRecord loads the applied rotation record the updater would load
// for tool, so doctor reports a corrupt one before an update refuses on it.
func CheckRotationRecord(ctx context.Context, fs afero.Fs, tool props.Tool) error {
	_, err := newRotationStore(fs, ConfigDir(tool)).Load(ctx)

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

// Save refuses a lower epoch, other keys at the stored epoch, and a record
// lacking a revoke it holds, because revocation is permanent (spec 0101 D20)
// and ApplyRotation does not serialise Load and Save. The record is written to
// a temporary file and renamed over the old one, so a crash leaves one record
// or the other.
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

	if dropped := droppedRevokes(stored, rec); len(dropped) > 0 {
		return errors.Wrapf(verify.ErrRotationRefused, "epoch %d drops the stored revoke of %s", rec.Epoch, strings.Join(dropped, ", "))
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

// droppedRevokes lists the stored revokes that rec leaves out.
func droppedRevokes(stored, rec verify.RotationRecord) []string {
	kept := make(map[string]bool, len(rec.Revoked))
	for _, fp := range rec.Revoked {
		kept[normalFingerprint(fp)] = true
	}

	var dropped []string

	for _, fp := range stored.Revoked {
		if !kept[normalFingerprint(fp)] {
			dropped = append(dropped, fp)
		}
	}

	return dropped
}

// normalFingerprint ignores case and spaces, as go/signing-trust's
// RotationStore contract does.
func normalFingerprint(fp string) string {
	return strings.ToUpper(strings.ReplaceAll(fp, " ", ""))
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

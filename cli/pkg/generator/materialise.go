package generator

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/afero"

	"gitlab.com/phpboyscout/go/errors"
)

// stagedFS buffers every write and removal of a multi-step generation against an
// in-memory layer over a read-through base, so the run is committed to the
// base (materialise) only once it fully succeeds, and the commit itself rolls
// back if it fails part-way. A mid-run failure therefore leaves the base tree
// (and its manifest) as it was, rather than stranding generator-written files
// whose manifest hashes were never persisted (which the next regenerate would
// misclassify as user modifications).
//
// It wraps afero.CopyOnWriteFs but differs in two ways: it records the exact set
// of paths written (so materialise copies only those, not the whole union), and
// it records — rather than EPERM-refusing — a Remove/RemoveAll of a base-only
// path, so the buffered run reproduces the direct-write deletion semantics
// (e.g. dropping signing.go when signing is disabled).
type stagedFS struct {
	afero.Fs // the copy-on-write union (reads fall through to base, writes hit layer)

	layer   afero.Fs
	base    afero.Fs
	written map[string]bool
	deleted map[string]bool
}

func newStagedFS(base afero.Fs) *stagedFS {
	layer := afero.NewMemMapFs()

	return &stagedFS{
		Fs:      afero.NewCopyOnWriteFs(base, layer),
		layer:   layer,
		base:    base,
		written: map[string]bool{},
		deleted: map[string]bool{},
	}
}

func (s *stagedFS) recordWrite(name string) {
	clean := filepath.Clean(name)
	s.written[clean] = true
	delete(s.deleted, clean)
}

func (s *stagedFS) Create(name string) (afero.File, error) {
	f, err := s.Fs.Create(name)
	if err == nil {
		s.recordWrite(name)
	}

	return f, err
}

func (s *stagedFS) OpenFile(name string, flag int, perm os.FileMode) (afero.File, error) {
	f, err := s.Fs.OpenFile(name, flag, perm)
	if err == nil && flag&(os.O_WRONLY|os.O_RDWR|os.O_CREATE|os.O_TRUNC|os.O_APPEND) != 0 {
		s.recordWrite(name)
	}

	return f, err
}

// Rename reproduces a move through the buffer: afero.CopyOnWriteFs refuses to
// rename a base-only path (EPERM), which would break the `--force` flat->Diátaxis
// docs migration. Instead the content is copied to newname (staged as a write)
// and oldname is recorded as a deletion, so materialise commits the move.
func (s *stagedFS) Rename(oldname, newname string) error {
	content, err := afero.ReadFile(s, oldname)
	if err != nil {
		return err
	}

	mode := os.FileMode(DefaultFileMode)
	if info, statErr := s.Stat(oldname); statErr == nil {
		mode = info.Mode().Perm()
	}

	if err := s.MkdirAll(filepath.Dir(newname), os.FileMode(DefaultDirMode)); err != nil {
		return err
	}

	if err := afero.WriteFile(s, newname, content, mode); err != nil {
		return err
	}

	s.recordDelete(oldname)

	return nil
}

func (s *stagedFS) Remove(name string) error {
	s.recordDelete(name)
	_ = s.layer.Remove(name)

	return nil
}

func (s *stagedFS) RemoveAll(name string) error {
	s.recordDelete(name)
	_ = s.layer.RemoveAll(name)

	return nil
}

func (s *stagedFS) recordDelete(name string) {
	clean := filepath.Clean(name)
	s.deleted[clean] = true
	delete(s.written, clean)

	// A directory removal also invalidates any staged writes/deletes beneath it.
	prefix := clean + string(filepath.Separator)
	for w := range s.written {
		if strings.HasPrefix(w, prefix) {
			delete(s.written, w)
		}
	}
}

// materialise commits the staged layer onto the base filesystem as one
// transaction: every written file is copied over (creating parents), then
// every recorded deletion that was not subsequently re-written is removed,
// each in sorted path order so a failure is reproducible. Copies precede
// deletions so a path deleted-then-rewritten within the run survives. Before
// a base path is touched its content is journaled, and a failure at any step
// restores every journaled path in reverse, so the base tree is either fully
// committed or as it was (#52). Parent directories created for a new file are
// not removed on rollback; an empty directory misclassifies nothing.
func (s *stagedFS) materialise() (err error) {
	journal := &commitJournal{base: s.base}

	defer func() {
		if err != nil {
			err = errors.Join(err, journal.rollback())
		}
	}()

	for _, name := range sortedPaths(s.written) {
		if err := journal.record(name); err != nil {
			return err
		}

		if err := s.copyStagedFile(name); err != nil {
			return err
		}
	}

	for _, name := range sortedPaths(s.deleted) {
		if err := journal.record(name); err != nil {
			return err
		}

		if err := s.base.RemoveAll(name); err != nil && !os.IsNotExist(err) {
			return errors.Wrapf(err, "failed to remove %s", name)
		}
	}

	return nil
}

func sortedPaths(set map[string]bool) []string {
	paths := make([]string, 0, len(set))
	for name := range set {
		paths = append(paths, name)
	}

	sort.Strings(paths)

	return paths
}

// copyStagedFile writes one staged path to the base. A recorded write the
// layer cannot produce is a bookkeeping fault, not a file to skip: the one
// legitimate way a staged write vanishes, a later directory removal, is
// already struck from the record by recordDelete.
func (s *stagedFS) copyStagedFile(name string) error {
	content, err := afero.ReadFile(s.layer, name)
	if err != nil {
		return errors.Wrapf(err, "staged write %s is not readable", name)
	}

	mode := os.FileMode(DefaultFileMode)
	if info, statErr := s.layer.Stat(name); statErr == nil {
		mode = info.Mode().Perm()
	}

	if err := s.base.MkdirAll(filepath.Dir(name), os.FileMode(DefaultDirMode)); err != nil {
		return errors.Wrapf(err, "failed to create directory for %s", name)
	}

	if err := afero.WriteFile(s.base, name, content, mode); err != nil {
		return errors.Wrapf(err, "failed to write %s", name)
	}

	return nil
}

// commitJournal is what materialise knows about the base tree before it
// changed it: the prior content and mode of every file under each touched
// path, or that the path was absent. rollback replays it newest first.
type commitJournal struct {
	base    afero.Fs
	entries []journalEntry
}

type journalEntry struct {
	path    string
	content []byte
	mode    os.FileMode
	absent  bool
}

// record journals a path about to be written or removed. A directory is
// journaled file by file so a RemoveAll can be undone.
func (j *commitJournal) record(name string) error {
	info, err := j.base.Stat(name)
	if os.IsNotExist(err) {
		j.entries = append(j.entries, journalEntry{path: name, absent: true})

		return nil
	}

	if err != nil {
		return errors.Wrapf(err, "failed to journal %s", name)
	}

	if !info.IsDir() {
		return j.recordFile(name, info.Mode().Perm())
	}

	return afero.Walk(j.base, name, func(path string, fi os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return errors.Wrapf(walkErr, "failed to journal %s", path)
		}

		if fi.IsDir() {
			return nil
		}

		return j.recordFile(path, fi.Mode().Perm())
	})
}

func (j *commitJournal) recordFile(name string, mode os.FileMode) error {
	content, err := afero.ReadFile(j.base, name)
	if err != nil {
		return errors.Wrapf(err, "failed to journal %s", name)
	}

	j.entries = append(j.entries, journalEntry{path: name, content: content, mode: mode})

	return nil
}

// rollback restores the journaled state, newest entry first, and reports
// every path it could not restore.
func (j *commitJournal) rollback() error {
	var errs []error

	for i := len(j.entries) - 1; i >= 0; i-- {
		e := j.entries[i]

		var err error
		if e.absent {
			err = j.base.RemoveAll(e.path)
		} else if err = j.base.MkdirAll(filepath.Dir(e.path), os.FileMode(DefaultDirMode)); err == nil {
			err = afero.WriteFile(j.base, e.path, e.content, e.mode)
		}

		if err != nil && !os.IsNotExist(err) {
			errs = append(errs, errors.Wrapf(err, "rollback of %s failed", e.path))
		}
	}

	return errors.Join(errs...)
}

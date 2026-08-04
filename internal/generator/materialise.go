package generator

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/afero"

	"gitlab.com/phpboyscout/go/errors"
)

// stagedFS buffers every write and removal of a multi-step generation against an
// in-memory layer over a read-through base, so the run can be committed to the
// base atomically (materialise) only once it fully succeeds — a mid-run failure
// leaves the base tree (and its manifest) untouched, rather than stranding
// generator-written files whose manifest hashes were never persisted (which the
// next regenerate would misclassify as user modifications).
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

// materialise commits the staged layer onto the base filesystem: every written
// file is copied over (creating parents), then every recorded deletion that was
// not subsequently re-written is removed. Copies precede deletions so a
// path deleted-then-rewritten within the run survives.
func (s *stagedFS) materialise() error {
	for name := range s.written {
		if err := s.copyStagedFile(name); err != nil {
			return err
		}
	}

	for name := range s.deleted {
		if err := s.base.RemoveAll(name); err != nil && !os.IsNotExist(err) {
			return errors.Wrapf(err, "failed to remove %s", name)
		}
	}

	return nil
}

func (s *stagedFS) copyStagedFile(name string) error {
	content, err := afero.ReadFile(s.layer, name)
	if err != nil {
		// The path was staged for write but is no longer readable from the layer
		// (e.g. superseded by a later directory removal). Nothing to commit.
		return nil //nolint:nilerr // a vanished staged write is not a commit error
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

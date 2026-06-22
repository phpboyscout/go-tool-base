// Package aferobilly adapts a go-billy/v5 Filesystem to an afero.Fs. It is the
// pure, reusable bridge behind pkg/vcs/repo's worktree-as-afero accessors, but
// works for any billy filesystem (memfs, osfs, chroot).
//
// Safety: every Fs and File operation is serialised through a configurable
// sync.Locker (default: a no-op). When the locker is the mutex that also guards
// the producer of the filesystem — e.g. a ThreadSafeRepo's worktree — a live
// afero.Fs handle is genuinely safe for concurrent use: each operation re-locks,
// so the handle can never touch the underlying filesystem unsynchronised.
//
// The returned afero.Fs/File expose no accessor for the wrapped billy object, so
// the lock boundary cannot be bypassed. A handle (and its open files) must NOT be
// used from inside a critical section that already holds the same locker (the
// repo mutex is non-reentrant — that would deadlock).
package aferobilly

import (
	"errors"
	"io"
	"os"
	"sync"
	"time"

	"github.com/go-git/go-billy/v5"
	bilutil "github.com/go-git/go-billy/v5/util"
	"github.com/spf13/afero"
)

// errIsDirectory backs the os.PathError returned by byte-level operations on a
// directory handle (billy cannot Open a directory, so the adapter synthesises a
// read-only directory file that only supports Readdir/Stat).
var errIsDirectory = errors.New("is a directory")

// errNotDirectory backs Readdir on a regular file — billy's ReadDir returns an
// empty list for a non-directory, but afero/os require an error.
var errNotDirectory = errors.New("not a directory")

// Compile-time assertions: the adapter is a full afero.Fs and implements the
// optional symlink interfaces; both file and directory handles are afero.Files.
var (
	_ afero.Fs        = (*billyFs)(nil)
	_ afero.Lstater   = (*billyFs)(nil)
	_ afero.Symlinker = (*billyFs)(nil)
	_ afero.File      = (*billyFile)(nil)
	_ afero.File      = (*billyDir)(nil)
)

// noopLocker is the default locker: Lock/Unlock do nothing. Used for a
// single-threaded backing filesystem so the adapter works without a real mutex.
// It is never nil (a nil sync.Locker panics on Lock).
type noopLocker struct{}

func (noopLocker) Lock()   {}
func (noopLocker) Unlock() {}

// Option configures the adapter.
type Option func(*billyFs)

// WithLocker serialises every Fs and File operation through l. Pass the mutex
// that guards the filesystem's producer (e.g. a ThreadSafeRepo's mutex) to make
// a live handle concurrency-safe. A nil locker is ignored (the no-op default is
// kept).
func WithLocker(l sync.Locker) Option {
	return func(f *billyFs) {
		if l != nil {
			f.mu = l
		}
	}
}

// New returns an afero.Fs backed by bfs. See the package doc for the locking
// contract.
func New(bfs billy.Filesystem, opts ...Option) afero.Fs {
	label := bfs.Root()
	if label == "" {
		label = "aferobilly"
	}

	f := &billyFs{b: bfs, mu: noopLocker{}, label: label}
	for _, o := range opts {
		o(f)
	}

	return f
}

type billyFs struct {
	b     billy.Filesystem
	mu    sync.Locker
	label string // stable Name() label, resolved once at construction
}

func (a *billyFs) Create(name string) (afero.File, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	bf, err := a.b.Create(name)

	return a.wrap(name, bf, err)
}

// Open delegates to OpenFile (which takes the lock) so the directory-handling
// path is shared.
func (a *billyFs) Open(name string) (afero.File, error) {
	return a.OpenFile(name, os.O_RDONLY, 0)
}

// OpenFile opens name. billy cannot open a directory, so an existing directory
// is returned as a synthesised read-only directory handle; everything else
// delegates to billy.
func (a *billyFs) OpenFile(name string, flag int, perm os.FileMode) (afero.File, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if fi, err := a.b.Stat(name); err == nil && fi.IsDir() {
		return &billyDir{fs: a.b, mu: a.mu, name: name}, nil
	}

	bf, err := a.b.OpenFile(name, flag, perm)

	return a.wrap(name, bf, err)
}

// Mkdir maps to billy's MkdirAll (billy has no plain Mkdir). Per the spec
// (OQ-4) this means it creates parents and is idempotent — it does NOT reproduce
// afero's strict "fail on missing parent / existing target" semantics.
func (a *billyFs) Mkdir(name string, perm os.FileMode) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	return a.b.MkdirAll(name, perm)
}

func (a *billyFs) MkdirAll(path string, perm os.FileMode) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	return a.b.MkdirAll(path, perm)
}

func (a *billyFs) Remove(name string) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	return a.b.Remove(name)
}

func (a *billyFs) RemoveAll(path string) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	return bilutil.RemoveAll(a.b, path)
}

func (a *billyFs) Rename(oldname, newname string) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	return a.b.Rename(oldname, newname)
}

func (a *billyFs) Stat(name string) (os.FileInfo, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	return a.b.Stat(name)
}

// Name returns the stable label resolved at construction (lock-free).
func (a *billyFs) Name() string { return a.label }

// Chmod/Chown/Chtimes are no-ops: billy memfs ignores modes, and failing here
// would break afero callers that set permissions while editing a worktree.
func (a *billyFs) Chmod(string, os.FileMode) error            { return nil }
func (a *billyFs) Chown(string, int, int) error               { return nil }
func (a *billyFs) Chtimes(string, time.Time, time.Time) error { return nil }

// LstatIfPossible implements afero.Lstater. billy always supports Lstat, so the
// bool ("lstat was used") is always true.
func (a *billyFs) LstatIfPossible(name string) (os.FileInfo, bool, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	fi, err := a.b.Lstat(name)

	return fi, true, err
}

// SymlinkIfPossible implements afero.Linker. NOTE the argument-order mapping:
// afero (oldname, newname) -> billy Symlink(target=oldname, link=newname).
func (a *billyFs) SymlinkIfPossible(oldname, newname string) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	return a.b.Symlink(oldname, newname)
}

// ReadlinkIfPossible implements afero.LinkReader.
func (a *billyFs) ReadlinkIfPossible(name string) (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	return a.b.Readlink(name)
}

func (a *billyFs) wrap(name string, bf billy.File, err error) (afero.File, error) {
	if err != nil {
		return nil, err
	}

	return &billyFile{bf: bf, fs: a.b, mu: a.mu, name: name}, nil
}

// billyFile adapts a regular billy.File to afero.File. Every operation re-locks
// the shared locker, since an open handle outlives the Open call that created it.
type billyFile struct {
	bf   billy.File
	fs   billy.Filesystem
	mu   sync.Locker
	name string
}

func (f *billyFile) Name() string { return f.name }

func (f *billyFile) Read(p []byte) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.bf.Read(p)
}

func (f *billyFile) ReadAt(p []byte, off int64) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.bf.ReadAt(p, off)
}

func (f *billyFile) Write(p []byte) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.bf.Write(p)
}

// WriteAt uses the file's native io.WriterAt when available (memfs and osfs
// files both provide it, even though the billy.File interface does not require
// it), falling back to a seek-based emulation that preserves the seek offset.
func (f *billyFile) WriteAt(p []byte, off int64) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if wa, ok := f.bf.(io.WriterAt); ok {
		return wa.WriteAt(p, off)
	}

	cur, err := f.bf.Seek(0, io.SeekCurrent)
	if err != nil {
		return 0, err
	}

	if _, err := f.bf.Seek(off, io.SeekStart); err != nil {
		return 0, err
	}

	n, werr := f.bf.Write(p)

	// Restore the original offset (best-effort; the write result wins).
	if _, serr := f.bf.Seek(cur, io.SeekStart); serr != nil && werr == nil {
		return n, serr
	}

	return n, werr
}

func (f *billyFile) Seek(offset int64, whence int) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.bf.Seek(offset, whence)
}

func (f *billyFile) Truncate(size int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.bf.Truncate(size)
}

func (f *billyFile) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.bf.Close()
}

func (f *billyFile) Stat() (os.FileInfo, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.fs.Stat(f.name)
}

// Sync is a no-op: billy has no fsync.
func (f *billyFile) Sync() error { return nil }

// WriteString delegates to Write (which locks).
func (f *billyFile) WriteString(s string) (int, error) { return f.Write([]byte(s)) }

// Readdir/Readdirnames on a regular file error, matching afero/os.
func (f *billyFile) Readdir(int) ([]os.FileInfo, error) {
	return nil, &os.PathError{Op: "readdir", Path: f.name, Err: errNotDirectory}
}

func (f *billyFile) Readdirnames(int) ([]string, error) {
	return nil, &os.PathError{Op: "readdirnames", Path: f.name, Err: errNotDirectory}
}

// billyDir is the synthesised handle for a directory (billy cannot Open one). It
// supports Readdir/Stat; byte-level operations error like os.File on a directory.
type billyDir struct {
	fs   billy.Filesystem
	mu   sync.Locker
	name string

	// directory iteration snapshot (afero/os Readdir(count) pagination).
	entries []os.FileInfo
	read    bool
	offset  int
}

func (d *billyDir) Name() string { return d.name }

func (d *billyDir) dirErr(op string) error {
	return &os.PathError{Op: op, Path: d.name, Err: errIsDirectory}
}

func (d *billyDir) Read([]byte) (int, error)          { return 0, d.dirErr("read") }
func (d *billyDir) ReadAt([]byte, int64) (int, error) { return 0, d.dirErr("readat") }
func (d *billyDir) Write([]byte) (int, error)         { return 0, d.dirErr("write") }
func (d *billyDir) WriteAt([]byte, int64) (int, error) {
	return 0, d.dirErr("writeat")
}
func (d *billyDir) WriteString(string) (int, error) { return 0, d.dirErr("write") }
func (d *billyDir) Seek(int64, int) (int64, error)  { return 0, d.dirErr("seek") }
func (d *billyDir) Truncate(int64) error            { return d.dirErr("truncate") }
func (d *billyDir) Close() error                    { return nil }
func (d *billyDir) Sync() error                     { return nil }

func (d *billyDir) Stat() (os.FileInfo, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	return d.fs.Stat(d.name)
}

// Readdir snapshots the full listing once (billy's ReadDir returns it all) and
// paginates it to match os.File semantics.
func (d *billyDir) Readdir(count int) ([]os.FileInfo, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if !d.read {
		entries, err := d.fs.ReadDir(d.name)
		if err != nil {
			return nil, err
		}

		d.entries = entries
		d.read = true
	}

	remaining := d.entries[d.offset:]

	if count <= 0 {
		d.offset = len(d.entries)

		return remaining, nil
	}

	if len(remaining) == 0 {
		return nil, io.EOF
	}

	if count > len(remaining) {
		count = len(remaining)
	}

	d.offset += count

	return remaining[:count], nil
}

func (d *billyDir) Readdirnames(n int) ([]string, error) {
	infos, err := d.Readdir(n)
	if err != nil {
		return nil, err
	}

	names := make([]string, len(infos))
	for i, fi := range infos {
		names[i] = fi.Name()
	}

	return names, nil
}

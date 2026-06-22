package aferobilly

import (
	"fmt"
	"io"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-git/go-billy/v5"
	"github.com/go-git/go-billy/v5/memfs"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newFS() afero.Fs { return New(memfs.New()) }

func TestRoundTrip_WriteReadStat(t *testing.T) {
	t.Parallel()

	fs := newFS()

	require.NoError(t, afero.WriteFile(fs, "/dir/sub/file.txt", []byte("hello world"), 0o644))

	data, err := afero.ReadFile(fs, "/dir/sub/file.txt")
	require.NoError(t, err)
	assert.Equal(t, "hello world", string(data))

	fi, err := fs.Stat("/dir/sub/file.txt")
	require.NoError(t, err)
	assert.Equal(t, int64(11), fi.Size())
	assert.Equal(t, "file.txt", fi.Name())
}

func TestReadAt_WriteAt(t *testing.T) {
	t.Parallel()

	fs := newFS()
	require.NoError(t, afero.WriteFile(fs, "/f", []byte("hello world"), 0o644))

	f, err := fs.OpenFile("/f", os.O_RDWR, 0o644)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	buf := make([]byte, 5)
	n, err := f.ReadAt(buf, 6)
	require.NoError(t, err)
	assert.Equal(t, "world", string(buf[:n]))

	// WriteAt must not disturb the seek offset.
	require.NoError(t, mustSeek(f, 3))
	_, err = f.WriteAt([]byte("WORLD"), 6)
	require.NoError(t, err)
	at, err := f.Seek(0, io.SeekCurrent)
	require.NoError(t, err)
	assert.Equal(t, int64(3), at, "WriteAt preserved the seek offset")

	all, err := afero.ReadFile(fs, "/f")
	require.NoError(t, err)
	assert.Equal(t, "hello WORLD", string(all))
}

func TestTruncateAndSeek(t *testing.T) {
	t.Parallel()

	fs := newFS()
	require.NoError(t, afero.WriteFile(fs, "/f", []byte("0123456789"), 0o644))

	f, err := fs.OpenFile("/f", os.O_RDWR, 0o644)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	require.NoError(t, f.Truncate(4))
	off, err := f.Seek(2, io.SeekStart)
	require.NoError(t, err)
	assert.Equal(t, int64(2), off)

	rest, err := io.ReadAll(f)
	require.NoError(t, err)
	assert.Equal(t, "23", string(rest))
}

func TestReaddir_Pagination(t *testing.T) {
	t.Parallel()

	fs := newFS()
	require.NoError(t, fs.MkdirAll("/d", 0o755))
	for _, n := range []string{"a", "b", "c"} {
		require.NoError(t, afero.WriteFile(fs, "/d/"+n, []byte("x"), 0o644))
	}

	// Readdir(-1) returns all.
	d, err := fs.Open("/d")
	require.NoError(t, err)
	all, err := d.Readdir(-1)
	require.NoError(t, err)
	assert.Len(t, all, 3)
	require.NoError(t, d.Close())

	// Paginated: 2 then 1, then io.EOF.
	d2, err := fs.Open("/d")
	require.NoError(t, err)
	defer func() { _ = d2.Close() }()
	first, err := d2.Readdir(2)
	require.NoError(t, err)
	assert.Len(t, first, 2)
	second, err := d2.Readdir(2)
	require.NoError(t, err)
	assert.Len(t, second, 1)
	_, err = d2.Readdir(2)
	require.ErrorIs(t, err, io.EOF)

	names, err := fs.Open("/d")
	require.NoError(t, err)
	defer func() { _ = names.Close() }()
	got, err := names.Readdirnames(-1)
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"a", "b", "c"}, got)
}

func TestRenameRemoveRemoveAll(t *testing.T) {
	t.Parallel()

	fs := newFS()
	require.NoError(t, afero.WriteFile(fs, "/a", []byte("x"), 0o644))

	require.NoError(t, fs.Rename("/a", "/b"))
	_, err := fs.Stat("/a")
	require.ErrorIs(t, err, os.ErrNotExist)
	_, err = fs.Stat("/b")
	require.NoError(t, err)

	require.NoError(t, fs.Remove("/b"))
	_, err = fs.Stat("/b")
	require.ErrorIs(t, err, os.ErrNotExist)

	require.NoError(t, afero.WriteFile(fs, "/tree/x/y", []byte("x"), 0o644))
	require.NoError(t, fs.RemoveAll("/tree"))
	_, err = fs.Stat("/tree/x/y")
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestMkdir_MapsToMkdirAll(t *testing.T) {
	t.Parallel()

	fs := newFS()
	// Per OQ-4, Mkdir creates parents (unlike strict afero Mkdir).
	require.NoError(t, fs.Mkdir("/x/y/z", 0o755))
	fi, err := fs.Stat("/x/y/z")
	require.NoError(t, err)
	assert.True(t, fi.IsDir())
}

func TestChmodChownChtimes_AreNoops(t *testing.T) {
	t.Parallel()

	fs := newFS()
	require.NoError(t, afero.WriteFile(fs, "/f", []byte("x"), 0o644))

	assert.NoError(t, fs.Chmod("/f", 0o600))
	assert.NoError(t, fs.Chown("/f", 1, 1))
	assert.NoError(t, fs.Chtimes("/f", time.Time{}, time.Time{}))
}

func TestSymlinkRoundTrip(t *testing.T) {
	t.Parallel()

	fs := newFS()
	require.NoError(t, afero.WriteFile(fs, "/target", []byte("payload"), 0o644))

	symlinker, ok := fs.(afero.Symlinker)
	require.True(t, ok, "adapter must advertise afero.Symlinker")

	require.NoError(t, symlinker.SymlinkIfPossible("/target", "/link"))

	dst, err := symlinker.ReadlinkIfPossible("/link")
	require.NoError(t, err)
	assert.Equal(t, "/target", dst)

	fi, used, err := symlinker.LstatIfPossible("/link")
	require.NoError(t, err)
	assert.True(t, used, "lstat was used")
	assert.NotZero(t, fi.Mode()&os.ModeSymlink, "Lstat reports a symlink")
}

func TestDirectoryHandle(t *testing.T) {
	t.Parallel()

	fs := newFS()
	require.NoError(t, fs.MkdirAll("/d", 0o755))
	require.NoError(t, afero.WriteFile(fs, "/d/f", []byte("x"), 0o644))

	d, err := fs.Open("/d")
	require.NoError(t, err)

	assert.Equal(t, "/d", d.Name())

	// Byte-level operations on a directory error; Readdir works; Close is nil.
	_, err = d.Read(make([]byte, 1))
	require.ErrorIs(t, err, errIsDirectory)
	_, err = d.ReadAt(make([]byte, 1), 0)
	require.ErrorIs(t, err, errIsDirectory)
	_, err = d.Write([]byte("x"))
	require.ErrorIs(t, err, errIsDirectory)
	_, err = d.WriteAt([]byte("x"), 0)
	require.ErrorIs(t, err, errIsDirectory)
	_, err = d.Seek(0, io.SeekStart)
	require.ErrorIs(t, err, errIsDirectory)
	require.ErrorIs(t, d.Truncate(0), errIsDirectory)
	_, err = d.WriteString("x")
	require.ErrorIs(t, err, errIsDirectory)

	// Stat/Readdir work; Sync/Close are no-ops.
	fi, err := d.Stat()
	require.NoError(t, err)
	assert.True(t, fi.IsDir())

	entries, err := d.Readdir(-1)
	require.NoError(t, err)
	assert.Len(t, entries, 1)
	assert.NoError(t, d.Sync())
	assert.NoError(t, d.Close())
}

func TestFileMiscOps(t *testing.T) {
	t.Parallel()

	fs := newFS()
	f, err := fs.Create("/f")
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	assert.Equal(t, "/f", f.Name())

	n, err := f.WriteString("hello")
	require.NoError(t, err)
	assert.Equal(t, 5, n)
	require.NoError(t, f.Sync()) // no-op

	data, err := afero.ReadFile(fs, "/f")
	require.NoError(t, err)
	assert.Equal(t, "hello", string(data))
}

func TestOpenMissing_Errors(t *testing.T) {
	t.Parallel()

	fs := newFS()
	_, err := fs.Open("/nope")
	require.Error(t, err, "opening a missing file surfaces the billy error (wrap error path)")
}

func TestReaddirnames_OnRegularFileErrors(t *testing.T) {
	t.Parallel()

	fs := newFS()
	require.NoError(t, afero.WriteFile(fs, "/f", []byte("x"), 0o644))

	f, err := fs.Open("/f")
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	_, err = f.Readdir(-1)
	require.ErrorIs(t, err, errNotDirectory, "Readdir on a regular file must error")
	_, err = f.Readdirnames(-1)
	require.ErrorIs(t, err, errNotDirectory, "Readdirnames on a regular file must error")
}

func TestName_FallbackLabel(t *testing.T) {
	t.Parallel()

	// A chroot at "/" reports Root() "/"; assert Name() returns a stable label.
	fs := newFS()
	assert.NotEmpty(t, fs.Name())
}

// noWriteAtFile hides io.WriterAt to force the seek-based WriteAt emulation.
type noWriteAtFile struct{ billy.File }

func TestWriteAt_SeekFallback(t *testing.T) {
	t.Parallel()

	bfs := memfs.New()
	require.NoError(t, writeBilly(bfs, "/f", "hello world"))

	bf, err := bfs.OpenFile("/f", os.O_RDWR, 0o644)
	require.NoError(t, err)

	// Construct a file whose bf does NOT satisfy io.WriterAt.
	af := &billyFile{bf: noWriteAtFile{bf}, fs: bfs, mu: noopLocker{}, name: "/f"}
	_, isWriterAt := af.bf.(io.WriterAt)
	require.False(t, isWriterAt, "test fixture must not expose io.WriterAt")

	_, err = af.WriteAt([]byte("WORLD"), 6)
	require.NoError(t, err)
	require.NoError(t, af.Close())

	got, err := readBilly(bfs, "/f")
	require.NoError(t, err)
	assert.Equal(t, "hello WORLD", got)
}

// countingLocker is a real mutex that also counts Lock calls.
type countingLocker struct {
	sync.Mutex
	locks atomic.Int64
}

func (c *countingLocker) Lock()   { c.locks.Add(1); c.Mutex.Lock() }
func (c *countingLocker) Unlock() { c.Mutex.Unlock() }

func TestEveryOperationLocks(t *testing.T) {
	t.Parallel()

	cl := &countingLocker{}
	fs := New(memfs.New(), WithLocker(cl))

	type op struct {
		name string
		run  func() error
	}
	ops := []op{
		{"MkdirAll", func() error { return fs.MkdirAll("/d", 0o755) }},
		// Create's handle is intentionally not closed here so the op is a single
		// locking call (memfs leak is harmless in a unit test).
		{"Create", func() error { _, e := fs.Create("/d/f"); return e }},
		{"Stat", func() error { _, e := fs.Stat("/d/f"); return e }},
		{"Rename", func() error { return fs.Rename("/d/f", "/d/g") }},
		{"Remove", func() error { return fs.Remove("/d/g") }},
		{"Mkdir", func() error { return fs.Mkdir("/d2", 0o755) }},
		{"RemoveAll", func() error { return fs.RemoveAll("/d2") }},
		{"Lstat", func() error { _, _, e := fs.(afero.Symlinker).LstatIfPossible("/d"); return e }},
	}

	for _, o := range ops {
		before := cl.locks.Load()
		require.NoError(t, o.run(), o.name)
		assert.Equal(t, before+1, cl.locks.Load(), "op %s must lock exactly once", o.name)
	}
}

func TestConcurrentUseIsRaceFree(t *testing.T) {
	t.Parallel()

	fs := New(memfs.New(), WithLocker(&sync.Mutex{}))
	require.NoError(t, fs.MkdirAll("/d", 0o755))

	var wg sync.WaitGroup
	for i := range 16 {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			name := fmt.Sprintf("/d/f%d", i)
			_ = afero.WriteFile(fs, name, []byte("payload"), 0o644)
			_, _ = afero.ReadFile(fs, name)
			_, _ = fs.Stat(name)
		}(i)
	}
	wg.Wait()
}

// --- small billy helpers (avoid importing util just for the test) ---

func writeBilly(bfs billy.Filesystem, name, content string) error {
	f, err := bfs.Create(name)
	if err != nil {
		return err
	}
	if _, err := f.Write([]byte(content)); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

func readBilly(bfs billy.Filesystem, name string) (string, error) {
	f, err := bfs.Open(name)
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()
	b, err := io.ReadAll(f)
	return string(b), err
}

func mustSeek(f afero.File, off int64) error {
	_, err := f.Seek(off, io.SeekStart)
	return err
}

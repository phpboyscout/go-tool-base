package generator

import (
	"context"
	"os"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

// refusingWriteFs is a base filesystem that refuses to open one path for
// writing, standing in for a full disk or a permission failure mid-commit.
type refusingWriteFs struct {
	afero.Fs

	refuse string
}

func (f refusingWriteFs) OpenFile(name string, flag int, perm os.FileMode) (afero.File, error) {
	if name == f.refuse && flag&(os.O_WRONLY|os.O_RDWR) != 0 {
		return nil, assert.AnError
	}

	return f.Fs.OpenFile(name, flag, perm)
}

func (f refusingWriteFs) Create(name string) (afero.File, error) {
	if name == f.refuse {
		return nil, assert.AnError
	}

	return f.Fs.Create(name)
}

func readOrAbsent(t *testing.T, fs afero.Fs, name string) string {
	t.Helper()

	data, err := afero.ReadFile(fs, name)
	if os.IsNotExist(err) {
		return "<absent>"
	}

	require.NoError(t, err)

	return string(data)
}

// TestStagedFS_MaterialiseRollsBackAFailedCommit is the transaction the
// staged overlay claims (#52): a copy that fails part-way through the commit
// leaves the base tree exactly as it was, including files already copied,
// files created and files deleted before the failure.
func TestStagedFS_MaterialiseRollsBackAFailedCommit(t *testing.T) {
	t.Parallel()

	mem := afero.NewMemMapFs()
	for name, content := range map[string]string{
		"/p/a.txt":       "a0",
		"/p/b.txt":       "b0",
		"/p/c.txt":       "c0",
		"/p/gone.txt":    "gone0",
		"/p/dir/one.txt": "one0",
		"/p/dir/two.txt": "two0",
	} {
		require.NoError(t, afero.WriteFile(mem, name, []byte(content), 0o644))
	}

	// The commit walks paths in sorted order, so c.txt fails after a.txt and
	// b.txt are copied and before the new file and the deletions.
	base := refusingWriteFs{Fs: mem, refuse: "/p/c.txt"}
	s := newStagedFS(base)

	require.NoError(t, afero.WriteFile(s, "/p/a.txt", []byte("a1"), 0o644))
	require.NoError(t, afero.WriteFile(s, "/p/b.txt", []byte("b1"), 0o644))
	require.NoError(t, afero.WriteFile(s, "/p/c.txt", []byte("c1"), 0o644))
	require.NoError(t, afero.WriteFile(s, "/p/new.txt", []byte("new"), 0o644))
	require.NoError(t, s.Remove("/p/gone.txt"))
	require.NoError(t, s.RemoveAll("/p/dir"))

	err := s.materialise()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "/p/c.txt")

	assert.Equal(t, "a0", readOrAbsent(t, mem, "/p/a.txt"), "an already-copied file is restored")
	assert.Equal(t, "b0", readOrAbsent(t, mem, "/p/b.txt"), "an already-copied file is restored")
	assert.Equal(t, "c0", readOrAbsent(t, mem, "/p/c.txt"), "the failed file keeps its original")
	assert.Equal(t, "<absent>", readOrAbsent(t, mem, "/p/new.txt"), "a created file is removed")
	assert.Equal(t, "gone0", readOrAbsent(t, mem, "/p/gone.txt"), "a deleted file is restored")
	assert.Equal(t, "one0", readOrAbsent(t, mem, "/p/dir/one.txt"), "a deleted directory is restored")
	assert.Equal(t, "two0", readOrAbsent(t, mem, "/p/dir/two.txt"), "a deleted directory is restored")
}

// TestStagedFS_MaterialiseRollsBackAFailedDeletion covers the second half of
// the commit: when a deletion fails after every copy landed, the copies are
// undone too.
func TestStagedFS_MaterialiseRollsBackAFailedDeletion(t *testing.T) {
	t.Parallel()

	mem := afero.NewMemMapFs()
	require.NoError(t, afero.WriteFile(mem, "/p/a.txt", []byte("a0"), 0o644))
	require.NoError(t, afero.WriteFile(mem, "/p/keep.txt", []byte("keep0"), 0o644))

	base := refusingRemoveFs{Fs: mem, refuse: "/p/keep.txt"}
	s := newStagedFS(base)

	require.NoError(t, afero.WriteFile(s, "/p/a.txt", []byte("a1"), 0o644))
	require.NoError(t, s.Remove("/p/keep.txt"))

	err := s.materialise()
	require.Error(t, err)

	assert.Equal(t, "a0", readOrAbsent(t, mem, "/p/a.txt"), "the copy is undone")
	assert.Equal(t, "keep0", readOrAbsent(t, mem, "/p/keep.txt"))
}

type refusingRemoveFs struct {
	afero.Fs

	refuse string
}

func (f refusingRemoveFs) RemoveAll(name string) error {
	if name == f.refuse {
		return assert.AnError
	}

	return f.Fs.RemoveAll(name)
}

// TestStagedFS_MaterialiseSucceedsInOrder pins the happy path once the commit
// is ordered: existing content replaced, new files created, deletions applied.
func TestStagedFS_MaterialiseSucceedsInOrder(t *testing.T) {
	t.Parallel()

	mem := afero.NewMemMapFs()
	require.NoError(t, afero.WriteFile(mem, "/p/a.txt", []byte("a0"), 0o644))
	require.NoError(t, afero.WriteFile(mem, "/p/gone.txt", []byte("gone0"), 0o644))

	s := newStagedFS(mem)
	require.NoError(t, afero.WriteFile(s, "/p/a.txt", []byte("a1"), 0o600))
	require.NoError(t, afero.WriteFile(s, "/p/new.txt", []byte("new"), 0o644))
	require.NoError(t, s.Remove("/p/gone.txt"))

	require.NoError(t, s.materialise())

	assert.Equal(t, "a1", readOrAbsent(t, mem, "/p/a.txt"))
	assert.Equal(t, "new", readOrAbsent(t, mem, "/p/new.txt"))
	assert.Equal(t, "<absent>", readOrAbsent(t, mem, "/p/gone.txt"))
}

// TestStagedFS_UnreadableStagedWriteIsAnError: a path recorded as written
// that the layer can no longer produce is a bug in the bookkeeping, not a
// file to skip silently (#52). The one legitimate way a staged write vanishes,
// a later directory removal, is already struck from the record by
// recordDelete, so anything left unreadable here is an error.
func TestStagedFS_UnreadableStagedWriteIsAnError(t *testing.T) {
	t.Parallel()

	mem := afero.NewMemMapFs()
	s := newStagedFS(mem)

	require.NoError(t, s.MkdirAll("/p", 0o755))
	require.NoError(t, afero.WriteFile(s, "/p/x.txt", []byte("x"), 0o644))
	require.NoError(t, s.layer.Remove("/p/x.txt"))

	err := s.materialise()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "/p/x.txt")
}

// TestWithStagedFS_RestoresPropsFSAfterPanic: the staged overlay is installed
// on the shared Props.FS for the duration of the buffered run, and a panic
// inside that run must not leave the process reading through the overlay
// (#52).
func TestWithStagedFS_RestoresPropsFSAfterPanic(t *testing.T) {
	t.Parallel()

	base := afero.NewMemMapFs()
	p := &props.Props{FS: base, Logger: logger.NewNoop(), Tool: props.Tool{Name: "demo"}}
	g := New(p, &Config{Path: "/p"})

	func() {
		defer func() {
			require.NotNil(t, recover(), "the panic propagates")
		}()

		_, _ = g.withStagedFS(context.Background(), func(context.Context) error {
			panic("mid-run")
		})
	}()

	assert.Same(t, base, p.FS, "Props.FS is the base again after the panic")
}

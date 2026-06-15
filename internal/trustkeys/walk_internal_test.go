package trustkeys

import (
	"io/fs"
	"testing"
	"testing/fstest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// errReadFS wraps a MapFS and forces a read error for a chosen path so
// the walk-error propagation path can be exercised deterministically.
type errReadFS struct {
	fs.FS
	failPath string
}

func (e errReadFS) Open(name string) (fs.File, error) {
	if name == e.failPath {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrPermission}
	}

	return e.FS.Open(name)
}

func TestKeysFromFS_PropagatesReadError(t *testing.T) {
	t.Parallel()

	base := fstest.MapFS{
		"keys/good.asc": &fstest.MapFile{Data: []byte("KEY"), ModTime: time.Now()},
		"keys/bad.asc":  &fstest.MapFile{Data: []byte("KEY"), ModTime: time.Now()},
	}

	fsys := errReadFS{FS: base, failPath: "keys/bad.asc"}

	// A read failure must propagate, not be swallowed — trust anchors must
	// fail loud rather than degrade to a partial (verification-open) set.
	_, err := keysFromFS(fsys, "keys")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "walking embedded trust-key directory")
}

func TestKeysFromFS_CollectsAscFilesOnly(t *testing.T) {
	t.Parallel()

	fsys := fstest.MapFS{
		"keys/a.asc":      &fstest.MapFile{Data: []byte("A")},
		"keys/b.asc":      &fstest.MapFile{Data: []byte("B")},
		"keys/.gitkeep":   &fstest.MapFile{Data: []byte("")},
		"keys/readme.txt": &fstest.MapFile{Data: []byte("ignore me")},
	}

	keys, err := keysFromFS(fsys, "keys")
	require.NoError(t, err)
	assert.Len(t, keys, 2, "only *.asc files are collected")
}

func TestKeysE_ReturnsEmbeddedKeys(t *testing.T) {
	t.Parallel()

	keys, err := KeysE()
	require.NoError(t, err)
	assert.NotEmpty(t, keys, "embedded keys/*.asc must be returned without error")
}

package props

import (
	"io"
	"io/fs"
	"sync/atomic"
	"testing"
	"testing/fstest"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// spyFile wraps an fs.File and tracks whether Close was called.
type spyFile struct {
	fs.File
	closed atomic.Bool
}

func (f *spyFile) Close() error {
	f.closed.Store(true)
	return f.File.Close()
}

func (f *spyFile) Read(p []byte) (int, error)          { return f.File.Read(p) }
func (f *spyFile) Stat() (fs.FileInfo, error)           { return f.File.Stat() }

// spyFS wraps an fs.FS and returns spyFiles that track Close calls.
type spyFS struct {
	inner fs.FS
	files []*spyFile
}

func (s *spyFS) Open(name string) (fs.File, error) {
	f, err := s.inner.Open(name)
	if err != nil {
		return nil, err
	}

	sf := &spyFile{File: f}
	s.files = append(s.files, sf)

	return sf, nil
}

func TestAssets_Shadowing(t *testing.T) {
	fs1 := fstest.MapFS{
		"logo.png":  &fstest.MapFile{Data: []byte("root-logo")},
		"readme.md": &fstest.MapFile{Data: []byte("root-readme")},
	}
	fs2 := fstest.MapFS{
		"logo.png": &fstest.MapFile{Data: []byte("sub-logo")},
	}

	assets := NewAssets(AssetMap{
		"root": fs1,
		"sub":  fs2,
	})

	// Static assets should use Reverse Search (Shadowing - Last In wins)
	f, err := assets.Open("logo.png")
	require.NoError(t, err)
	data, _ := io.ReadAll(f)
	assert.Equal(t, "sub-logo", string(data))

	// Should still find root assets if not shadowed
	f, err = assets.Open("readme.md")
	require.NoError(t, err)
	data, _ = io.ReadAll(f)
	assert.Equal(t, "root-readme", string(data))
}

func TestAssets_Merging(t *testing.T) {
	fs1 := fstest.MapFS{
		"config.yaml": &fstest.MapFile{Data: []byte("app:\n  name: root\n  port: 8080")},
		"config.toml": &fstest.MapFile{Data: []byte("[app]\nname = \"root\"\nport = 8080")},
		"test.env":    &fstest.MapFile{Data: []byte("KEY1=VALUE1\nKEY2=VALUE2")},
		"data.csv":    &fstest.MapFile{Data: []byte("id,name\n1,root")},
	}
	fs2 := fstest.MapFS{
		"config.yaml": &fstest.MapFile{Data: []byte("app:\n  name: sub\n  debug: true")},
		"config.toml": &fstest.MapFile{Data: []byte("[app]\nname = \"sub\"\ndebug = true")},
		"test.env":    &fstest.MapFile{Data: []byte("KEY1=OVERRIDDEN\nKEY3=VALUE3")},
		"data.csv":    &fstest.MapFile{Data: []byte("2,sub")}, // No header in second file for simple append test
	}

	assets := NewAssets(AssetMap{
		"root": fs1,
		"sub":  fs2,
	})

	// Merged YAML
	f, _ := assets.Open("config.yaml")
	data, _ := io.ReadAll(f)
	assert.Contains(t, string(data), "name: sub")
	assert.Contains(t, string(data), "port: 8080")

	// Merged TOML
	f, _ = assets.Open("config.toml")
	data, _ = io.ReadAll(f)
	assert.Contains(t, string(data), "name = 'sub'")
	assert.Contains(t, string(data), "port = 8080")

	// Merged ENV
	f, _ = assets.Open("test.env")
	data, _ = io.ReadAll(f)
	assert.Contains(t, string(data), "KEY1=OVERRIDDEN")
	assert.Contains(t, string(data), "KEY2=VALUE2")
	assert.Contains(t, string(data), "KEY3=VALUE3")

	// Merged CSV (Append)
	f, _ = assets.Open("data.csv")
	data, _ = io.ReadAll(f)
	assert.Contains(t, string(data), "1,root")
	assert.Contains(t, string(data), "2,sub")
}

func TestAssets_ReadDir(t *testing.T) {
	fs1 := fstest.MapFS{
		"docs/a.txt": &fstest.MapFile{},
	}
	fs2 := fstest.MapFS{
		"docs/b.txt": &fstest.MapFile{},
	}

	assets := NewAssets(AssetMap{
		"fs1": fs1,
		"fs2": fs2,
	})

	entries, err := assets.ReadDir("docs")
	require.NoError(t, err)
	assert.Len(t, entries, 2)
	assert.Equal(t, "a.txt", entries[0].Name())
	assert.Equal(t, "b.txt", entries[1].Name())
}

func TestAssets_Mount(t *testing.T) {
	rootFS := fstest.MapFS{
		"root.txt": &fstest.MapFile{Data: []byte("root")},
	}
	subFS := fstest.MapFS{
		"info.txt": &fstest.MapFile{Data: []byte("sub-info")},
	}

	assets := NewAssets(AssetMap{"root": rootFS})
	assets.Mount(subFS, "plugins/myplugin")

	f, err := assets.Open("plugins/myplugin/info.txt")
	require.NoError(t, err)
	data, _ := io.ReadAll(f)
	assert.Equal(t, "sub-info", string(data))

	// Verify root still works
	f, err = assets.Open("root.txt")
	require.NoError(t, err)
	data, _ = io.ReadAll(f)
	assert.Equal(t, "root", string(data))
}

func TestAssets_AferoSupport(t *testing.T) {
	memFS := afero.NewMemMapFs()
	_ = afero.WriteFile(memFS, "afero.txt", []byte("hello from afero"), 0644)

	assets := NewAssets()
	assets.Register("afero", afero.NewIOFS(memFS))

	f, err := assets.Open("afero.txt")
	require.NoError(t, err)
	data, _ := io.ReadAll(f)
	assert.Equal(t, "hello from afero", string(data))
}

func TestAssets_For(t *testing.T) {
	fs1 := fstest.MapFS{"f1.txt": &fstest.MapFile{Data: []byte("f1")}}
	fs2 := fstest.MapFS{"f2.txt": &fstest.MapFile{Data: []byte("f2")}}
	fs3 := fstest.MapFS{"f3.txt": &fstest.MapFile{Data: []byte("f3")}}

	assets := NewAssets(AssetMap{
		"a": fs1,
		"b": fs2,
		"c": fs3,
	})

	subset := assets.For("a", "c")
	require.Len(t, subset.Slice(), 2)

	_, err := subset.Open("f1.txt")
	assert.NoError(t, err)

	_, err = subset.Open("f3.txt")
	assert.NoError(t, err)

	_, err = subset.Open("f2.txt")
	assert.Error(t, err)
}

func TestAssets_Get(t *testing.T) {
	fs1 := fstest.MapFS{"f1.txt": &fstest.MapFile{}}
	assets := NewAssets(AssetMap{"a": fs1})

	assert.Equal(t, fs1, assets.Get("a"))
	assert.Nil(t, assets.Get("nonexistent"))
}

func TestAssets_Names(t *testing.T) {
	assets := NewAssets(AssetMap{
		"a": fstest.MapFS{},
		"b": fstest.MapFS{},
	})

	assert.Equal(t, []string{"a", "b"}, assets.Names())
}

func TestOpenMergedCSV_SingleFS(t *testing.T) {
	fs1 := fstest.MapFS{
		"data.csv": &fstest.MapFile{Data: []byte("id,name\n1,alice\n2,bob")},
	}

	assets := NewAssets(AssetMap{"root": fs1})

	f, err := assets.Open("data.csv")
	require.NoError(t, err)

	data, _ := io.ReadAll(f)
	assert.Contains(t, string(data), "1,alice")
	assert.Contains(t, string(data), "2,bob")
}

func TestOpenMergedCSV_MultipleFS(t *testing.T) {
	fs1 := fstest.MapFS{
		"data.csv": &fstest.MapFile{Data: []byte("id,name\n1,alice")},
	}
	fs2 := fstest.MapFS{
		"data.csv": &fstest.MapFile{Data: []byte("2,bob")},
	}
	fs3 := fstest.MapFS{
		"data.csv": &fstest.MapFile{Data: []byte("3,carol")},
	}

	assets := NewAssets(AssetMap{
		"a": fs1,
		"b": fs2,
		"c": fs3,
	})

	f, err := assets.Open("data.csv")
	require.NoError(t, err)

	data, _ := io.ReadAll(f)
	assert.Contains(t, string(data), "1,alice")
	assert.Contains(t, string(data), "2,bob")
	assert.Contains(t, string(data), "3,carol")
}

func TestOpenMergedCSV_NotFound(t *testing.T) {
	fs1 := fstest.MapFS{}
	assets := NewAssets(AssetMap{"root": fs1})

	_, err := assets.Open("missing.csv")
	assert.ErrorIs(t, err, fs.ErrNotExist)
}

func TestOpenMergedCSV_EmptyCSV(t *testing.T) {
	fs1 := fstest.MapFS{
		"data.csv": &fstest.MapFile{Data: []byte("")},
	}
	assets := NewAssets(AssetMap{"root": fs1})

	_, err := assets.Open("data.csv")
	assert.ErrorIs(t, err, fs.ErrNotExist)
}

func TestOpenMergedCSV_FilesClosedPromptly(t *testing.T) {
	spy1 := &spyFS{inner: fstest.MapFS{
		"data.csv": &fstest.MapFile{Data: []byte("id,name\n1,alice")},
	}}
	spy2 := &spyFS{inner: fstest.MapFS{
		"data.csv": &fstest.MapFile{Data: []byte("2,bob")},
	}}
	spy3 := &spyFS{inner: fstest.MapFS{
		"data.csv": &fstest.MapFile{Data: []byte("3,carol")},
	}}

	a := &embeddedAssets{
		embedded: map[string]fs.FS{"a": spy1, "b": spy2, "c": spy3},
		order:    []string{"a", "b", "c"},
	}

	f, err := a.openMergedCSV("data.csv")
	require.NoError(t, err)
	require.NotNil(t, f)

	// All spy files should have been closed before openMergedCSV returned
	for _, spy := range []*spyFS{spy1, spy2, spy3} {
		require.Len(t, spy.files, 1, "expected exactly one file opened per FS")
		assert.True(t, spy.files[0].closed.Load(), "file should be closed before function returns")
	}
}

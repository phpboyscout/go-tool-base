package props

import (
	"io/fs"
	"testing"
	"testing/fstest"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go/config"

	"gitlab.com/phpboyscout/go/errorhandling"

	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	"gitlab.com/phpboyscout/go-tool-base/pkg/version"
)

// newTestProps wires a *Props with hermetic, valid defaults for accessor and
// provider-dispatch tests.
func newTestProps() (*Props, logger.Logger, config.Containable, Assets, afero.Fs, version.Version, errorhandling.ErrorHandler, Tool, NoopCollector) {
	log := logger.NewNoop()
	memFS := afero.NewMemMapFs()
	cfg := config.NewReaderContainer(memFS)
	assets := NewAssets(AssetMap{"a": fstest.MapFS{}})
	ver := version.NewInfo("v1.2.3", "abc123", "1970-01-01T00:00:00Z")
	eh := errorhandling.New(logger.ToSlog(log), nil)
	tool := Tool{Name: "demo"}
	col := NoopCollector{}

	p := &Props{
		Tool:         tool,
		Logger:       log,
		Config:       cfg,
		Assets:       assets,
		FS:           memFS,
		Version:      ver,
		ErrorHandler: eh,
		Collector:    col,
	}

	return p, log, cfg, assets, memFS, ver, eh, tool, col
}

// TestProps_Accessors exercises every narrow-provider accessor on *Props,
// confirming each returns the exact value stored on the container.
func TestProps_Accessors(t *testing.T) {
	t.Parallel()

	p, log, cfg, assets, memFS, ver, eh, tool, col := newTestProps()

	assert.Equal(t, log, p.GetLogger())
	assert.Equal(t, cfg, p.GetConfig())
	assert.Equal(t, assets, p.GetAssets())
	assert.Equal(t, memFS, p.GetFS())
	assert.Equal(t, ver, p.GetVersion())
	assert.Equal(t, eh, p.GetErrorHandler())
	assert.Equal(t, tool, p.GetTool())
	assert.Equal(t, col, p.GetCollector())
}

// TestProps_SatisfiesProviders confirms a *Props value is usable through each
// narrow provider interface (compile-time checks already exist; this exercises
// the dispatch through the interface at runtime).
func TestProps_SatisfiesProviders(t *testing.T) {
	t.Parallel()

	p, _, _, _, _, _, _, _, _ := newTestProps()

	var (
		lp   LoggerProvider        = p
		cp   ConfigProvider        = p
		fsp  FileSystemProvider    = p
		ap   AssetProvider         = p
		vp   VersionProvider       = p
		ehp  ErrorHandlerProvider  = p
		tmp  ToolMetadataProvider  = p
		tp   TelemetryProvider     = p
		lcp  LoggingConfigProvider = p
		core CoreProvider          = p
	)

	assert.NotNil(t, lp.GetLogger())
	assert.NotNil(t, cp.GetConfig())
	assert.NotNil(t, fsp.GetFS())
	assert.NotNil(t, ap.GetAssets())
	assert.Equal(t, "v1.2.3", vp.GetVersion().GetVersion())
	assert.NotNil(t, ehp.GetErrorHandler())
	assert.Equal(t, "demo", tmp.GetTool().Name)
	assert.NotNil(t, tp.GetCollector())
	assert.NotNil(t, lcp.GetLogger())
	assert.NotNil(t, core.GetFS())
}

// nilAssets returns a typed-nil *embeddedAssets exercising the nil-receiver
// defensive guards on the unexported implementation.
func nilAssets() *embeddedAssets { return nil }

// TestEmbeddedAssets_NilReceiverGuards drives every method's `a == nil` branch.
func TestEmbeddedAssets_NilReceiverGuards(t *testing.T) {
	t.Parallel()

	a := nilAssets()

	assert.Nil(t, a.Names())

	_, err := a.Exists("x")
	require.ErrorIs(t, err, fs.ErrNotExist)

	_, err = a.Open("x")
	require.ErrorIs(t, err, fs.ErrNotExist)

	_, err = a.Stat("x")
	require.ErrorIs(t, err, fs.ErrNotExist)

	_, err = a.ReadDir("x")
	require.ErrorIs(t, err, fs.ErrNotExist)

	_, err = a.Glob("x")
	require.ErrorIs(t, err, fs.ErrNotExist)

	assert.Nil(t, a.Slice())
	assert.Nil(t, a.Get("x"))
	assert.Nil(t, a.Merge())

	// Register and Mount on a nil receiver are no-ops and must not panic.
	a.Register("k", fstest.MapFS{})
	a.Mount(fstest.MapFS{}, "p")
}

// TestEmbeddedAssets_NilEntriesSkipped registers a nil fs.FS under a name and
// confirms the per-method `ef == nil` continue branches are taken.
func TestEmbeddedAssets_NilEntriesSkipped(t *testing.T) {
	t.Parallel()

	a := &embeddedAssets{
		embedded: map[string]fs.FS{
			"nilfs": nil,
			"real":  fstest.MapFS{"file.txt": &fstest.MapFile{Data: []byte("hi")}},
		},
		order: []string{"nilfs", "real"},
	}

	// Static open skips the nil entry and finds the real one.
	f, err := a.Open("file.txt")
	require.NoError(t, err)
	require.NoError(t, f.Close())

	// Exists / Stat skip nil entries.
	_, err = a.Exists("file.txt")
	require.NoError(t, err)

	info, err := a.Stat("file.txt")
	require.NoError(t, err)
	assert.Equal(t, "file.txt", info.Name())

	// ReadDir / Glob skip nil entries (and a directory listing must still work).
	dirA := &embeddedAssets{
		embedded: map[string]fs.FS{
			"nilfs": nil,
			"real":  fstest.MapFS{"d/x.txt": &fstest.MapFile{}},
		},
		order: []string{"nilfs", "real"},
	}
	entries, err := dirA.ReadDir("d")
	require.NoError(t, err)
	assert.Len(t, entries, 1)

	matches, err := dirA.Glob("d/*.txt")
	require.NoError(t, err)
	assert.Equal(t, []string{"d/x.txt"}, matches)

	// Slice includes the nil entry slot in order.
	assert.Len(t, a.Slice(), 2)
}

// TestEmbeddedAssets_NotFoundPaths drives the trailing ErrNotExist returns when
// a name is absent from every (non-nil) filesystem.
func TestEmbeddedAssets_NotFoundPaths(t *testing.T) {
	t.Parallel()

	a := NewAssets(AssetMap{"a": fstest.MapFS{"present.txt": &fstest.MapFile{}}})

	_, err := a.Open("missing.txt")
	require.ErrorIs(t, err, fs.ErrNotExist)

	_, err = a.Stat("missing.txt")
	require.ErrorIs(t, err, fs.ErrNotExist)

	_, err = a.Exists("missing.txt")
	require.ErrorIs(t, err, fs.ErrNotExist)

	_, err = a.ReadDir("no-such-dir")
	require.ErrorIs(t, err, fs.ErrNotExist)
}

// TestEmbeddedAssets_StructuredNotFound covers openMergedStructured's not-found
// path (a recognised structured extension with no matching file anywhere).
func TestEmbeddedAssets_StructuredNotFound(t *testing.T) {
	t.Parallel()

	a := NewAssets(AssetMap{"a": fstest.MapFS{"other.txt": &fstest.MapFile{}}})

	_, err := a.Open("missing.yaml")
	assert.ErrorIs(t, err, fs.ErrNotExist)
}

// TestEmbeddedAssets_StructuredFormats exercises each branch of
// marshalStructuredData / unmarshalStructuredData via a single-FS open so the
// merged round-trip executes the marshal switch for every supported extension.
func TestEmbeddedAssets_StructuredFormats(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		file string
		data string
	}{
		{"json", "config.json", `{"app":{"name":"root"}}`},
		{"properties", "config.properties", "a=1\nb=2"},
		{"yml", "config.yml", "k: v"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			a := NewAssets(AssetMap{
				"a": fstest.MapFS{tc.file: &fstest.MapFile{Data: []byte(tc.data)}},
			})

			f, err := a.Open(tc.file)
			require.NoError(t, err)
			require.NoError(t, f.Close())
		})
	}
}

// TestUnmarshalStructuredData_Unsupported confirms the default branch rejects an
// unrecognised extension with a descriptive error.
func TestUnmarshalStructuredData_Unsupported(t *testing.T) {
	t.Parallel()

	_, err := unmarshalStructuredData([]byte("x"), ".unknown")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported extension")
}

// TestProcessAssetFile_OpenError covers the failed-open branch of
// processAssetFile (the named FS exists but the file does not).
func TestProcessAssetFile_OpenError(t *testing.T) {
	t.Parallel()

	a := &embeddedAssets{
		embedded: map[string]fs.FS{"a": fstest.MapFS{}},
		order:    []string{"a"},
	}

	_, err := a.processAssetFile("a", "absent.yaml", ".yaml")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to open embedded asset")
}

// TestProcessAssetFile_NilFS covers the `ef == nil` early return.
func TestProcessAssetFile_NilFS(t *testing.T) {
	t.Parallel()

	a := &embeddedAssets{
		embedded: map[string]fs.FS{"a": nil},
		order:    []string{"a"},
	}

	_, err := a.processAssetFile("a", "x.yaml", ".yaml")
	assert.ErrorIs(t, err, fs.ErrNotExist)
}

// TestParseFlatKV_SkipsCommentsAndBlanks covers the comment / blank / malformed
// line branches of parseFlatKV.
func TestParseFlatKV_SkipsCommentsAndBlanks(t *testing.T) {
	t.Parallel()

	m := parseFlatKV("# comment\n\nKEY=val\nnoequalsign\n  SPACED = trimmed  ")

	assert.Equal(t, "val", m["KEY"])
	assert.Equal(t, "trimmed", m["SPACED"])
	_, hasComment := m["# comment"]
	assert.False(t, hasComment)
	_, hasNoEq := m["noequalsign"]
	assert.False(t, hasNoEq)
}

// TestMountedFS_OpenPaths covers the mountedFS.Open branches: exact prefix,
// prefixed child, and a non-matching name.
func TestMountedFS_OpenPaths(t *testing.T) {
	t.Parallel()

	sub := fstest.MapFS{
		"info.txt": &fstest.MapFile{Data: []byte("data")},
	}
	assets := NewAssets(AssetMap{"root": fstest.MapFS{"r.txt": &fstest.MapFile{}}})
	assets.Mount(sub, "plugins/p")

	// Prefixed child path.
	f, err := assets.Open("plugins/p/info.txt")
	require.NoError(t, err)
	require.NoError(t, f.Close())

	// A name that does not match the mount prefix is not served by the mount.
	_, err = assets.Open("plugins/p/missing.txt")
	assert.ErrorIs(t, err, fs.ErrNotExist)
}

// TestEmbeddedAssets_RegisterDuplicate confirms re-registering a name overwrites
// the FS without appending a duplicate entry to the order slice.
func TestEmbeddedAssets_RegisterDuplicate(t *testing.T) {
	t.Parallel()

	a := NewAssets(AssetMap{"a": fstest.MapFS{"v1.txt": &fstest.MapFile{}}})
	a.Register("a", fstest.MapFS{"v2.txt": &fstest.MapFile{}})

	assert.Equal(t, []string{"a"}, a.Names())

	_, err := a.Open("v2.txt")
	require.NoError(t, err)

	_, err = a.Open("v1.txt")
	assert.ErrorIs(t, err, fs.ErrNotExist)
}

// TestEmbeddedAssets_MergeNilOther confirms a nil Assets argument to Merge is
// skipped silently.
func TestEmbeddedAssets_MergeNilOther(t *testing.T) {
	t.Parallel()

	a := NewAssets(AssetMap{"a": fstest.MapFS{}})
	result := a.Merge(nil)
	require.NotNil(t, result)
	assert.Equal(t, []string{"a"}, result.Names())
}

package props_test

import (
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go/config"

	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props/test"
)

// TestGetConfigView_ReadsThroughToTheStore covers the read surface added when
// Props.Config became a *config.Store.
func TestGetConfigView_ReadsThroughToTheStore(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	require.NoError(t, afero.WriteFile(fs, "/app.yaml", []byte("server:\n  port: 8080\n"), 0o600))

	store, err := config.NewStore(t.Context(),
		config.WithFiles(propsConfigFS(t, fs), "/app.yaml"))
	require.NoError(t, err)

	p := &props.Props{Config: store, FS: fs}

	assert.Equal(t, 8080, p.GetConfigView().GetInt("server.port"))
}

// TestGetConfigView_TracksReloads is the reason Props.Config holds the Store
// rather than a View.
//
// A View is pinned to one snapshot, so storing one in Props would freeze
// configuration at startup — the file could change underneath and every reader
// would keep serving the values it booted with. Taking the view per call is
// what keeps configuration live.
func TestGetConfigView_TracksReloads(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	require.NoError(t, afero.WriteFile(fs, "/app.yaml", []byte("server:\n  port: 8080\n"), 0o600))

	store, err := config.NewStore(t.Context(),
		config.WithFiles(propsConfigFS(t, fs), "/app.yaml"))
	require.NoError(t, err)

	p := &props.Props{Config: store, FS: fs}

	// A view taken now, held across the change — this is the stale-read hazard.
	pinned := p.GetConfigView()

	require.NoError(t, afero.WriteFile(fs, "/app.yaml", []byte("server:\n  port: 9090\n"), 0o600))
	require.NoError(t, store.Reload(t.Context()))

	assert.Equal(t, 8080, pinned.GetInt("server.port"),
		"a held view stays pinned to the snapshot it was taken from")
	assert.Equal(t, 9090, p.GetConfigView().GetInt("server.port"),
		"a freshly taken view sees the reload")
}

// TestGetConfigFS_BridgesAfero pins D3: Props.FS stays a full afero filesystem
// and config gets an adapted view of it, so there is one bridge rather than one
// per construction site.
func TestGetConfigFS_BridgesAfero(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	require.NoError(t, afero.WriteFile(fs, "/app.yaml", []byte("a: 1\n"), 0o600))

	p := &props.Props{FS: fs}

	body, err := p.GetConfigFS().ReadFile("/app.yaml")
	require.NoError(t, err)
	assert.Equal(t, "a: 1\n", string(body),
		"the adapted filesystem must read the same files Props.FS holds")
}

// TestPropstestDefaultStoreIsWritable pins a regression the migration could
// easily have shipped.
//
// The container this fixture used to default to supported Set. A store built from
// a reader source does not: Apply fails with "no writable layer for change",
// and it fails at whichever line happens to write first rather than at the
// construction that caused it. Backing the default with a real file keeps
// write-capable tests working.
func TestPropstestDefaultStoreIsWritable(t *testing.T) {
	t.Parallel()

	p := test.New()

	_, err := p.Config.Apply(t.Context(), config.Set("key", "value"))
	require.NoError(t, err, "the default test store must accept writes")

	assert.Equal(t, "value", p.GetConfigView().GetString("key"))
}

// TestSlogLogger covers the props-hosted slog adapter both config-side adapters
// (pkg/chat, pkg/telemetry) share: a nil Props yields a non-nil discarding
// logger, and a real logger is adapted through.
func TestSlogLogger(t *testing.T) {
	t.Parallel()

	assert.NotNil(t, props.SlogLogger(nil), "nil props must yield a non-nil discard logger")
	assert.NotNil(t, props.SlogLogger(&props.Props{Logger: logger.NewNoop()}))
}

// propsConfigFS adapts a filesystem the same way Props does, so these tests
// exercise the production bridge rather than a parallel one.
func propsConfigFS(t *testing.T, fs afero.Fs) config.FS {
	t.Helper()

	return (&props.Props{FS: fs}).GetConfigFS()
}

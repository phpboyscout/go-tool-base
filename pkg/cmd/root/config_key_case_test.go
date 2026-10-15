package root

import (
	"context"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go/config"

	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	p "gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

// go/config spec 0015 D8: two keys in one file that differ only in case used
// to load with a random winner. The tool now stops, naming the file.
func TestBuildConfigStore_RefusesAFileWhoseKeysCollideInCase(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	require.NoError(t, afero.WriteFile(fs, "/etc/mytool/config.yaml",
		[]byte("Log:\n  Level: warn\nlog:\n  level: debug\n"), 0o600))

	_, err := buildConfigStore(t.Context(), ConfigLoadOptions{
		Props:    tomlLinkedProps(t, fs, nil),
		CfgPaths: []string{"/etc/mytool/config.yaml"},
	})
	require.ErrorIs(t, err, config.ErrKeyCollision)
	assert.Contains(t, err.Error(), "/etc/mytool/config.yaml")
}

// go/config spec 0015 D9: setting an existing key spelled differently edits
// the file's key instead of adding a second one beside it.
func TestBuildConfigStore_ASetKeepsTheFilesSpelling(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	require.NoError(t, afero.WriteFile(fs, "/etc/mytool/config.toml", []byte("[Log]\nLevel = \"warn\"\n"), 0o600))

	store, err := buildConfigStore(t.Context(), ConfigLoadOptions{
		Props:    tomlLinkedProps(t, fs, nil),
		CfgPaths: []string{"/etc/mytool/config.toml"},
	})
	require.NoError(t, err)

	_, err = store.Apply(t.Context(), config.Set("log.level", "debug"))
	require.NoError(t, err)

	written, err := afero.ReadFile(fs, "/etc/mytool/config.toml")
	require.NoError(t, err)
	assert.Equal(t, "[Log]\nLevel = \"debug\"\n", string(written))
}

func exactProps(t *testing.T, fs afero.Fs) *p.Props {
	t.Helper()

	props, err := p.New(p.Tool{Name: "mytool", Config: p.ConfigSpec{KeyCase: p.KeyCaseExact}}, logger.NewNoop(), fs)
	require.NoError(t, err)

	return props
}

// Spec 0209 D2: an exact tool's runtime Store keeps its file's spelling, so
// two keys differing only in case are two keys rather than a collision.
func TestBuildConfigStore_AnExactToolKeepsEachSpelling(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	require.NoError(t, afero.WriteFile(fs, "/etc/mytool/config.yaml",
		[]byte("Log:\n  Level: warn\nlog:\n  level: debug\n"), 0o600))

	store, err := buildConfigStore(t.Context(), ConfigLoadOptions{
		Props:    exactProps(t, fs),
		CfgPaths: []string{"/etc/mytool/config.yaml"},
	})
	require.NoError(t, err)

	view := store.View()
	assert.True(t, view.CaseSensitiveKeys())
	assert.Equal(t, "warn", view.GetString("Log.Level"))
	assert.Equal(t, "debug", view.GetString("log.level"))
}

// Spec 0209 D2: the bootstrap Store a config source reads its settings from
// agrees with the runtime Store about keys.
func TestBootstrapStore_FollowsTheToolsKeyCase(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	require.NoError(t, afero.WriteFile(fs, "/etc/mytool/config.yaml", []byte("Log:\n  Level: warn\n"), 0o600))

	opts := ConfigLoadOptions{Props: exactProps(t, fs), CfgPaths: []string{"/etc/mytool/config.yaml"}}

	store, err := bootstrapStore(t.Context(), opts, opts.Props.Tool.ResolvedConfigSpec().Layers)
	require.NoError(t, err)
	assert.True(t, store.View().CaseSensitiveKeys())
	assert.Equal(t, "warn", store.View().GetString("Log.Level"))
}

// plainBackend holds no keys of its own and takes no key mode.
type plainBackend struct{}

func (plainBackend) ID() string { return "plain" }

func (plainBackend) Load(context.Context, []config.Layer) ([]config.Layer, error) { return nil, nil }

func (plainBackend) Capabilities() config.Capabilities { return config.Capabilities{} }

// keyCaseRecorder is a backend that records the mode a Store binds it to.
type keyCaseRecorder struct {
	bound []bool
}

func (*keyCaseRecorder) ID() string { return "recorder" }

func (*keyCaseRecorder) Load(context.Context, []config.Layer) ([]config.Layer, error) {
	return nil, nil
}

func (*keyCaseRecorder) Capabilities() config.Capabilities { return config.Capabilities{} }

func (r *keyCaseRecorder) BindKeyCase(caseSensitive bool) error {
	r.bound = append(r.bound, caseSensitive)

	return nil
}

// Spec 0209 D3: a config source's wrapper forwards the Store's key mode to
// the backend inside it, as go/config's own decorators do (0015 D3).
func TestWrapSource_ForwardsTheKeyCase(t *testing.T) {
	t.Parallel()

	for _, exact := range []bool{false, true} {
		inner := &keyCaseRecorder{}
		load := &sourceLoad{src: p.ConfigSource{Name: "vault", Kind: "vault"}, log: logger.NewNoop()}

		opts := []config.StoreOption{config.WithBackend(wrapSource(inner, false, load))}
		if exact {
			opts = append(opts, config.WithCaseSensitiveKeys())
		}

		_, err := config.NewStore(t.Context(), opts...)
		require.NoError(t, err)
		assert.Equal(t, []bool{exact}, inner.bound)
	}
}

// A wrapped backend that holds no keys of its own is bound to nothing.
func TestWrapSource_BindsANonBinderToNothing(t *testing.T) {
	t.Parallel()

	load := &sourceLoad{src: p.ConfigSource{Name: "vault", Kind: "vault"}, log: logger.NewNoop()}
	wrapped := wrapSource(plainBackend{}, false, load)

	binder, ok := wrapped.(config.KeyCaseBinder)
	require.True(t, ok, "the wrapper always offers the binder, so a decorator inside is never skipped")
	require.NoError(t, binder.BindKeyCase(true))
}

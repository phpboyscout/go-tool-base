package root

import (
	"context"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go/config"

	p "gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
)

// Spec 0204 D10: the root records how each declared slot fared, in declared
// order, so doctor can report the stack without rebuilding it.
func TestSources_EachSlotIsRecorded(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	writeFile(t, fs, "/cfg/config.yaml", "config:\n  sources:\n    team:\n      path: /src/team.yaml\n    down:\n      path: /src/down.yaml\n")
	writeFile(t, fs, "/src/team.yaml", "log:\n  level: debug\n")

	no := false
	st := sourcedTool{
		fs: fs,
		sources: []p.ConfigSource{
			{Name: "team", Kind: "memfile"},
			{Name: "spare", Kind: "memfile", Required: &no},
			{Name: "down", Kind: "broken", Required: &no},
		},
		layers: []p.ConfigLayer{p.LayerDefaults, "team", "spare", "down", p.LayerFiles, p.LayerEnv, p.LayerFlags},
		overrides: map[string]setup.SourceFactory{"team": func(ctx context.Context, s config.Reader, b setup.ConfigBootstrap) (config.Backend, error) {
			setup.ReportSourceCredential(b, "auth.env")

			return memfileFactory(fs)(ctx, s, b)
		}},
	}

	props := st.props(t)
	_, err := buildConfigStore(t.Context(), ConfigLoadOptions{Props: props, CfgPaths: []string{"/cfg/config.yaml"}, AllowEmpty: true})
	require.NoError(t, err)

	require.Len(t, props.SourceStatuses, 3)

	team := props.SourceStatuses[0]
	assert.Equal(t, "team", team.Slot.Name)
	assert.Equal(t, p.ConfigSourceBuilt, team.State)
	assert.Equal(t, "auth.env", team.Credential, "the factory's rung")
	assert.False(t, team.Writable)

	assert.Equal(t, p.ConfigSourceUnconfigured, props.SourceStatuses[1].State)

	down := props.SourceStatuses[2]
	assert.Equal(t, p.ConfigSourceUnavailable, down.State)
	assert.Contains(t, down.Err, "connection refused")
}

type countingCloser struct{ closed int }

func (c *countingCloser) Close() error { c.closed++; return nil }

// What a factory hands the store to close is closed with the store, and
// closed straight away if a later required slot stops the build.
func TestSources_TheStoreClosesWhatFactoriesOwn(t *testing.T) {
	t.Parallel()

	owning := func(closer *countingCloser, fs afero.Fs) setup.SourceFactory {
		return func(ctx context.Context, s config.Reader, b setup.ConfigBootstrap) (config.Backend, error) {
			setup.CloseWithStore(b, closer)

			return memfileFactory(fs)(ctx, s, b)
		}
	}

	t.Run("with the store", func(t *testing.T) {
		t.Parallel()

		fs := afero.NewMemMapFs()
		writeFile(t, fs, "/cfg/config.yaml", teamConfigured)
		writeFile(t, fs, "/src/team.yaml", "log:\n  level: debug\n")

		closer := &countingCloser{}
		st := sourcedTool{fs: fs, sources: []p.ConfigSource{{Name: "team", Kind: "memfile"}}, layers: stackWith("team", p.LayerEnv),
			overrides: map[string]setup.SourceFactory{"team": owning(closer, fs)}}

		store, err := st.build(t)
		require.NoError(t, err)
		assert.Zero(t, closer.closed, "open while the store is")

		require.NoError(t, store.Close())
		assert.Equal(t, 1, closer.closed)
	})

	t.Run("when a later slot stops the build", func(t *testing.T) {
		t.Parallel()

		fs := afero.NewMemMapFs()
		writeFile(t, fs, "/cfg/config.yaml", teamConfigured+"    down:\n      path: /src/down.yaml\n")
		writeFile(t, fs, "/src/team.yaml", "log:\n  level: debug\n")

		closer := &countingCloser{}
		st := sourcedTool{fs: fs,
			sources:   []p.ConfigSource{{Name: "team", Kind: "memfile"}, {Name: "down", Kind: "broken"}},
			layers:    []p.ConfigLayer{p.LayerDefaults, "team", "down", p.LayerFiles, p.LayerEnv, p.LayerFlags},
			overrides: map[string]setup.SourceFactory{"team": owning(closer, fs)}}

		_, err := st.build(t)
		require.ErrorIs(t, err, setup.ErrConfigSourceUnavailable)
		assert.Equal(t, 1, closer.closed, "nothing is left open")
	})
}

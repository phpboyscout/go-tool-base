package root

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go/config"
	"gitlab.com/phpboyscout/go/errors"

	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	p "gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
)

// remote is a source whose factory only builds a client, as Consul's and
// Vault's do, so an unreachable server is found when the store first loads
// it, not when the factory runs. down switches the server off.
type remote struct{ down *atomic.Bool }

func (remote) ID() string                        { return "remote:team" }
func (remote) Capabilities() config.Capabilities { return config.Capabilities{} }

func (r remote) Load(context.Context, []config.Layer) ([]config.Layer, error) {
	if r.down.Load() {
		return nil, errors.New("dial tcp 127.0.0.1:8500: connect: connection refused")
	}

	return []config.Layer{{Source: config.Source{Kind: "remote", Name: "remote:team"}, Values: map[string]any{"marker": "remote"}}}, nil
}

func remoteTool(fs afero.Fs, down *atomic.Bool, required *bool, log logger.Logger) sourcedTool {
	return sourcedTool{
		fs: fs, log: log,
		sources: []p.ConfigSource{{Name: "team", Kind: "memfile", Required: required}},
		layers:  stackWith("team", p.LayerFiles),
		overrides: map[string]setup.SourceFactory{"team": func(context.Context, config.Reader, setup.ConfigBootstrap) (config.Backend, error) {
			return remote{down: down}, nil
		}},
	}
}

// Spec 0204 D6, found by the phase 5 acceptance run: a source that builds but
// cannot be reached fails on the store's first load, and that is where the
// slot's policy has to apply.
func TestSources_ALoadFailure(t *testing.T) {
	t.Parallel()

	no := false

	t.Run("a required source refuses, naming the slot", func(t *testing.T) {
		t.Parallel()

		fs := afero.NewMemMapFs()
		writeFile(t, fs, "/cfg/config.yaml", "log:\n  level: warn\n")

		down := &atomic.Bool{}
		down.Store(true)

		_, err := remoteTool(fs, down, nil, nil).build(t)
		require.ErrorIs(t, err, setup.ErrConfigSourceUnavailable)
		assert.Contains(t, err.Error(), `"team"`)
		assert.Contains(t, err.Error(), "connection refused")
	})

	t.Run("an optional source is left out, with a warning and a status", func(t *testing.T) {
		t.Parallel()

		fs := afero.NewMemMapFs()
		writeFile(t, fs, "/cfg/config.yaml", "log:\n  level: warn\n")

		down := &atomic.Bool{}
		down.Store(true)

		log := logger.NewBuffer()
		st := remoteTool(fs, down, &no, log)
		props := st.props(t)

		store, err := buildConfigStore(t.Context(), ConfigLoadOptions{Props: props, CfgPaths: []string{"/cfg/config.yaml"}, AllowEmpty: true})
		require.NoError(t, err)
		assert.Equal(t, "warn", store.View().GetString("log.level"), "the stack resolves without it")
		assert.True(t, logged(log.Entries(), "team"), "and says so")

		require.Len(t, props.SourceStatuses, 1)
		assert.Equal(t, p.ConfigSourceUnavailable, props.SourceStatuses[0].State, "doctor reports it left out")
		assert.Contains(t, props.SourceStatuses[0].Err, "connection refused")
	})

	t.Run("a later reload failure keeps the last snapshot", func(t *testing.T) {
		t.Parallel()

		fs := afero.NewMemMapFs()
		writeFile(t, fs, "/cfg/config.yaml", "log:\n  level: warn\n")

		down := &atomic.Bool{}

		store, err := remoteTool(fs, down, &no, nil).build(t)
		require.NoError(t, err)
		require.Equal(t, "remote", store.View().GetString("marker"))

		down.Store(true)
		require.Error(t, store.Reload(t.Context()), "a blip is not a reason to drop the source's values")
		assert.Equal(t, "remote", store.View().GetString("marker"))
	})
}

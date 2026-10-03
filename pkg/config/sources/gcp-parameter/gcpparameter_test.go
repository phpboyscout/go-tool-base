package gcpparameter

import (
	"context"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go/config"
	configgcpparameter "gitlab.com/phpboyscout/go/config-gcp-parameter"

	"gitlab.com/phpboyscout/go-tool-base/pkg/config/sources/internal/gcpsource"
	"gitlab.com/phpboyscout/go-tool-base/pkg/config/sources/internal/gcpsource/gcpsourcetest"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
)

type fakePM map[string]string

func (f fakePM) Get(_ context.Context, parameter string) (configgcpparameter.Parameter, error) {
	return configgcpparameter.Parameter{ID: parameter, VersionName: parameter + "/versions/1", Payload: []byte(f[parameter])}, nil
}

func (f fakePM) List(context.Context, string) ([]configgcpparameter.Parameter, error) {
	return nil, nil
}

// Spec 0204 D18: a gcp-parameter source reads one parameter whose payload is
// a document, YAML unless its value format says otherwise.
func TestFactory_ReadsOneParameter(t *testing.T) {
	t.Parallel()

	open := func(context.Context, config.Reader) (configgcpparameter.PM, io.Closer, error) {
		return fakePM{"mytool": "log:\n  level: debug\n"}, nil, nil
	}

	backend, err := factoryWith(open)(t.Context(), settings(t, "project: acme\nparameter: mytool\n"), bootstrap{})
	require.NoError(t, err)
	assert.Equal(t, "debug", resolve(t, backend).GetString("log.level"))

	_, watchable := backend.(config.WatchableBackend)
	assert.True(t, watchable, "built at rung 2, the backend keeps its watch")
}

func TestFactory_Refuses(t *testing.T) {
	t.Parallel()

	open := func(context.Context, config.Reader) (configgcpparameter.PM, io.Closer, error) {
		return fakePM{}, nil, nil
	}

	_, err := factoryWith(open)(t.Context(), settings(t, "parameter: mytool\n"), bootstrap{})
	require.ErrorIs(t, err, gcpsource.ErrNoProject)

	_, err = factoryWith(open)(t.Context(), settings(t, "project: acme\n"), bootstrap{})
	require.ErrorIs(t, err, ErrNoParameter, "neither a parameter nor a prefix")

	_, err = factoryWith(open)(t.Context(), settings(t, "project: acme\nparameter: a\nprefix: b\n"), bootstrap{})
	require.ErrorIs(t, err, ErrNoParameter, "both")
}

func TestOpenParameters_BuildsWithoutTheNetwork(t *testing.T) {
	gcpsourcetest.Isolate(t)

	pm, closer, err := openParameters(t.Context(), settings(t, "project: acme\n"))
	require.NoError(t, err)
	assert.NotNil(t, pm)
	require.NotNil(t, closer, "the client is handed on to be closed")
	assert.NoError(t, closer.Close())
}

type closing struct {
	setup.ConfigBootstrap
	got []io.Closer
}

func (c *closing) CloseWithStore(closer io.Closer) { c.got = append(c.got, closer) }

type stubCloser struct{}

func (stubCloser) Close() error { return nil }

// The client the opener built is the store's to close.
func TestFactory_HandsTheClientToTheStore(t *testing.T) {
	t.Parallel()

	open := func(context.Context, config.Reader) (configgcpparameter.PM, io.Closer, error) {
		return fakePM{}, stubCloser{}, nil
	}
	b := &closing{ConfigBootstrap: bootstrap{}}

	_, err := factoryWith(open)(t.Context(), settings(t, "project: acme\nparameter: mytool\n"), b)
	require.NoError(t, err)
	assert.Equal(t, []io.Closer{stubCloser{}}, b.got)
}

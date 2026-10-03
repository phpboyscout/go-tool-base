package gcpsecret

import (
	"context"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go/config"
	configgcpsecret "gitlab.com/phpboyscout/go/config-gcp-secret"

	"gitlab.com/phpboyscout/go-tool-base/pkg/config/sources/internal/gcpsource"
	"gitlab.com/phpboyscout/go-tool-base/pkg/config/sources/internal/gcpsource/gcpsourcetest"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
)

type fakeSecrets map[string]string

func (f fakeSecrets) Access(_ context.Context, id, _ string) (configgcpsecret.Payload, error) {
	return configgcpsecret.Payload{VersionName: id + "/versions/1", Data: []byte(f[id])}, nil
}

func (f fakeSecrets) Describe(_ context.Context, id, _ string) (configgcpsecret.Version, error) {
	return configgcpsecret.Version{Name: id + "/versions/1", State: "ENABLED"}, nil
}

func (f fakeSecrets) Versions(context.Context, string) ([]configgcpsecret.Version, error) {
	return nil, nil
}

func (f fakeSecrets) List(context.Context, string) ([]configgcpsecret.Secret, error) {
	var out []configgcpsecret.Secret
	for id := range f {
		out = append(out, configgcpsecret.Secret{ID: id})
	}

	return out, nil
}

// Spec 0204 D18: a gcp-secret source reads one secret whose payload is a
// document (JSON by default) from its slot's project, and is sensitive.
func TestFactory_ReadsOneSecret(t *testing.T) {
	t.Parallel()

	var project string

	open := func(_ context.Context, s config.Reader) (configgcpsecret.API, io.Closer, error) {
		project = s.GetString("project")

		return fakeSecrets{"app": `{"db":{"host":"db.internal"}}`}, nil, nil
	}

	backend, err := factoryWith(open)(t.Context(), settings(t, "project: acme\nsecret: app\n"), withJSON)
	require.NoError(t, err)
	assert.Equal(t, "acme", project)
	assert.Equal(t, "db.internal", resolve(t, backend).GetString("db.host"))
	assert.True(t, backend.Capabilities().Sensitive)

	_, watchable := backend.(config.WatchableBackend)
	assert.True(t, watchable, "built at rung 2, the backend keeps its watch")
}

func TestFactory_Refuses(t *testing.T) {
	t.Parallel()

	open := func(context.Context, config.Reader) (configgcpsecret.API, io.Closer, error) {
		return fakeSecrets{}, nil, nil
	}

	_, err := factoryWith(open)(t.Context(), settings(t, "secret: app\n"), withJSON)
	require.ErrorIs(t, err, gcpsource.ErrNoProject)

	_, err = factoryWith(open)(t.Context(), settings(t, "project: acme\nsecret: app\n"), bootstrap{})
	require.ErrorIs(t, err, setup.ErrUnlinkedConfigFormat, "a JSON secret needs the json format linked")
}

// The production opener builds a client without the network. Not parallel:
// the environment is process-wide.
func TestOpenSecrets_BuildsWithoutTheNetwork(t *testing.T) {
	gcpsourcetest.Isolate(t)

	api, closer, err := openSecrets(t.Context(), settings(t, "project: acme\n"))
	require.NoError(t, err)
	assert.NotNil(t, api)
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

	open := func(context.Context, config.Reader) (configgcpsecret.API, io.Closer, error) {
		return fakeSecrets{}, stubCloser{}, nil
	}
	b := &closing{ConfigBootstrap: bootstrap{}}

	_, err := factoryWith(open)(t.Context(), settings(t, "project: acme\n"), b)
	require.NoError(t, err)
	assert.Equal(t, []io.Closer{stubCloser{}}, b.got)
}

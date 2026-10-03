package gcpgcs

import (
	"context"
	"io"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go/config"
	configafero "gitlab.com/phpboyscout/go/config-afero"
	configgcpgcs "gitlab.com/phpboyscout/go/config-gcp-gcs"

	"gitlab.com/phpboyscout/go-tool-base/pkg/config/sources/internal/gcpsource/gcpsourcetest"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
)

// Spec 0204 D2, D18: a gcp-gcs source reads one object, in the format its
// name says.
func TestFactory_ReadsTheObject(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	require.NoError(t, afero.WriteFile(fs, "mytool/config.yaml", []byte("log:\n  level: debug\n"), 0o600))

	open := func(context.Context, config.Reader) (config.FS, io.Closer, error) {
		return configafero.Wrap(fs), nil, nil
	}

	backend, err := factoryWith(open)(t.Context(), settings(t, "bucket: acme-config\nobject: mytool/config.yaml\n"), bootstrap{})
	require.NoError(t, err)
	assert.Equal(t, "debug", resolve(t, backend).GetString("log.level"))
}

func TestFactory_Refuses(t *testing.T) {
	t.Parallel()

	_, err := factoryWith(openBucket)(t.Context(), settings(t, "object: c.yaml\n"), bootstrap{})
	require.ErrorIs(t, err, ErrNoObject)

	_, err = factoryWith(openBucket)(t.Context(), settings(t, "bucket: b\nobject: c.toml\n"), bootstrap{})
	require.ErrorIs(t, err, setup.ErrUnlinkedConfigFormat)
}

func TestOpenBucket_BuildsWithoutTheNetwork(t *testing.T) {
	gcpsourcetest.Isolate(t)

	fsys, closer, err := openBucket(t.Context(), settings(t, "bucket: acme-config\n"))
	require.NoError(t, err)
	assert.NotNil(t, fsys)
	require.NotNil(t, closer, "the client is handed on to be closed")

	hinter, ok := fsys.(config.PollIntervalHinter)
	require.True(t, ok, "the owned filesystem keeps the adapter's poll hint")
	assert.Equal(t, configgcpgcs.PollInterval, hinter.PollInterval(), "a minute, not the core's two seconds of billed reads")

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

	open := func(context.Context, config.Reader) (config.FS, io.Closer, error) {
		return configafero.Wrap(afero.NewMemMapFs()), stubCloser{}, nil
	}
	b := &closing{ConfigBootstrap: bootstrap{}}

	_, err := factoryWith(open)(t.Context(), settings(t, "bucket: b\nobject: c.yaml\n"), b)
	require.NoError(t, err)
	assert.Equal(t, []io.Closer{stubCloser{}}, b.got)
}

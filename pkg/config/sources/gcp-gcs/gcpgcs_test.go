package gcpgcs

import (
	"context"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go/config"
	configafero "gitlab.com/phpboyscout/go/config-afero"

	"gitlab.com/phpboyscout/go-tool-base/pkg/config/sources/internal/gcpsource/gcpsourcetest"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
)

// Spec 0204 D2, D18: a gcp-gcs source reads one object, in the format its
// name says.
func TestFactory_ReadsTheObject(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	require.NoError(t, afero.WriteFile(fs, "mytool/config.yaml", []byte("log:\n  level: debug\n"), 0o600))

	open := func(context.Context, config.Reader) (config.FS, error) { return configafero.Wrap(fs), nil }

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

	fsys, err := openBucket(t.Context(), settings(t, "bucket: acme-config\n"))
	require.NoError(t, err)
	assert.NotNil(t, fsys)
}

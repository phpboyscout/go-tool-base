package gcpsource_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go/config"

	"gitlab.com/phpboyscout/go-tool-base/pkg/config/sources/internal/gcpsource"
	"gitlab.com/phpboyscout/go-tool-base/pkg/config/sources/internal/gcpsource/gcpsourcetest"
)

func settings(t *testing.T, yaml string) config.Reader {
	t.Helper()

	store, err := config.NewStore(t.Context(), config.WithReaders(config.NamedSource{Name: "s", Content: []byte(yaml)}))
	require.NoError(t, err)

	return store.View()
}

// An endpoint is one more client option over the ambient ones. Not parallel:
// the environment is process-wide.
func TestClientOptions_AppliesTheEndpoint(t *testing.T) {
	gcpsourcetest.Isolate(t)

	plain, err := gcpsource.ClientOptions(t.Context(), settings(t, "project: acme\n"), gcpsource.ScopeCloudPlatform)
	require.NoError(t, err)

	withEndpoint, err := gcpsource.ClientOptions(t.Context(), settings(t, "endpoint: localhost:8085\n"), gcpsource.ScopeCloudPlatform)
	require.NoError(t, err)
	assert.Len(t, withEndpoint, len(plain)+1)
}

// A project is never guessed: credentials name a principal, not a project.
func TestProject(t *testing.T) {
	t.Parallel()

	got, err := gcpsource.Project(settings(t, "project: acme\n"))
	require.NoError(t, err)
	assert.Equal(t, "acme", got)

	_, err = gcpsource.Project(settings(t, "location: global\n"))
	require.ErrorIs(t, err, gcpsource.ErrNoProject)
}

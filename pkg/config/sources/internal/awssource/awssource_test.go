package awssource_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go/config"

	"gitlab.com/phpboyscout/go-tool-base/pkg/config/sources/internal/awssource"
	"gitlab.com/phpboyscout/go-tool-base/pkg/config/sources/internal/awssource/awssourcetest"
)

func settings(t *testing.T, yaml string) config.Reader {
	t.Helper()

	store, err := config.NewStore(t.Context(), config.WithReaders(config.NamedSource{Name: "settings", Content: []byte(yaml)}))
	require.NoError(t, err)

	return store.View()
}

// Spec 0204 D18: the AWS config comes from awsclient's ambient chain, with
// the slot's region and endpoint applied. Not parallel: the environment is
// process-wide.
func TestConfig_AppliesTheSlotsSettings(t *testing.T) {
	awssourcetest.Isolate(t)

	cfg, err := awssource.Config(t.Context(), settings(t, "region: eu-west-2\nendpoint: http://localhost:4566\n"))
	require.NoError(t, err)
	assert.Equal(t, "eu-west-2", cfg.Region)
	require.NotNil(t, cfg.BaseEndpoint)
	assert.Equal(t, "http://localhost:4566", *cfg.BaseEndpoint)
}

// A named profile is passed to the SDK's config loading, which refuses one
// that does not exist: the slot fails rather than silently using another.
func TestConfig_AMissingProfileIsAnError(t *testing.T) {
	awssourcetest.Isolate(t)

	_, err := awssource.Config(t.Context(), settings(t, "profile: no-such-profile\n"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no-such-profile")
}

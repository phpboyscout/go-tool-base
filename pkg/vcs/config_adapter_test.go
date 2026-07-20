package vcs

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go/config"
)

func TestConfigFromReader_Nil(t *testing.T) {
	t.Parallel()

	assert.Nil(t, ConfigFromReader(nil))
}

func TestConfigFromReader_PreservesEnvAwareSubtrees(t *testing.T) {
	t.Setenv("GTB_GITHUB_AUTH_VALUE", "env-token")
	t.Setenv("GTB_GITHUB_URL_API", "https://env.example.com/api/v3/")

	store, err := config.NewStore(t.Context(),
		config.WithReaders(config.NamedSource{
			Name:    "test",
			Content: []byte("github:\n  auth:\n    value: file-token\n  url:\n    api: https://file.example.com/api/v3/\n"),
		}),
		config.WithEnv("GTB"),
	)
	require.NoError(t, err)

	adapted := ConfigFromReader(store.View())
	require.NotNil(t, adapted)

	githubCfg := adapted.Sub("github")
	require.NotNil(t, githubCfg)

	assert.Equal(t, "env-token", githubCfg.GetString("auth.value"))
	assert.Equal(t, "https://env.example.com/api/v3/", githubCfg.GetString("url.api"))
}

// TestConfigFromReader_AbsentSub pins the nil contract forge's guards rely on:
// Sub of a section defined nowhere returns nil, not an empty reader.
func TestConfigFromReader_AbsentSub(t *testing.T) {
	t.Parallel()

	store, err := config.NewStore(t.Context(),
		config.WithReaders(config.NamedSource{Name: "test", Content: []byte("github:\n  auth:\n    value: tok\n")}))
	require.NoError(t, err)

	adapted := ConfigFromReader(store.View())
	require.NotNil(t, adapted)

	assert.Nil(t, adapted.Sub("gitlab"))
	assert.NotNil(t, adapted.Sub("github"))
	assert.Nil(t, adapted.Sub("github").Sub("url"))
}

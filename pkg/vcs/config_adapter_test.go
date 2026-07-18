package vcs

import (
	"strings"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go/config"

	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
)

func TestConfigFromContainable_Nil(t *testing.T) {
	t.Parallel()

	assert.Nil(t, ConfigFromContainable(nil))
}

func TestConfigFromContainable_PreservesEnvAwareSubtrees(t *testing.T) {
	t.Setenv("GTB_GITHUB_AUTH_VALUE", "env-token")
	t.Setenv("GTB_GITHUB_URL_API", "https://env.example.com/api/v3/")

	cfg := config.NewReaderContainer(
		afero.NewMemMapFs(),
		config.WithLogger(logger.ToSlog(logger.NewNoop())),
		config.WithConfigFormat("yaml"),
		config.WithEnvPrefix("GTB"),
		config.WithConfigReaders(strings.NewReader("github:\n  auth:\n    value: file-token\n  url:\n    api: https://file.example.com/api/v3/\n")),
	)

	adapted := ConfigFromContainable(cfg)
	require.NotNil(t, adapted)

	githubCfg := adapted.Sub("github")
	require.NotNil(t, githubCfg)

	assert.Equal(t, "env-token", githubCfg.GetString("auth.value"))
	assert.Equal(t, "https://env.example.com/api/v3/", githubCfg.GetString("url.api"))
}

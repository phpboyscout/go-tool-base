package github

import (
	"strings"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"

	"gitlab.com/phpboyscout/go-tool-base/pkg/config"
	"gitlab.com/phpboyscout/go-tool-base/pkg/vcs/release"
)

func TestClientSettingsFromConfig(t *testing.T) {
	t.Parallel()

	cfg := config.NewReaderContainer(
		afero.NewMemMapFs(),
		config.WithConfigFormat("yaml"),
		config.WithConfigReaders(strings.NewReader(`
github:
  url:
    api: https://ghe.example.com/api/v3/
    upload: https://uploads.ghe.example.com/
  auth:
    env: GITHUB_TOKEN
    value: literal-token
    keychain: tool/github.auth
`)),
	)

	settings := ClientSettingsFromConfig(
		release.ReleaseSourceConfig{Host: "github.com"},
		cfg.Sub("github"),
	)

	assert.Equal(t, release.ReleaseSourceConfig{Host: "github.com"}, settings.ReleaseSource)
	assert.Equal(t, "https://ghe.example.com/api/v3/", settings.APIURL)
	assert.Equal(t, "https://uploads.ghe.example.com/", settings.UploadURL)
	assert.Equal(t, "GITHUB_TOKEN", settings.Auth.Env)
	assert.Equal(t, "literal-token", settings.Auth.Value)
	assert.Equal(t, "tool/github.auth", settings.Auth.Keychain)
}

func TestClientSettingsFromConfig_Nil(t *testing.T) {
	t.Parallel()

	source := release.ReleaseSourceConfig{Host: "github.com"}

	settings := ClientSettingsFromConfig(source, nil)

	assert.Equal(t, source, settings.ReleaseSource)
	assert.Empty(t, settings.APIURL)
	assert.Empty(t, settings.UploadURL)
	assert.Empty(t, settings.Auth.Env)
	assert.Empty(t, settings.Auth.Value)
	assert.Empty(t, settings.Auth.Keychain)
}

package github

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go/config"

	"gitlab.com/phpboyscout/go/forge"

	"gitlab.com/phpboyscout/go-tool-base/pkg/vcs"
)

func TestClientSettingsFromConfig(t *testing.T) {
	t.Parallel()

	store, err := config.NewStore(t.Context(),
		config.WithReaders(config.NamedSource{Name: "test", Content: []byte(`
github:
  url:
    api: https://ghe.example.com/api/v3/
    upload: https://uploads.ghe.example.com/
  auth:
    env: GITHUB_TOKEN
    value: literal-token
    keychain: tool/github.auth
`)}))
	require.NoError(t, err)

	settings := ClientSettingsFromConfig(
		forge.ReleaseSourceConfig{Host: "github.com"},
		vcs.ConfigFromReader(store.View()).Sub("github"),
	)

	assert.Equal(t, forge.ReleaseSourceConfig{Host: "github.com"}, settings.ReleaseSource)
	assert.Equal(t, "https://ghe.example.com/api/v3/", settings.APIURL)
	assert.Equal(t, "https://uploads.ghe.example.com/", settings.UploadURL)
	assert.Equal(t, "GITHUB_TOKEN", settings.Auth.Env)
	assert.Equal(t, "literal-token", settings.Auth.Value)
	assert.Equal(t, "tool/github.auth", settings.Auth.Keychain)
}

func TestClientSettingsFromConfig_Nil(t *testing.T) {
	t.Parallel()

	source := forge.ReleaseSourceConfig{Host: "github.com"}

	settings := ClientSettingsFromConfig(source, nil)

	assert.Equal(t, source, settings.ReleaseSource)
	assert.Empty(t, settings.APIURL)
	assert.Empty(t, settings.UploadURL)
	assert.Empty(t, settings.Auth.Env)
	assert.Empty(t, settings.Auth.Value)
	assert.Empty(t, settings.Auth.Keychain)
}

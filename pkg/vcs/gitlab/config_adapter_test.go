package gitlab

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"gitlab.com/phpboyscout/go-tool-base/pkg/vcs/release"
)

func TestSettingsFromConfig(t *testing.T) {
	t.Parallel()

	source := release.ReleaseSourceConfig{Host: "gitlab.example.com", Owner: "group", Repo: "tool"}
	settings := SettingsFromConfig(source, testConfig{
		"url.api":       "https://gitlab.example.com/api/v4",
		"auth.env":      "GITLAB_TOKEN_REF",
		"auth.value":    "literal-token",
		"auth.keychain": "gitlab/tool",
	})

	assert.Equal(t, source, settings.ReleaseSource)
	assert.Equal(t, "https://gitlab.example.com/api/v4", settings.APIURL)
	assert.Equal(t, "GITLAB_TOKEN_REF", settings.Auth.Env)
	assert.Equal(t, "literal-token", settings.Auth.Value)
	assert.Equal(t, "gitlab/tool", settings.Auth.Keychain)
}

func TestSettingsFromConfig_Nil(t *testing.T) {
	t.Parallel()

	source := release.ReleaseSourceConfig{Host: "gitlab.example.com"}
	settings := SettingsFromConfig(source, nil)

	assert.Equal(t, source, settings.ReleaseSource)
	assert.Empty(t, settings.APIURL)
	assert.Empty(t, settings.Auth)
}

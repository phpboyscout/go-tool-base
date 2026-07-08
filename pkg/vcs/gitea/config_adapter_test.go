package gitea

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"gitlab.com/phpboyscout/go-tool-base/pkg/vcs/release"
)

func TestSettingsFromConfig(t *testing.T) {
	t.Parallel()

	source := release.ReleaseSourceConfig{Host: "https://gitea.example.com", Owner: "org", Repo: "tool"}
	settings := SettingsFromConfig(source, testReleaseConfig{
		values: map[string]string{"url.api": "https://override.example.com"},
		subs: map[string]testReleaseConfig{
			"gitea": {values: map[string]string{
				"auth.env":      "GITEA_TOKEN_REF",
				"auth.value":    "literal-token",
				"auth.keychain": "gitea/tool",
			}},
		},
	}, "GITEA_TOKEN")

	assert.Equal(t, source, settings.ReleaseSource)
	assert.Equal(t, "https://override.example.com", settings.APIURL)
	assert.Equal(t, "GITEA_TOKEN_REF", settings.Auth.Env)
	assert.Equal(t, "literal-token", settings.Auth.Value)
	assert.Equal(t, "gitea/tool", settings.Auth.Keychain)
	assert.Equal(t, "GITEA_TOKEN", settings.TokenFallbackEnv)
}

func TestSettingsFromConfig_Nil(t *testing.T) {
	t.Parallel()

	source := release.ReleaseSourceConfig{Host: "https://gitea.example.com"}
	settings := SettingsFromConfig(source, nil, "GITEA_TOKEN")

	assert.Equal(t, source, settings.ReleaseSource)
	assert.Empty(t, settings.APIURL)
	assert.Empty(t, settings.Auth)
	assert.Equal(t, "GITEA_TOKEN", settings.TokenFallbackEnv)
}

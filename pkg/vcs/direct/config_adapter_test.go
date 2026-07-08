package direct_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"gitlab.com/phpboyscout/go-tool-base/pkg/vcs/direct"
	"gitlab.com/phpboyscout/go-tool-base/pkg/vcs/release"
)

func TestSettingsFromConfig(t *testing.T) {
	t.Parallel()

	source := release.ReleaseSourceConfig{Repo: "tool", Params: map[string]string{"url_template": "https://example.com/{version}.tar.gz"}}
	settings := direct.SettingsFromConfig(source, testReleaseConfig{
		subs: map[string]testReleaseConfig{
			"direct": {values: map[string]string{"token": "literal-token"}},
		},
	})

	assert.Equal(t, source, settings.ReleaseSource)
	assert.Equal(t, "literal-token", settings.Token)
}

func TestSettingsFromConfig_Nil(t *testing.T) {
	t.Parallel()

	source := release.ReleaseSourceConfig{Repo: "tool"}
	settings := direct.SettingsFromConfig(source, nil)

	assert.Equal(t, source, settings.ReleaseSource)
	assert.Empty(t, settings.Token)
}

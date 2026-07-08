package bitbucket

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/pkg/vcs/release"
)

func TestSettingsFromConfig(t *testing.T) {
	t.Parallel()

	source := release.ReleaseSourceConfig{Params: map[string]string{"filename_pattern": `^tool-(.+)\.tar\.gz$`}}
	settings, err := SettingsFromConfig(source, bitbucketConfig(map[string]string{
		"username":     "literal-user",
		"app_password": "literal-pass",
	}))
	require.NoError(t, err)

	assert.Equal(t, source, settings.ReleaseSource)
	assert.Equal(t, "literal-user", settings.Username)
	assert.Equal(t, "literal-pass", settings.AppPassword)
	assert.Equal(t, `^tool-(.+)\.tar\.gz$`, settings.FilenamePattern)
}

func TestSettingsFromConfig_Nil(t *testing.T) {
	t.Parallel()

	source := release.ReleaseSourceConfig{}
	settings, err := SettingsFromConfig(source, nil)
	require.NoError(t, err)

	assert.Equal(t, source, settings.ReleaseSource)
	assert.Empty(t, settings.Username)
	assert.Empty(t, settings.AppPassword)
	assert.Empty(t, settings.FilenamePattern)
}

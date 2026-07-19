package repo

import (
	"strings"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go/config"

	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/vcs/release"
)

func repoCfgFromYAML(t *testing.T, yaml string) config.Containable {
	t.Helper()

	return config.NewReaderContainer(
		afero.NewMemMapFs(),
		config.WithConfigFormat("yaml"),
		config.WithConfigReaders(strings.NewReader(yaml)),
	)
}

func TestSettingsFromContainable_Nil(t *testing.T) {
	t.Parallel()

	source := release.ReleaseSourceConfig{Type: "github"}

	settings := SettingsFromContainable(source, nil, logger.NewNoop(), afero.NewMemMapFs())

	assert.Equal(t, "github", settings.Forge)
	assert.False(t, settings.Private)
	assert.False(t, settings.AuthEnabled)
	assert.Nil(t, settings.Token)
	assert.False(t, settings.SSH.Configured)
}

func TestSettingsFromContainable_TokenAuth(t *testing.T) {
	t.Parallel()

	cfg := repoCfgFromYAML(t, `github: {auth: {value: tok-from-config}}`)

	settings := SettingsFromContainable(
		release.ReleaseSourceConfig{Type: "github"},
		cfg,
		logger.NewNoop(),
		afero.NewMemMapFs(),
	)

	assert.True(t, settings.AuthEnabled)
	require.NotNil(t, settings.Token)
	// The adapter bound the github subtree, so the token resolves from it.
	assert.Equal(t, "tok-from-config", settings.Token())
	assert.False(t, settings.SSH.Configured)
}

func TestSettingsFromContainable_ProviderOverride(t *testing.T) {
	t.Parallel()

	cfg := repoCfgFromYAML(t, `
vcs: {provider: gitlab}
gitlab: {auth: {value: gl-tok-from-config}}
`)

	settings := SettingsFromContainable(
		release.ReleaseSourceConfig{Type: "github"},
		cfg,
		logger.NewNoop(),
		afero.NewMemMapFs(),
	)

	assert.Equal(t, "gitlab", settings.Forge)
	require.NotNil(t, settings.Token)
	// The provider override switched the bound subtree to gitlab.
	assert.Equal(t, "gl-tok-from-config", settings.Token())
}

func TestSettingsFromContainable_SSHKey(t *testing.T) {
	t.Parallel()

	cfg := repoCfgFromYAML(t, `
github:
  ssh:
    key:
      env: GTB_TEST_SSH_KEY
      path: /id_ed25519
`)

	settings := SettingsFromContainable(
		release.ReleaseSourceConfig{Type: "github"},
		cfg,
		logger.NewNoop(),
		afero.NewMemMapFs(),
	)

	assert.True(t, settings.SSH.Configured)
	assert.True(t, settings.SSH.HasKey)
	assert.Equal(t, "GTB_TEST_SSH_KEY", settings.SSH.Env)
	assert.Equal(t, "/id_ed25519", settings.SSH.Path)
}

func TestSettingsFromContainable_ScalarSSH(t *testing.T) {
	t.Parallel()

	cfg := repoCfgFromYAML(t, `github: {ssh: true}`)

	settings := SettingsFromContainable(
		release.ReleaseSourceConfig{Type: "github"},
		cfg,
		logger.NewNoop(),
		afero.NewMemMapFs(),
	)

	assert.True(t, settings.SSH.Configured)
	assert.False(t, settings.SSH.HasKey)
}

func TestSettingsFromProps(t *testing.T) {
	t.Parallel()

	cfg := repoCfgFromYAML(t, `github: {auth: {value: tok-from-config}}`)
	p := &props.Props{
		Tool: props.Tool{ReleaseSource: props.ReleaseSource{
			Type:    "github",
			Private: true,
		}},
		Logger: logger.NewNoop(),
		Config: cfg,
		FS:     afero.NewMemMapFs(),
	}

	settings := SettingsFromProps(p)

	assert.Equal(t, "github", settings.Forge)
	assert.True(t, settings.Private, "Private must carry over from the release source")
	assert.True(t, settings.AuthEnabled)
	require.NotNil(t, settings.Token)
	assert.Equal(t, "tok-from-config", settings.Token())
}

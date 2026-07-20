package repo

import (
	"strings"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go/config"
	"gitlab.com/phpboyscout/go/forge"
	gorepo "gitlab.com/phpboyscout/go/repo"

	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
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

	source := forge.ReleaseSourceConfig{Type: "github"}

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
		forge.ReleaseSourceConfig{Type: "github"},
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
		forge.ReleaseSourceConfig{Type: "github"},
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
		forge.ReleaseSourceConfig{Type: "github"},
		cfg,
		logger.NewNoop(),
		afero.NewMemMapFs(),
	)

	assert.True(t, settings.SSH.Configured)
	assert.True(t, settings.SSH.HasKey)
	// An explicit path wins over the named environment variable.
	assert.Equal(t, "/id_ed25519", settings.SSH.Path)
}

// TestSettingsFromContainable_SSHKeyFromEnv proves the adapter — not the repo
// package — resolves a named environment variable into a concrete path, so
// Settings arrives fully resolved.
func TestSettingsFromContainable_SSHKeyFromEnv(t *testing.T) {
	t.Setenv("GTB_TEST_SSH_KEY_FROM_ENV", "/from/env/id_ed25519")

	cfg := repoCfgFromYAML(t, `
github:
  ssh:
    key:
      env: GTB_TEST_SSH_KEY_FROM_ENV
`)

	settings := SettingsFromContainable(
		forge.ReleaseSourceConfig{Type: "github"},
		cfg,
		logger.NewNoop(),
		afero.NewMemMapFs(),
	)

	assert.Equal(t, "/from/env/id_ed25519", settings.SSH.Path)
}

func TestSettingsFromContainable_ScalarSSH(t *testing.T) {
	t.Parallel()

	cfg := repoCfgFromYAML(t, `github: {ssh: true}`)

	settings := SettingsFromContainable(
		forge.ReleaseSourceConfig{Type: "github"},
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

// TestSettingsFromProps_Nil covers the nil-props guard: a caller with no
// container gets empty settings rather than a panic.
func TestSettingsFromProps_Nil(t *testing.T) {
	t.Parallel()

	assert.Equal(t, gorepo.Settings{}, SettingsFromProps(nil))
}

// TestResolveForge covers the normalisation the adapter applies before storing
// the result in Settings.Forge. Because the module applies the same rule
// internally, storing the normalised value makes that pass a no-op — the config
// subtree, the fallback env var and the auth convention cannot disagree.
func TestResolveForge(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"github":      "github",
		"GitLab":      "gitlab",
		"  gitea  ":   "gitea",
		"":            "github",
		"direct":      "github",
		"self-hosted": "self-hosted",
	}

	for in, want := range cases {
		t.Run("in="+in, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, want, resolveForge(in))
		})
	}
}

// TestNewRepoFromProps covers both GTB constructors end to end, including the
// nil-props path.
func TestNewRepoFromProps(t *testing.T) {
	t.Parallel()

	p := &props.Props{
		Tool:   props.Tool{ReleaseSource: props.ReleaseSource{Type: "github"}},
		Logger: logger.NewNoop(),
		Config: repoCfgFromYAML(t, `github: {auth: {value: tok}}`),
		FS:     afero.NewMemMapFs(),
	}

	r, err := NewRepoFromProps(p)
	require.NoError(t, err)
	require.NotNil(t, r)

	ts, err := NewThreadSafeRepoFromProps(p)
	require.NoError(t, err)
	require.NotNil(t, ts)

	// Nil props resolves to empty settings, which is still constructible.
	rNil, err := NewRepoFromProps(nil)
	require.NoError(t, err)
	assert.NotNil(t, rNil)
}

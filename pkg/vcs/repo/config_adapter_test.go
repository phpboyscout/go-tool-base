package repo

import (
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

func repoStoreFromYAML(t *testing.T, yaml string) *config.Store {
	t.Helper()

	store, err := config.NewStore(t.Context(),
		config.WithReaders(config.NamedSource{Name: "test", Content: []byte(yaml)}))
	require.NoError(t, err)

	return store
}

func repoViewFromYAML(t *testing.T, yaml string) *config.View {
	t.Helper()

	return repoStoreFromYAML(t, yaml).View()
}

func TestSettingsFromReader_Nil(t *testing.T) {
	t.Parallel()

	source := forge.ReleaseSourceConfig{Type: "github"}

	settings := SettingsFromReader(source, nil, logger.NewNoop(), afero.NewMemMapFs())

	assert.Equal(t, "github", settings.Forge)
	assert.False(t, settings.Private)
	assert.False(t, settings.AuthEnabled)
	assert.Nil(t, settings.Token)
	assert.False(t, settings.SSH.Configured)
}

func TestSettingsFromReader_TokenAuth(t *testing.T) {
	t.Parallel()

	cfg := repoViewFromYAML(t, `github: {auth: {value: tok-from-config}}`)

	settings := SettingsFromReader(
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

func TestSettingsFromReader_ProviderOverride(t *testing.T) {
	t.Parallel()

	cfg := repoViewFromYAML(t, `
vcs: {provider: gitlab}
gitlab: {auth: {value: gl-tok-from-config}}
`)

	settings := SettingsFromReader(
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

func TestSettingsFromReader_SSHKey(t *testing.T) {
	t.Parallel()

	cfg := repoViewFromYAML(t, `
github:
  ssh:
    key:
      env: GTB_TEST_SSH_KEY
      path: /id_ed25519
`)

	settings := SettingsFromReader(
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

// TestSettingsFromReader_SSHKeyFromEnv proves the adapter — not the repo
// package — resolves a named environment variable into a concrete path, so
// Settings arrives fully resolved.
func TestSettingsFromReader_SSHKeyFromEnv(t *testing.T) {
	t.Setenv("GTB_TEST_SSH_KEY_FROM_ENV", "/from/env/id_ed25519")

	cfg := repoViewFromYAML(t, `
github:
  ssh:
    key:
      env: GTB_TEST_SSH_KEY_FROM_ENV
`)

	settings := SettingsFromReader(
		forge.ReleaseSourceConfig{Type: "github"},
		cfg,
		logger.NewNoop(),
		afero.NewMemMapFs(),
	)

	assert.Equal(t, "/from/env/id_ed25519", settings.SSH.Path)
}

func TestSettingsFromReader_ScalarSSH(t *testing.T) {
	t.Parallel()

	cfg := repoViewFromYAML(t, `github: {ssh: true}`)

	settings := SettingsFromReader(
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

	p := &props.Props{
		Tool: props.Tool{ReleaseSource: props.ReleaseSource{
			Type:    "github",
			Private: true,
		}},
		Logger: logger.NewNoop(),
		Config: repoStoreFromYAML(t, `github: {auth: {value: tok-from-config}}`),
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
// store gets empty settings rather than a panic.
func TestSettingsFromProps_Nil(t *testing.T) {
	t.Parallel()

	assert.Equal(t, gorepo.Settings{}, SettingsFromProps(nil))
}

// TestSettingsFromProps_NilStore covers Props built without a store (tests,
// hand-constructed Props): same no-auth settings as a nil reader, no panic.
func TestSettingsFromProps_NilStore(t *testing.T) {
	t.Parallel()

	p := &props.Props{
		Tool:   props.Tool{ReleaseSource: props.ReleaseSource{Type: "github"}},
		Logger: logger.NewNoop(),
		FS:     afero.NewMemMapFs(),
	}

	settings := SettingsFromProps(p)

	assert.Equal(t, "github", settings.Forge)
	assert.False(t, settings.AuthEnabled)
	assert.Nil(t, settings.Token)
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
		Config: repoStoreFromYAML(t, `github: {auth: {value: tok}}`),
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

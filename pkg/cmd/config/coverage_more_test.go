package config

import (
	"testing"

	"github.com/spf13/afero"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/pkg/config"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

// fileBoundProps returns Props whose live viper is bound to path on fs, so the
// writable-path resolution used by set/unset/edit resolves to that file.
func fileBoundProps(fs afero.Fs, path string) *props.Props {
	v := viper.New()
	v.SetConfigFile(path)

	return &props.Props{
		FS:     fs,
		Config: config.NewContainerFromViper(nil, v),
		Tool:   props.Tool{Name: "tool"},
	}
}

// TestLoadWritableSettings_NullContentResetsMap — a file holding "null"
// unmarshals to a nil map, which loadWritableSettings must reset to an empty,
// non-nil map so callers can mutate it.
func TestLoadWritableSettings_NullContentResetsMap(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	path := "/etc/tool/config.yaml"
	require.NoError(t, afero.WriteFile(fs, path, []byte("null\n"), 0o600))

	_, gotPath, settings, err := loadWritableSettings(fileBoundProps(fs, path))
	require.NoError(t, err)
	assert.Equal(t, path, gotPath)
	require.NotNil(t, settings)
	assert.Empty(t, settings)
}

// TestLoadWritableSettings_InvalidYAML — an existing file with unparseable YAML
// surfaces a descriptive error rather than silently starting from empty.
func TestLoadWritableSettings_InvalidYAML(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	path := "/etc/tool/config.yaml"
	require.NoError(t, afero.WriteFile(fs, path, []byte("}{"), 0o600))

	_, _, _, err := loadWritableSettings(fileBoundProps(fs, path))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "parsing existing config")
}

// TestPersistConfigValue_PropagatesLoadError — persistConfigValue surfaces a
// loadWritableSettings failure (here: an unparseable existing file).
func TestPersistConfigValue_PropagatesLoadError(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	path := "/etc/tool/config.yaml"
	require.NoError(t, afero.WriteFile(fs, path, []byte("}{"), 0o600))

	err := persistConfigValue(fileBoundProps(fs, path), "log.level", "debug")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "parsing existing config")
}

// TestWriteConfigAtomic_MkdirError — a read-only filesystem fails the lazy
// parent-directory create, and the error is wrapped descriptively.
func TestWriteConfigAtomic_MkdirError(t *testing.T) {
	t.Parallel()

	ro := afero.NewReadOnlyFs(afero.NewMemMapFs())

	err := writeConfigAtomic(ro, "/etc/tool/config.yaml", []byte("k: v\n"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "create config directory")
}

// TestSeedOrRead_EmptyToolNameFallback — when the file is absent and the tool
// name is empty, the seeded header falls back to the generic "tool" label.
func TestSeedOrRead_EmptyToolNameFallback(t *testing.T) {
	t.Parallel()

	out := seedOrRead(afero.NewMemMapFs(), "/absent/config.yaml", "")
	assert.Contains(t, string(out), "tool configuration")
}

// TestResolveEnvVarName_AssumeYes — with --yes the default env var name is
// returned without prompting.
func TestResolveEnvVarName_AssumeYes(t *testing.T) {
	t.Parallel()

	name, err := resolveEnvVarName(MigrateOptions{AssumeYes: true}, literalCredential{
		Key: "github.auth.value",
	})
	require.NoError(t, err)
	assert.Equal(t, "GITHUB_TOKEN", name)
}

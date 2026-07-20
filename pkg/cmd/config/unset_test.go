package config_test

import (
	"bytes"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	cfg "gitlab.com/phpboyscout/go/config"
	configafero "gitlab.com/phpboyscout/go/config-afero"

	"gitlab.com/phpboyscout/go-tool-base/pkg/cmd/config"
	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

// newFileConfig seeds a config file on a fresh memmap FS and returns Props
// whose store has that file as its writable layer, so set/unset/edit's
// read-modify-write round trip exercises the same afero filesystem the store
// reads and writes through.
func newFileConfig(t *testing.T, contents string, opts ...cfg.StoreOption) (*props.Props, afero.Fs, string) {
	t.Helper()

	fs := afero.NewMemMapFs()
	path := "/etc/tool/config.yaml"
	require.NoError(t, afero.WriteFile(fs, path, []byte(contents), 0o600))

	store, err := cfg.NewStore(t.Context(),
		append([]cfg.StoreOption{cfg.WithFiles(configafero.Wrap(fs), path)}, opts...)...)
	require.NoError(t, err)

	return &props.Props{
		Config: store,
		FS:     fs,
		Tool:   props.Tool{Name: "tool"},
		Logger: logger.NewNoop(),
	}, fs, path
}

func TestCmdUnset_RemovesFileKey(t *testing.T) {
	t.Parallel()

	p, fs, path := newFileConfig(t, "log:\n  level: info\nfeature:\n  enabled: true\n")

	cmd := config.NewCmdUnset(p)
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetArgs([]string{"feature.enabled"})

	require.NoError(t, cmd.Execute())
	assert.Contains(t, buf.String(), "unset feature.enabled")

	data, err := afero.ReadFile(fs, path)
	require.NoError(t, err)
	assert.NotContains(t, string(data), "enabled")
	// The required key survives; the live store reflects the removal.
	assert.False(t, p.Config.View().IsSet("feature.enabled"))
	assert.Equal(t, "info", p.Config.View().GetString("log.level"))
}

func TestCmdUnset_NestedKey(t *testing.T) {
	t.Parallel()

	p, fs, path := newFileConfig(t, "log:\n  level: info\nai:\n  provider: openai\n  model: gpt\n")

	cmd := config.NewCmdUnset(p)
	cmd.SetArgs([]string{"ai.model"})
	require.NoError(t, cmd.Execute())

	data, err := afero.ReadFile(fs, path)
	require.NoError(t, err)
	assert.NotContains(t, string(data), "model")
	assert.Contains(t, string(data), "provider")
}

func TestCmdUnset_KeyAbsentFromFile(t *testing.T) {
	t.Parallel()

	p, _, _ := newFileConfig(t, "log:\n  level: info\n")

	cmd := config.NewCmdUnset(p)
	cmd.SetArgs([]string{"does.not.exist"})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), `config key "does.not.exist" not found`)
}

// TestCmdUnset_EnvOnlyKeyRefused — a key present only via an env layer is not
// file-backed, so unset refuses it rather than silently "succeeding".
func TestCmdUnset_EnvOnlyKeyRefused(t *testing.T) {
	t.Parallel()

	p, _, _ := newFileConfig(t, "log:\n  level: info\n",
		cfg.WithEnv("TOOLTEST", cfg.WithEnviron(func() []string {
			return []string{"TOOLTEST_FEATURE_ENABLED=true"}
		})))

	require.True(t, p.Config.View().IsSet("feature.enabled"), "env layer must resolve")

	cmd := config.NewCmdUnset(p)
	cmd.SetArgs([]string{"feature.enabled"})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), `config key "feature.enabled" not found`)
}

// TestCmdUnset_EnvShadowedFileValueUnsettable — a file value hidden under an
// env override is still file-backed, so unset removes it from the file; the
// env value keeps resolving afterwards.
func TestCmdUnset_EnvShadowedFileValueUnsettable(t *testing.T) {
	t.Parallel()

	p, fs, path := newFileConfig(t, "log:\n  level: info\nfeature:\n  enabled: true\n",
		cfg.WithEnv("TOOLTEST", cfg.WithEnviron(func() []string {
			return []string{"TOOLTEST_FEATURE_ENABLED=false"}
		})))

	cmd := config.NewCmdUnset(p)
	cmd.SetArgs([]string{"feature.enabled"})
	require.NoError(t, cmd.Execute())

	data, err := afero.ReadFile(fs, path)
	require.NoError(t, err)
	assert.NotContains(t, string(data), "enabled")
	// The env override still supplies the key after the file entry is gone.
	assert.True(t, p.Config.View().IsSet("feature.enabled"))
}

// TestCmdUnset_RequiredKeyRefused pins the no-defaults case: the fixture
// Props carry no asset bundles, so nothing backfills log.level and the
// removal would leave the resolved configuration invalid — refused, file
// untouched.
func TestCmdUnset_RequiredKeyRefused(t *testing.T) {
	t.Parallel()

	p, fs, path := newFileConfig(t, "log:\n  level: info\n")

	cmd := config.NewCmdUnset(p)
	cmd.SetArgs([]string{"log.level"})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid")

	// File is untouched — the required key is still present.
	data, rerr := afero.ReadFile(fs, path)
	require.NoError(t, rerr)
	assert.Contains(t, string(data), "level")
}

// TestCmdUnset_DefaultedKeyFallsBack pins the framework-defaults case: with
// asset bundles registered the candidate is validated layered over the
// merged embedded defaults, so removing log.level succeeds and the resolved
// value falls back to the shipped default.
func TestCmdUnset_DefaultedKeyFallsBack(t *testing.T) {
	t.Parallel()

	p, fs, path := newFileConfig(t, "log:\n  level: debug\n")
	p.Assets = props.NewAssets()

	cmd := config.NewCmdUnset(p)
	cmd.SetArgs([]string{"log.level"})
	require.NoError(t, cmd.Execute())

	data, err := afero.ReadFile(fs, path)
	require.NoError(t, err)
	assert.NotContains(t, string(data), "debug",
		"the user's override must leave the file")
}

func TestCmdUnset_JSONOutput(t *testing.T) {
	t.Parallel()

	p, _, _ := newFileConfig(t, "log:\n  level: info\nfeature:\n  enabled: true\n")

	cmd := config.NewCmdUnset(p)
	cmd.Flags().String("output", "json", "")
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetArgs([]string{"feature.enabled"})

	require.NoError(t, cmd.Execute())
	assert.Contains(t, buf.String(), `"key": "feature.enabled"`)
	assert.Contains(t, buf.String(), `"status": "success"`)
}

func TestCmdUnset_NilConfig(t *testing.T) {
	t.Parallel()

	cmd := config.NewCmdUnset(&props.Props{Config: nil})
	cmd.SetArgs([]string{"log.level"})
	assert.Error(t, cmd.Execute())
}

package config_test

import (
	"bytes"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/pkg/cmd/config"
	gtbconfig "gitlab.com/phpboyscout/go-tool-base/pkg/config"
	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

// newFileConfig seeds a config file on a fresh memmap FS and returns a
// file-bound container plus the FS, so unset's read-modify-write-reload round
// trip exercises the same afero filesystem viper is wired to (v.SetFs).
func newFileConfig(t *testing.T, contents string) (*props.Props, afero.Fs, string) {
	t.Helper()

	fs := afero.NewMemMapFs()
	path := "/etc/tool/config.yaml"
	require.NoError(t, afero.WriteFile(fs, path, []byte(contents), 0o600))

	c, err := gtbconfig.LoadFilesContainer(fs,
		gtbconfig.WithLogger(logger.ToSlog(logger.NewNoop())),
		gtbconfig.WithConfigFiles(path),
	)
	require.NoError(t, err)

	return &props.Props{Config: c, FS: fs, Tool: props.Tool{Name: "tool"}}, fs, path
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
	// The required key survives; the live config reflects the rewrite.
	assert.False(t, p.Config.IsSet("feature.enabled"))
	assert.Equal(t, "info", p.Config.GetString("log.level"))
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

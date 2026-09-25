package config_test

import (
	"bytes"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	configtoml "gitlab.com/phpboyscout/go/config-toml"

	"gitlab.com/phpboyscout/go-tool-base/pkg/cmd/config"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
)

// Spec 0204 D14: config convert loads through the old codec, writes through
// the new one, and leaves the old file for the user to remove.
func TestCmdConvert_YAMLToTOML(t *testing.T) {
	t.Parallel()

	p, fs, _ := ownFormatProps(t, "toml", "")
	require.NoError(t, afero.WriteFile(fs, "/cfg/config.yaml", []byte("log:\n  level: debug\ngithub:\n  auth:\n    env: GITHUB_TOKEN\n"), 0o600))

	cmd := config.NewCmdConvert(p)
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"--from", "/cfg/config.yaml", "--to", "/cfg/config.toml"})
	require.NoError(t, cmd.Execute())

	written, err := afero.ReadFile(fs, "/cfg/config.toml")
	require.NoError(t, err)

	docs, err := configtoml.Codec{}.Decode("/cfg/config.toml", written)
	require.NoError(t, err)
	assert.Equal(t, "debug", docs[0]["log"].(map[string]any)["level"])
	assert.Equal(t, "GITHUB_TOKEN", docs[0]["github"].(map[string]any)["auth"].(map[string]any)["env"])

	info, err := fs.Stat("/cfg/config.toml")
	require.NoError(t, err)
	assert.Equal(t, "-rw-------", info.Mode().Perm().String(), "the new file may hold credentials")

	kept, err := afero.Exists(fs, "/cfg/config.yaml")
	require.NoError(t, err)
	assert.True(t, kept, "the old file is left for the user to remove")
	assert.Contains(t, out.String(), "/cfg/config.yaml")
}

// An existing target is never overwritten: it may be the user's converted
// file, edited since.
func TestCmdConvert_RefusesAnExistingTarget(t *testing.T) {
	t.Parallel()

	p, fs, _ := ownFormatProps(t, "toml", "")
	require.NoError(t, afero.WriteFile(fs, "/cfg/config.yaml", []byte("a: 1\n"), 0o600))
	require.NoError(t, afero.WriteFile(fs, "/cfg/config.toml", []byte("a = 2\n"), 0o600))

	cmd := config.NewCmdConvert(p)
	cmd.SetArgs([]string{"--from", "/cfg/config.yaml", "--to", "/cfg/config.toml"})
	require.ErrorIs(t, cmd.Execute(), config.ErrConvertTargetExists)

	data, _ := afero.ReadFile(fs, "/cfg/config.toml")
	assert.Equal(t, "a = 2\n", string(data))
}

func TestCmdConvert_RefusesAnUnlinkedTarget(t *testing.T) {
	t.Parallel()

	p, fs, _ := ownFormatProps(t, "toml", "")
	require.NoError(t, afero.WriteFile(fs, "/cfg/config.yaml", []byte("a: 1\n"), 0o600))

	cmd := config.NewCmdConvert(p)
	cmd.SetArgs([]string{"--from", "/cfg/config.yaml", "--to", "/cfg/config.hcl"})
	require.ErrorIs(t, cmd.Execute(), setup.ErrUnlinkedConfigFormat)
}

func TestCmdConvert_RefusesAMissingSource(t *testing.T) {
	t.Parallel()

	p, _, _ := ownFormatProps(t, "toml", "")

	cmd := config.NewCmdConvert(p)
	cmd.SetArgs([]string{"--from", "/cfg/config.yaml", "--to", "/cfg/config.toml"})
	require.Error(t, cmd.Execute())
}

// convert has to run on a tool that refuses to start, so it opts out of the
// config check the refusal sits behind.
func TestCmdConvert_SkipsTheConfigCheck(t *testing.T) {
	t.Parallel()

	p, _, _ := ownFormatProps(t, "toml", "")
	assert.True(t, setup.SkipsConfigCheck(config.NewCmdConvert(p)))
}

package root

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	configtoml "gitlab.com/phpboyscout/go/config-toml"
	"gitlab.com/phpboyscout/go/features"

	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	p "gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
)

// strandedTool is a TOML tool, auto-initialising, whose user config is still
// the config.yaml it wrote before its format changed.
func strandedTool(t *testing.T) (*p.Props, string) {
	t.Helper()

	home := t.TempDir()
	t.Setenv("HOME", home)

	reg := features.NewRegistry()
	for _, d := range p.DescriptorsIn(features.Default().Snapshot()) {
		require.NoError(t, reg.Declare(d))
	}

	require.NoError(t, reg.Declare(setup.ConfigFormatDescriptor("toml")))
	setup.RegisterConfigCodecOn(reg, "toml", configtoml.Codec{}, ".toml")

	tool := p.Tool{
		Name:      "mytool",
		Config:    p.ConfigSpec{Format: "toml"},
		Bootstrap: p.BootstrapPolicy{AutoInitialise: true},
	}

	props, err := p.New(tool, logger.NewNoop(), afero.NewOsFs(), p.WithFeatures(reg.Snapshot()), p.WithAssets(p.NewAssets()))
	require.NoError(t, err)

	dir := filepath.Join(home, ".mytool")
	require.NoError(t, os.MkdirAll(dir, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "config.yaml"), []byte("log:\n  level: debug\n"), 0o600))

	return props, dir
}

// Spec 0204 D14: the tool refuses to start rather than read nothing, or
// auto-initialise a fresh file over the one the user has.
func TestResolveBootstrapConfig_RefusesAStrandedConfig(t *testing.T) {
	props, dir := strandedTool(t)

	cmd := newConfigFlagCmd(t)
	_, err := resolveBootstrapConfig(props, cmd, nil, setup.DefaultConfigPaths(props), nil)
	require.ErrorIs(t, err, setup.ErrConfigFormatChanged)
	assert.Contains(t, err.Error(), filepath.Join(dir, "config.yaml"))

	_, statErr := os.Stat(filepath.Join(dir, "config.toml"))
	assert.ErrorIs(t, statErr, os.ErrNotExist, "auto-initialise must not write over the question")
}

// A command that opts out of the config check still runs, which is how doctor
// reports the condition and config convert resolves it.
func TestResolveBootstrapConfig_StrandedConfigSparesTheOptedOut(t *testing.T) {
	props, _ := strandedTool(t)

	cmd := newConfigFlagCmd(t)
	cmd.SetContext(t.Context())
	setup.SkipConfigCheck(cmd)

	_, err := resolveBootstrapConfig(props, cmd, nil, setup.DefaultConfigPaths(props), nil)
	require.NoError(t, err)
}

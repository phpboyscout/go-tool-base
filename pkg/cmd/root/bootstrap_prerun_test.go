package root

import (
	"context"
	"io"
	"path/filepath"
	"testing"
	"testing/fstest"

	"github.com/spf13/afero"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	p "gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
)

// noConfigProps builds a Props with an empty in-memory FS (no config file) and
// the network/prompt-driven features disabled, so an end-to-end Execute
// exercises only the missing-config bootstrap path. init stays enabled (the
// default) unless the caller overrides Features.
func noConfigProps(t *testing.T, name string, features ...p.FeatureState) *p.Props {
	t.Helper()

	base := []p.FeatureState{
		p.Disable(p.UpdateCmd),
		p.Disable(p.McpCmd),
		p.Disable(p.DocsCmd),
		p.Disable(p.DoctorCmd),
		p.Disable(p.TelemetryCmd),
	}

	// Real tools embed assets/init/config.yaml; the tolerant-load path layers it
	// in, so the test props must carry it too.
	assets := p.NewAssets(p.AssetMap{
		"init": fstest.MapFS{
			"assets/init/config.yaml": &fstest.MapFile{Data: []byte("log:\n  level: info\n")},
		},
	})

	return &p.Props{
		Logger: logger.NewNoop(),
		FS:     afero.NewMemMapFs(),
		Assets: assets,
		Tool: p.Tool{
			Name:     name,
			Features: p.SetFeatures(append(base, features...)...),
		},
	}
}

// autoInitConfigPath is where auto-init writes the default config for a tool —
// the same default path the root command searches when --config is not given.
func autoInitConfigPath(fs afero.Fs, name string) string {
	return filepath.Join(setup.GetDefaultConfigDir(fs, name), setup.DefaultConfigFilename)
}

func execChild(t *testing.T, props *p.Props, child *cobra.Command) error {
	t.Helper()

	setup.ResetRegistryForTesting()
	t.Cleanup(setup.ResetRegistryForTesting)

	rootCmd := NewCmdRoot(props, setup.Wrap("", child))
	rootCmd.SetArgs([]string{"child"})
	rootCmd.SetOut(io.Discard)
	rootCmd.SetErr(io.Discard)

	return rootCmd.ExecuteContext(context.Background())
}

// TestPreRun_MissingConfig_HardFailsByDefault is the baseline: with init enabled
// and no bootstrap policy, a missing config is a hard error and the command
// never runs.
func TestPreRun_MissingConfig_HardFailsByDefault(t *testing.T) {
	props := noConfigProps(t, "hardfail-tool")

	var childRan bool
	child := &cobra.Command{Use: "child", RunE: func(_ *cobra.Command, _ []string) error {
		childRan = true

		return nil
	}}

	err := execChild(t, props, child)
	require.Error(t, err, "missing config with init enabled must hard-fail")
	assert.False(t, childRan, "child must not run when bootstrap hard-fails")
	assert.Nil(t, props.Config)
}

// TestPreRun_SkipConfigCheck_ByName relaxes the missing-config gate for a named
// command: bootstrap still runs (props.Config populated from embedded defaults)
// and the command executes.
func TestPreRun_SkipConfigCheck_ByName(t *testing.T) {
	props := noConfigProps(t, "skipname-tool")
	props.Tool.Bootstrap = p.BootstrapPolicy{SkipConfigCheck: []string{"child"}}

	var childRan bool
	child := &cobra.Command{Use: "child", RunE: func(_ *cobra.Command, _ []string) error {
		childRan = true

		return nil
	}}

	require.NoError(t, execChild(t, props, child))
	assert.True(t, childRan, "named skip-config-check command must run despite missing config")
	require.NotNil(t, props.Config, "bootstrap must still populate config (embedded defaults)")
	assert.False(t, exists(t, props.FS, autoInitConfigPath(props.FS, "skipname-tool")),
		"skip-config-check must not write a config file")
}

// TestPreRun_SkipConfigCheck_ByPath matches on the full command path, so a bare
// name at a different level would not collide.
func TestPreRun_SkipConfigCheck_ByPath(t *testing.T) {
	props := noConfigProps(t, "skippath-tool")
	props.Tool.Bootstrap = p.BootstrapPolicy{SkipConfigCheck: []string{"skippath-tool child"}}

	var childRan bool
	child := &cobra.Command{Use: "child", RunE: func(_ *cobra.Command, _ []string) error {
		childRan = true

		return nil
	}}

	require.NoError(t, execChild(t, props, child))
	assert.True(t, childRan, "full-path skip-config-check entry must match")
	require.NotNil(t, props.Config)
}

// TestPreRun_SkipConfigCheck_ByAnnotation relaxes the gate via the
// setup.SkipConfigCheck annotation rather than the policy list.
func TestPreRun_SkipConfigCheck_ByAnnotation(t *testing.T) {
	props := noConfigProps(t, "skipannot-tool")

	var childRan bool
	child := &cobra.Command{Use: "child", RunE: func(_ *cobra.Command, _ []string) error {
		childRan = true

		return nil
	}}
	setup.SkipConfigCheck(child)

	require.NoError(t, execChild(t, props, child))
	assert.True(t, childRan, "annotated command must run despite missing config")
	require.NotNil(t, props.Config)
}

// TestPreRun_AutoInitialise_HealsMissingConfig writes the default config and
// reloads it, instead of hard-failing.
func TestPreRun_AutoInitialise_HealsMissingConfig(t *testing.T) {
	props := noConfigProps(t, "autoinit-tool")
	props.Tool.Bootstrap = p.BootstrapPolicy{AutoInitialise: true}

	var childRan bool
	child := &cobra.Command{Use: "child", RunE: func(_ *cobra.Command, _ []string) error {
		childRan = true

		return nil
	}}

	require.NoError(t, execChild(t, props, child))
	assert.True(t, childRan, "command must run after auto-initialise heals the config")
	require.NotNil(t, props.Config)
	assert.True(t, exists(t, props.FS, autoInitConfigPath(props.FS, "autoinit-tool")),
		"auto-initialise must write the default config file")
}

// TestPreRun_AutoInitialise_NoOpWhenInitDisabled: with init disabled the tool
// already tolerates empty config, so auto-init must not run (no file written).
func TestPreRun_AutoInitialise_NoOpWhenInitDisabled(t *testing.T) {
	props := noConfigProps(t, "autoinit-noinit-tool", p.Disable(p.InitCmd))
	props.Tool.Bootstrap = p.BootstrapPolicy{AutoInitialise: true}

	child := &cobra.Command{Use: "child", RunE: func(_ *cobra.Command, _ []string) error { return nil }}

	require.NoError(t, execChild(t, props, child))
	require.NotNil(t, props.Config, "init-disabled tool still gets embedded defaults")
	assert.False(t, exists(t, props.FS, autoInitConfigPath(props.FS, "autoinit-noinit-tool")),
		"auto-initialise must be a no-op when init is disabled")
}

// TestPreRun_SkipConfigCheck_TakesPrecedenceOverAutoInitialise: a command that
// declared it owns bootstrap must not be auto-initialised for.
func TestPreRun_SkipConfigCheck_TakesPrecedenceOverAutoInitialise(t *testing.T) {
	props := noConfigProps(t, "precedence-tool")
	props.Tool.Bootstrap = p.BootstrapPolicy{
		AutoInitialise:  true,
		SkipConfigCheck: []string{"child"},
	}

	child := &cobra.Command{Use: "child", RunE: func(_ *cobra.Command, _ []string) error { return nil }}

	require.NoError(t, execChild(t, props, child))
	require.NotNil(t, props.Config)
	assert.False(t, exists(t, props.FS, autoInitConfigPath(props.FS, "precedence-tool")),
		"skip-config-check must take precedence: no auto-init file written")
}

// TestAutoInitialiseConfig_NoHomeDir covers the guard when the config dir cannot
// be resolved (HOME unset).
func TestAutoInitialiseConfig_NoHomeDir(t *testing.T) {
	t.Setenv("HOME", "")
	t.Setenv("USERPROFILE", "") // windows

	props := &p.Props{
		Logger: logger.NewNoop(),
		FS:     afero.NewMemMapFs(),
		Assets: p.NewAssets(),
		Tool:   p.Tool{Name: "nohome-tool"},
	}

	_, err := autoInitialiseConfig(t.Context(), props, ConfigLoadOptions{Props: props})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "config directory")
}

func exists(t *testing.T, fs afero.Fs, path string) bool {
	t.Helper()

	ok, err := afero.Exists(fs, path)
	require.NoError(t, err)

	return ok
}

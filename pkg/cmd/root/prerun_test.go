package root

import (
	"context"
	"log/slog"
	"testing"

	"github.com/spf13/afero"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go/errorhandling"

	forgetest "gitlab.com/phpboyscout/go/forge/test"

	"gitlab.com/phpboyscout/go-tool-base/internal/testutil"
	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	p "gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
	ver "gitlab.com/phpboyscout/go-tool-base/pkg/version"
)

// The root pre-run and its construction helpers, exercised hermetically: no
// network, keychain, TTY or wall-clock dependency.

// newUpdateProps builds a Props with the given current version and an injected
// in-memory release source. emptyConfig provides a usable, empty config
// container (GetBool/GetString return zero values).
func newUpdateProps(t *testing.T, currentVersion string, provider *forgetest.Source) *p.Props {
	t.Helper()

	fs := afero.NewMemMapFs()

	return &p.Props{
		Logger:  logger.NewBuffer(),
		FS:      fs,
		Config:  testutil.StoreFromYAML(t, "{}\n"),
		Version: ver.NewInfo(currentVersion, "", ""),
		Tool:    updateTool(provider),
	}
}

func mkUpdateCmd(t *testing.T) *cobra.Command {
	t.Helper()

	cmd := &cobra.Command{Use: "covtool"}
	cmd.Flags().Bool("ci", false, "ci flag")
	cmd.Flags().Bool("debug", false, "debug flag")
	cmd.SetContext(context.Background())

	return cmd
}

type assertErr struct{}

func (assertErr) Error() string { return "synthetic download failure" }

// --- Telemetry collector building -----------------------------------------

func telemetryProps(t *testing.T, tcfg p.TelemetryConfig, enabledFeature bool) *p.Props {
	t.Helper()

	fs := afero.NewMemMapFs()

	var feature p.FeatureState
	if enabledFeature {
		feature = p.Enable(p.TelemetryCmd)
	} else {
		feature = p.Disable(p.TelemetryCmd)
	}

	return &p.Props{
		Logger:  logger.NewBuffer(),
		FS:      fs,
		Config:  testutil.StoreFromYAML(t, "{}\n"),
		Version: ver.NewInfo("v1.0.0", "", ""),
		Tool: p.Tool{
			Name:      "covtelemetrytool",
			Telemetry: tcfg,
			Features:  p.SetFeatures(feature),
		},
	}
}

// --- selectTelemetryBackend branches --------------------------------------

// --- resolveVersionString --------------------------------------------------

func TestResolveVersionString(t *testing.T) {
	t.Parallel()

	withVersion := &p.Props{Version: ver.NewInfo("v1.2.3", "", "")}
	assert.Equal(t, "v1.2.3", withVersion.Version.GetVersion())

	unset := &p.Props{}
	assert.Empty(t, unset.Version.GetVersion(), "a zero Info reads as no version, no nil check needed")
}

// --- telemetry consent persistence -----------------------------------------

// --- NewCmdRootWithConfig --------------------------------------------------

func TestNewCmdRootWithConfig(t *testing.T) {
	t.Parallel()

	props := &p.Props{
		Logger: logger.NewNoop(),
		FS:     afero.NewMemMapFs(),
		Assets: p.NewAssets(),
		Tool: p.Tool{
			Name: "covwithconfig",
			Features: p.SetFeatures(
				p.Disable(p.UpdateCmd),
				p.Disable(p.InitCmd),
				p.Disable(p.McpCmd),
				p.Disable(p.DocsCmd),
				p.Disable(p.DoctorCmd),
			),
		},
	}

	cmd := NewCmdRootWithConfig(props, []string{"/custom/path.yaml"})
	require.NotNil(t, cmd)
	assert.Equal(t, "covwithconfig", cmd.Use)
}

// --- commandTreeHasPersistentPreRun ----------------------------------------

func TestCommandTreeHasPersistentPreRun(t *testing.T) {
	t.Parallel()

	// No hooks anywhere.
	plain := &cobra.Command{Use: "plain"}
	assert.False(t, commandTreeHasPersistentPreRun(plain))

	// Hook on the node itself (PersistentPreRunE).
	withHookE := &cobra.Command{
		Use:               "withHookE",
		PersistentPreRunE: func(*cobra.Command, []string) error { return nil },
	}
	assert.True(t, commandTreeHasPersistentPreRun(withHookE))

	// Hook on the node itself (PersistentPreRun, non-E variant).
	withHook := &cobra.Command{
		Use:              "withHook",
		PersistentPreRun: func(*cobra.Command, []string) {},
	}
	assert.True(t, commandTreeHasPersistentPreRun(withHook))

	// Hook on a descendant only.
	parent := &cobra.Command{Use: "parent"}
	child := &cobra.Command{
		Use:               "child",
		PersistentPreRunE: func(*cobra.Command, []string) error { return nil },
	}
	parent.AddCommand(child)
	assert.True(t, commandTreeHasPersistentPreRun(parent))
}

// --- promptTelemetryConsent early-return branches --------------------------
//
// The interactive form.Run() success tail is TTY-bound and intentionally left
// uncovered; every non-interactive early-return is exercised here.

func consentProps(t *testing.T, cfgYAML string, featureEnabled bool) *p.Props {
	t.Helper()

	fs := afero.NewMemMapFs()

	var feature p.FeatureState
	if featureEnabled {
		feature = p.Enable(p.TelemetryCmd)
	} else {
		feature = p.Disable(p.TelemetryCmd)
	}

	if cfgYAML == "" {
		cfgYAML = "{}\n"
	}

	cfg := testutil.StoreFromYAML(t, cfgYAML)

	return &p.Props{
		Logger: logger.NewBuffer(),
		FS:     fs,
		Config: cfg,
		Tool: p.Tool{
			Name:     "covconsenttool",
			Features: p.SetFeatures(feature),
		},
	}
}

// The former TestPromptTelemetryConsent_NonInteractiveFormFails relied on huh
// erroring out on a non-terminal stdin to exercise the form-error tail. That
// reliance is exactly what this fix removes: the prompt is now gated on
// utils.IsInteractive() before the form is built. The deferred (non-interactive)
// path and the interactive form-error tail are covered deterministically by
// TestPromptTelemetryConsent_NonInteractiveSkipsPrompt and
// TestPromptTelemetryConsent_InteractiveReachesForm in
// prerun_prompt_tty_guard_test.go.

// --- registerFeatureCommands: ConfigCmd + TelemetryCmd enabled --------------

func TestRegisterFeatureCommands_ConfigAndTelemetryEnabled(t *testing.T) {
	t.Parallel()

	props := &p.Props{
		Logger: logger.NewNoop(),
		FS:     afero.NewMemMapFs(),
		Assets: p.NewAssets(),
		Tool: p.Tool{
			Name: "covfeaturetool",
			Features: p.SetFeatures(
				p.Disable(p.UpdateCmd),
				p.Disable(p.InitCmd),
				p.Disable(p.McpCmd),
				p.Disable(p.DocsCmd),
				p.Disable(p.DoctorCmd),
				p.Enable(p.ConfigCmd),
				p.Enable(p.TelemetryCmd),
				p.Enable(p.ChangelogCmd),
			),
		},
	}

	cmd := NewCmdRoot(props)

	names := map[string]bool{}
	for _, c := range cmd.Commands() {
		names[c.Name()] = true
	}

	assert.True(t, names["config"], "config command should be registered")
	assert.True(t, names["telemetry"], "telemetry command should be registered")
	assert.True(t, names["changelog"], "changelog command should be registered")
}

// TestConfigIndependentBuiltinsSkipConfigGate pins that built-in commands which
// read nothing from config (version, changelog, man, docs) carry the
// skip-config-check annotation, so they run on a fresh install before any
// config file exists — while a config-dependent command (config) does not.
func TestConfigIndependentBuiltinsSkipConfigGate(t *testing.T) {
	t.Parallel()

	props := &p.Props{
		Logger: logger.NewNoop(),
		FS:     afero.NewMemMapFs(),
		Assets: p.NewAssets(),
		Tool: p.Tool{
			Name: "skipgatetool",
			Features: p.SetFeatures(
				p.Enable(p.ChangelogCmd),
				p.Enable(p.ManCmd),
				p.Enable(p.DocsCmd),
				p.Enable(p.ConfigCmd),
			),
		},
	}

	cmd := NewCmdRoot(props)

	byName := map[string]*cobra.Command{}
	for _, c := range cmd.Commands() {
		byName[c.Name()] = c
	}

	for _, name := range []string{"version", "changelog", "man", "docs"} {
		sub := byName[name]
		require.NotNil(t, sub, "%s must be registered", name)
		assert.True(t, setup.SkipsConfigCheck(sub), "%s must skip the config gate", name)
	}

	require.NotNil(t, byName["config"])
	assert.False(t, setup.SkipsConfigCheck(byName["config"]),
		"config is config-dependent and must not skip the gate")
}

// --- newRootPreRunE: init-skip and update-exit paths -----------------------

// preRunProps builds Props wired so the PersistentPreRunE closure runs end to
// end against an injected in-memory release source.
func preRunProps(t *testing.T, currentVersion string, provider *forgetest.Source) *p.Props {
	t.Helper()

	fs := afero.NewMemMapFs()

	return &p.Props{
		Logger:       logger.NewBuffer(),
		FS:           fs,
		Assets:       nil, // nil skips embedded-config load (no assets/init/config.yaml fixture)
		Config:       testutil.StoreFromYAML(t, "{}\n"),
		ErrorHandler: errorhandling.New(logger.ToSlog(logger.NewNoop()), nil),
		Version:      ver.NewInfo(currentVersion, "", ""),
		Tool: p.Tool{
			Name: "covprerun",
			ReleaseSource: p.ReleaseSource{
				Type:  "github",
				Owner: "example",
				Repo:  "covprerun",
			},
			ReleaseProvider: provider,
			Features: p.SetFeatures(
				p.Disable(p.InitCmd),
				p.Disable(p.McpCmd),
				p.Disable(p.DocsCmd),
				p.Disable(p.DoctorCmd),
				p.Disable(p.TelemetryCmd),
			),
		},
	}
}

func preRunCmd(t *testing.T) *cobra.Command {
	t.Helper()

	cmd := &cobra.Command{Use: "covprerun"}
	cmd.Flags().Bool("ci", false, "ci flag")
	cmd.Flags().Bool("debug", false, "debug flag")
	cmd.SetContext(context.Background())

	return cmd
}

// TestNewRootPreRunE_InitCmdSkipsConfig proves the InitCmd fast-path: when the
// dispatched command is the init feature, config loading is skipped and only
// the debug log level is applied.
func TestNewRootPreRunE_InitCmdSkipsConfig(t *testing.T) {
	t.Parallel()

	props := &p.Props{
		Logger: logger.NewBuffer(),
		FS:     afero.NewMemMapFs(),
		Tool:   p.Tool{Name: "covinit"},
	}

	mcpLogLevel := &slog.LevelVar{}
	state := newRootState()
	preRun := newRootPreRunE(props, nil, mcpLogLevel, state, map[string]*pflag.Flag{})

	// setup.Wrap stamps the InitCmd feature annotation the closure checks.
	wrapped := setup.Wrap(p.InitCmd, &cobra.Command{Use: "init"})
	cmd := wrapped.Command
	cmd.Flags().Bool("ci", false, "ci flag")
	cmd.Flags().Bool("debug", true, "debug flag")
	require.NoError(t, cmd.Flags().Set("debug", "true"))
	cmd.SetContext(context.Background())

	require.NoError(t, preRun(cmd, nil))

	assert.Nil(t, props.Config, "init path must skip config loading")
	assert.Equal(t, slog.LevelDebug, mcpLogLevel.Level(), "debug flag must still apply on init path")
}

// TestNewRootPreRunE_UpdateExit proves the closure returns ErrUpdateComplete
// when an accepted update reports ShouldExit. Driven hermetically via the
// injected release source and an accepting form creator.
func TestNewRootPreRunE_UpdateExit(t *testing.T) {
	// Not parallel: update.Update writes the extracted binary to the
	// HOME-derived data dir on a real FS. Neutralise any ambient CI env so the
	// update check is not skipped (Config.GetBool("ci") reads CI via AutomaticEnv).
	t.Setenv("HOME", t.TempDir())
	t.Setenv("CI", "")

	const tool = "covprerunexit"

	asset := forgetest.TarGzAsset(tool, tool, "#!/bin/sh\necho new\n")
	provider := forgetest.New(forgetest.WithRelease("v2.0.0", asset))

	fs := afero.NewOsFs()
	props := &p.Props{
		Logger:       logger.NewBuffer(),
		FS:           fs,
		Assets:       nil, // nil skips embedded-config load (no assets/init/config.yaml fixture)
		Config:       testutil.StoreFromYAML(t, "{}\n"),
		ErrorHandler: errorhandling.New(logger.ToSlog(logger.NewNoop()), nil),
		Version:      ver.NewInfo("v1.0.0", "", ""),
		Tool: p.Tool{
			Name: tool,
			// Prompt policy: an outdated binary consults the form (which we
			// accept below). Disabled (the framework default) would only log.
			UpdatePolicy: p.UpdatePolicyPrompt,
			ReleaseSource: p.ReleaseSource{
				Type:  "github",
				Owner: "example",
				Repo:  tool,
			},
			ReleaseProvider: provider,
			Features: p.SetFeatures(
				p.Disable(p.InitCmd),
				p.Disable(p.McpCmd),
				p.Disable(p.DocsCmd),
				p.Disable(p.DoctorCmd),
				p.Disable(p.TelemetryCmd),
				p.Disable(p.ChangelogCmd),
			),
		},
	}

	// Accept the update at the prompt.
	props.IO = promptIO("y")

	mcpLogLevel := &slog.LevelVar{}
	state := newRootState()

	preRun := newRootPreRunE(props, nil, mcpLogLevel, state, map[string]*pflag.Flag{})

	cmd := preRunCmd(t)
	err := preRun(cmd, nil)

	require.ErrorIs(t, err, ErrUpdateComplete, "accepted update must return ErrUpdateComplete")
}

// TestNewRootPreRunE_UpToDate runs the full closure to completion against an
// up-to-date release source: config is loaded, telemetry collector wired, and
// the update check returns no exit.
func TestNewRootPreRunE_UpToDate(t *testing.T) {
	// Not parallel: the update check stamps a HOME-derived marker file.
	t.Setenv("HOME", t.TempDir())

	provider := forgetest.New(forgetest.WithRelease("v1.0.0"))
	props := preRunProps(t, "v1.0.0", provider)
	props.FS = afero.NewOsFs()
	props.Config = testutil.StoreFromYAML(t, "{}\n")

	mcpLogLevel := &slog.LevelVar{}
	state := newRootState()
	preRun := newRootPreRunE(props, nil, mcpLogLevel, state, map[string]*pflag.Flag{})

	require.NoError(t, preRun(preRunCmd(t), nil))
	require.NotNil(t, props.Config, "config must be loaded")
	require.NotNil(t, props.Collector, "telemetry collector must be wired")
}

// TestNewRootPreRunE_UpdateDisabledReturnsEarly covers the UpdateCmd-disabled
// branch that returns before any update check.
func TestNewRootPreRunE_UpdateDisabledReturnsEarly(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	props := &p.Props{
		Logger:       logger.NewBuffer(),
		FS:           fs,
		Assets:       nil, // nil skips embedded-config load (no assets/init/config.yaml fixture)
		Config:       testutil.StoreFromYAML(t, "{}\n"),
		ErrorHandler: errorhandling.New(logger.ToSlog(logger.NewNoop()), nil),
		Version:      ver.NewInfo("v1.0.0", "", ""),
		Tool: p.Tool{
			Name: "covnoupdate",
			Features: p.SetFeatures(
				p.Disable(p.InitCmd),
				p.Disable(p.UpdateCmd),
				p.Disable(p.TelemetryCmd),
			),
		},
	}

	mcpLogLevel := &slog.LevelVar{}
	state := newRootState()
	preRun := newRootPreRunE(props, nil, mcpLogLevel, state, map[string]*pflag.Flag{})

	require.NoError(t, preRun(preRunCmd(t), nil))
	require.NotNil(t, props.Config)
}

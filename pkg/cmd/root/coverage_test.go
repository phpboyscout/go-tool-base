package root

import (
	"context"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"

	"charm.land/huh/v2"
	"github.com/spf13/afero"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go/config"

	"gitlab.com/phpboyscout/go-tool-base/pkg/errorhandling"
	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	p "gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
	"gitlab.com/phpboyscout/go-tool-base/pkg/telemetry"
	"gitlab.com/phpboyscout/go-tool-base/pkg/vcs/release/releasetest"
	ver "gitlab.com/phpboyscout/go-tool-base/pkg/version"
)

// coverage_test.go raises coverage for the self-update check, telemetry
// collector building, and bootstrap-construction helpers in root.go. The
// self-update functions are exercised hermetically by injecting an in-memory
// release source via props.Tool.ReleaseProvider and pinning a deterministic
// current version with version.NewInfo — no network, keychain, TTY, or
// wall-clock dependency.

// updateTool returns Tool metadata wired for hermetic self-update tests: a
// release source the injected provider satisfies, with the update command
// enabled and other prompt/network features off.
func updateTool(provider *releasetest.Source) p.Tool {
	return p.Tool{
		Name: "covtool",
		ReleaseSource: p.ReleaseSource{
			Type:  "github",
			Owner: "example",
			Repo:  "covtool",
		},
		ReleaseProvider: provider,
		Features: p.SetFeatures(
			p.Disable(p.InitCmd),
			p.Disable(p.McpCmd),
			p.Disable(p.DocsCmd),
			p.Disable(p.DoctorCmd),
			p.Disable(p.TelemetryCmd),
		),
	}
}

// newUpdateProps builds a Props with the given current version and an injected
// in-memory release source. emptyConfig provides a usable, empty config
// container (GetBool/GetString return zero values).
func newUpdateProps(t *testing.T, currentVersion string, provider *releasetest.Source) *p.Props {
	t.Helper()

	fs := afero.NewMemMapFs()

	return &p.Props{
		Logger:  logger.NewBuffer(),
		FS:      fs,
		Config:  config.NewReaderContainer(fs),
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

// TestCheckForUpdates_UpToDate exercises the happy path where the running
// binary already matches the latest release: no error, no exit, and the latest
// version is reported via the logger.
func TestCheckForUpdates_UpToDate(t *testing.T) {
	setup.ResetRegistryForTesting()
	t.Cleanup(setup.ResetRegistryForTesting)
	t.Parallel()

	provider := releasetest.New(releasetest.WithRelease("v1.0.0"))
	props := newUpdateProps(t, "v1.0.0", provider)
	state := newRootState()
	flags := &FlagValues{}

	result := checkForUpdates(context.Background(), mkUpdateCmd(t), props, flags, state)

	require.NotNil(t, result)
	require.NoError(t, result.Error)
	assert.False(t, result.ShouldExit)
	assert.False(t, result.HasUpdated)
}

// TestCheckForUpdates_OutdatedDeclines covers the outdated branch under the
// default (disabled) policy: the latest version is newer than the running
// binary, so handleOutdatedVersion runs, logs availability, and records the
// cached version — but does not block or exit.
func TestCheckForUpdates_OutdatedDeclines(t *testing.T) {
	setup.ResetRegistryForTesting()
	t.Cleanup(setup.ResetRegistryForTesting)
	t.Parallel()

	provider := releasetest.New(releasetest.WithRelease("v2.0.0"))
	props := newUpdateProps(t, "v1.0.0", provider)
	state := newRootState()
	flags := &FlagValues{}

	result := checkForUpdates(context.Background(), mkUpdateCmd(t), props, flags, state)

	require.NotNil(t, result)
	// Default policy is "disabled": available update is logged, not blocked.
	require.NoError(t, result.Error)
	assert.False(t, result.ShouldExit)
}

// TestCheckForUpdates_SkippedWhenDevelopment proves the skip path: a
// development version short-circuits before any provider call.
func TestCheckForUpdates_SkippedWhenDevelopment(t *testing.T) {
	setup.ResetRegistryForTesting()
	t.Cleanup(setup.ResetRegistryForTesting)
	t.Parallel()

	provider := releasetest.New(releasetest.WithRelease("v2.0.0"))
	props := newUpdateProps(t, "v0.0.0-dev", provider)
	state := newRootState()
	flags := &FlagValues{}

	result := checkForUpdates(context.Background(), mkUpdateCmd(t), props, flags, state)

	require.NotNil(t, result)
	require.NoError(t, result.Error)
	assert.False(t, result.ShouldExit)
}

// TestCheckForUpdates_EnabledPolicyBlocks proves that under the "enabled"
// policy an outdated binary with a declined (non-interactive) prompt becomes a
// hard error rather than a masked continue.
func TestCheckForUpdates_EnabledPolicyBlocks(t *testing.T) {
	setup.ResetRegistryForTesting()
	t.Cleanup(setup.ResetRegistryForTesting)
	// Not parallel: neutralises any ambient CI env so the update check is not
	// skipped (Config.GetBool("ci") reads the CI env via viper AutomaticEnv).
	t.Setenv("CI", "")

	provider := releasetest.New(releasetest.WithRelease("v2.0.0"))
	props := newUpdateProps(t, "v1.0.0", provider)
	props.Tool.UpdatePolicy = p.UpdatePolicyEnabled
	// Decline deterministically: a nil-returning form creator that leaves
	// runUpdate false simulates a non-interactive environment.
	state := newRootState()
	state.formCreator = func(runUpdate *bool) *huh.Form { *runUpdate = false; return nil }

	flags := &FlagValues{}

	result := checkForUpdates(context.Background(), mkUpdateCmd(t), props, flags, state)

	require.NotNil(t, result)
	require.Error(t, result.Error, "enabled policy must block on a declined required update")
}

// TestWarnIfBehindCached covers the cached-version reminder path.
func TestWarnIfBehindCached(t *testing.T) {
	setup.ResetRegistryForTesting()
	t.Cleanup(setup.ResetRegistryForTesting)
	// Not parallel: writes the cached-version marker via HOME-derived dir.
	t.Setenv("HOME", t.TempDir())

	log := logger.NewBuffer()
	fs := afero.NewOsFs()
	props := &p.Props{
		Logger:  log,
		FS:      fs,
		Config:  config.NewReaderContainer(fs),
		Version: ver.NewInfo("v1.0.0", "", ""),
		Tool:    p.Tool{Name: "covcachetool"},
	}

	// No cached version yet -> no warning.
	warnIfBehindCached(props)
	assert.False(t, log.Contains("a newer"))

	// Record a newer cached version, then warn.
	require.NoError(t, setup.SetCheckedVersion(fs, props.Tool.Name, "v2.0.0"))
	warnIfBehindCached(props)
	assert.True(t, log.Contains("a newer"), "must warn when cached latest is newer than running binary")
}

// TestWarnIfBehindCached_DevelopmentSkips proves a development build never
// emits the cached reminder.
func TestWarnIfBehindCached_DevelopmentSkips(t *testing.T) {
	setup.ResetRegistryForTesting()
	t.Cleanup(setup.ResetRegistryForTesting)
	t.Parallel()

	log := logger.NewBuffer()
	props := &p.Props{
		Logger:  log,
		FS:      afero.NewMemMapFs(),
		Version: ver.NewInfo("v0.0.0-dev", "", ""),
		Tool:    p.Tool{Name: "covdevtool"},
	}

	warnIfBehindCached(props)
	assert.False(t, log.Contains("a newer"))
}

// TestWarnIfBehindCached_NilVersion proves the nil-Version guard.
func TestWarnIfBehindCached_NilVersion(t *testing.T) {
	setup.ResetRegistryForTesting()
	t.Cleanup(setup.ResetRegistryForTesting)
	t.Parallel()

	log := logger.NewBuffer()
	props := &p.Props{
		Logger: log,
		FS:     afero.NewMemMapFs(),
		Tool:   p.Tool{Name: "covniltool"},
	}

	assert.NotPanics(t, func() { warnIfBehindCached(props) })
}

// TestRecordCheckedVersion_Outdated stores the latest version in the marker
// body when the binary is behind.
func TestRecordCheckedVersion_Outdated(t *testing.T) {
	setup.ResetRegistryForTesting()
	t.Cleanup(setup.ResetRegistryForTesting)
	// Not parallel: writes the marker via HOME-derived dir.
	t.Setenv("HOME", t.TempDir())

	provider := releasetest.New(releasetest.WithRelease("v2.0.0"))
	fs := afero.NewOsFs()
	props := &p.Props{
		Logger:  logger.NewBuffer(),
		FS:      fs,
		Config:  config.NewReaderContainer(fs),
		Version: ver.NewInfo("v1.0.0", "", ""),
		Tool:    updateTool(provider),
	}

	updater, err := setup.NewUpdater(context.Background(), props, "", false)
	require.NoError(t, err)

	recordCheckedVersion(context.Background(), props, updater, false)

	assert.Equal(t, "v2.0.0", setup.GetCheckedVersion(fs, props.Tool.Name),
		"outdated check must cache the latest version in the marker body")
}

// TestRecordCheckedVersion_UpToDate clears the marker body when current.
func TestRecordCheckedVersion_UpToDate(t *testing.T) {
	setup.ResetRegistryForTesting()
	t.Cleanup(setup.ResetRegistryForTesting)
	t.Setenv("HOME", t.TempDir())

	provider := releasetest.New(releasetest.WithRelease("v1.0.0"))
	fs := afero.NewOsFs()
	props := &p.Props{
		Logger:  logger.NewBuffer(),
		FS:      fs,
		Config:  config.NewReaderContainer(fs),
		Version: ver.NewInfo("v1.0.0", "", ""),
		Tool:    updateTool(provider),
	}

	// Seed a stale cached version that must be cleared.
	require.NoError(t, setup.SetCheckedVersion(fs, props.Tool.Name, "v9.9.9"))

	updater, err := setup.NewUpdater(context.Background(), props, "", false)
	require.NoError(t, err)

	recordCheckedVersion(context.Background(), props, updater, true)

	assert.Empty(t, setup.GetCheckedVersion(fs, props.Tool.Name),
		"up-to-date check must clear the cached version body")
}

// TestPerformUpdate_Success drives the self-update accept path entirely from
// the in-memory release source: a tar.gz binary asset is served and extracted,
// and the result flags exit-and-rerun.
func TestPerformUpdate_Success(t *testing.T) {
	setup.ResetRegistryForTesting()
	t.Cleanup(setup.ResetRegistryForTesting)
	// Not parallel: update.Update writes the extracted binary + markers to the
	// HOME-derived data dir on a real FS.
	t.Setenv("HOME", t.TempDir())

	const tool = "covupdtool"

	asset := releasetest.TarGzAsset(tool, tool, "#!/bin/sh\necho updated\n")
	provider := releasetest.New(releasetest.WithRelease("v2.0.0", asset))

	fs := afero.NewOsFs()
	props := &p.Props{
		Logger:  logger.NewBuffer(),
		FS:      fs,
		Config:  config.NewReaderContainer(fs),
		Version: ver.NewInfo("v1.0.0", "", ""),
		Tool: p.Tool{
			Name: tool,
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

	state := newRootState()
	result := &UpdateCheckResult{}

	performUpdate(context.Background(), props, result, state)

	require.NoError(t, result.Error)
	assert.True(t, state.redirectingToUpdate)
	assert.True(t, result.HasUpdated)
	assert.True(t, result.ShouldExit)
}

// TestPerformUpdate_DownloadError covers the failure tail: a provider that
// errors on download surfaces result.Error without HasUpdated/ShouldExit.
func TestPerformUpdate_DownloadError(t *testing.T) {
	setup.ResetRegistryForTesting()
	t.Cleanup(setup.ResetRegistryForTesting)
	t.Setenv("HOME", t.TempDir())

	const tool = "covupderrtool"

	asset := releasetest.TarGzAsset(tool, tool, "binary")
	provider := releasetest.New(
		releasetest.WithRelease("v2.0.0", asset),
		releasetest.WithDownloadError(assertErr{}),
	)

	fs := afero.NewOsFs()
	props := &p.Props{
		Logger:  logger.NewBuffer(),
		FS:      fs,
		Config:  config.NewReaderContainer(fs),
		Version: ver.NewInfo("v1.0.0", "", ""),
		Tool: p.Tool{
			Name: tool,
			ReleaseSource: p.ReleaseSource{
				Type:  "github",
				Owner: "example",
				Repo:  tool,
			},
			ReleaseProvider: provider,
			Features:        p.SetFeatures(p.Disable(p.InitCmd), p.Disable(p.TelemetryCmd)),
		},
	}

	state := newRootState()
	result := &UpdateCheckResult{}

	performUpdate(context.Background(), props, result, state)

	require.Error(t, result.Error, "a failed download must surface result.Error")
	assert.False(t, result.HasUpdated)
	assert.False(t, result.ShouldExit)
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
		Config:  config.NewReaderContainer(fs),
		Version: ver.NewInfo("v1.0.0", "", ""),
		Tool: p.Tool{
			Name:      "covtelemetrytool",
			Telemetry: tcfg,
			Features:  p.SetFeatures(feature),
		},
	}
}

func TestBuildTelemetryCollector_FeatureDisabled(t *testing.T) {
	setup.ResetRegistryForTesting()
	t.Cleanup(setup.ResetRegistryForTesting)
	t.Parallel()

	props := telemetryProps(t, p.TelemetryConfig{}, false)
	c := buildTelemetryCollector(context.Background(), props)
	require.NotNil(t, c)
	assert.False(t, c.Enabled(), "telemetry feature disabled -> noop collector")
}

func TestBuildTelemetryCollector_EnabledFeatureButConfigOff(t *testing.T) {
	setup.ResetRegistryForTesting()
	t.Cleanup(setup.ResetRegistryForTesting)
	t.Parallel()

	// Feature enabled but telemetry.enabled is false in config -> noop.
	props := telemetryProps(t, p.TelemetryConfig{}, true)
	c := buildTelemetryCollector(context.Background(), props)
	require.NotNil(t, c)
	assert.False(t, c.Enabled())
}

func TestBuildTelemetryCollector_ForceEnabledHTTPBackend(t *testing.T) {
	setup.ResetRegistryForTesting()
	t.Cleanup(setup.ResetRegistryForTesting)
	t.Parallel()

	props := telemetryProps(t, p.TelemetryConfig{
		ForceEnabled: true,
		Endpoint:     "https://telemetry.example.com/v1/events",
	}, true)

	c := buildTelemetryCollector(context.Background(), props)
	require.NotNil(t, c)
	assert.True(t, c.Enabled())
	assert.Contains(t, c.BackendInfo(), "http")
}

func TestBuildTelemetryCollector_EnvOverrideEnablesLocalFile(t *testing.T) {
	setup.ResetRegistryForTesting()
	t.Cleanup(setup.ResetRegistryForTesting)
	// Not parallel: sets TELEMETRY_ENABLED / TELEMETRY_LOCAL env vars.
	t.Setenv("TELEMETRY_ENABLED", "true")
	t.Setenv("TELEMETRY_LOCAL", "true")
	t.Setenv("HOME", t.TempDir())

	props := telemetryProps(t, p.TelemetryConfig{}, true)

	c := buildTelemetryCollector(context.Background(), props)
	require.NotNil(t, c)
	assert.True(t, c.Enabled())
	assert.Contains(t, c.BackendInfo(), "file")
}

// --- selectTelemetryBackend branches --------------------------------------

func TestSelectTelemetryBackend_CustomBackend(t *testing.T) {
	setup.ResetRegistryForTesting()
	t.Cleanup(setup.ResetRegistryForTesting)
	t.Parallel()

	props := telemetryProps(t, p.TelemetryConfig{
		Backend: func(*p.Props) any { return telemetry.NewNoopBackend() },
	}, true)

	b, info := selectTelemetryBackend(context.Background(), props, telemetry.Config{}, t.TempDir())
	require.NotNil(t, b)
	assert.Equal(t, "custom", info)
}

func TestSelectTelemetryBackend_InvalidCustomBackend(t *testing.T) {
	setup.ResetRegistryForTesting()
	t.Cleanup(setup.ResetRegistryForTesting)
	t.Parallel()

	props := telemetryProps(t, p.TelemetryConfig{
		Backend: func(*p.Props) any { return "not a backend" },
	}, true)

	b, info := selectTelemetryBackend(context.Background(), props, telemetry.Config{}, t.TempDir())
	require.NotNil(t, b)
	assert.Contains(t, info, "noop")
}

func TestSelectTelemetryBackend_LocalOnly(t *testing.T) {
	setup.ResetRegistryForTesting()
	t.Cleanup(setup.ResetRegistryForTesting)
	t.Parallel()

	props := telemetryProps(t, p.TelemetryConfig{}, true)

	b, info := selectTelemetryBackend(context.Background(), props, telemetry.Config{LocalOnly: true}, t.TempDir())
	require.NotNil(t, b)
	assert.Contains(t, info, "file")
}

func TestSelectTelemetryBackend_OTel(t *testing.T) {
	setup.ResetRegistryForTesting()
	t.Cleanup(setup.ResetRegistryForTesting)
	t.Parallel()

	props := telemetryProps(t, p.TelemetryConfig{
		OTelEndpoint: "https://collector.example.com:4318",
		OTelInsecure: true,
		OTelHeaders:  map[string]string{"x-token": "abc"},
	}, true)

	b, info := selectTelemetryBackend(context.Background(), props, telemetry.Config{}, t.TempDir())
	require.NotNil(t, b)
	assert.Contains(t, info, "otlp")
}

func TestSelectTelemetryBackend_HTTP(t *testing.T) {
	setup.ResetRegistryForTesting()
	t.Cleanup(setup.ResetRegistryForTesting)
	t.Parallel()

	props := telemetryProps(t, p.TelemetryConfig{
		Endpoint: "https://telemetry.example.com/events",
	}, true)

	b, info := selectTelemetryBackend(context.Background(), props, telemetry.Config{}, t.TempDir())
	require.NotNil(t, b)
	assert.Contains(t, info, "http")
}

func TestSelectTelemetryBackend_DefaultNoop(t *testing.T) {
	setup.ResetRegistryForTesting()
	t.Cleanup(setup.ResetRegistryForTesting)
	t.Parallel()

	props := telemetryProps(t, p.TelemetryConfig{}, true)

	b, info := selectTelemetryBackend(context.Background(), props, telemetry.Config{}, t.TempDir())
	require.NotNil(t, b)
	assert.Contains(t, info, "noop")
}

// --- resolveVersionString --------------------------------------------------

func TestResolveVersionString(t *testing.T) {
	setup.ResetRegistryForTesting()
	t.Cleanup(setup.ResetRegistryForTesting)
	t.Parallel()

	withVersion := &p.Props{Version: ver.NewInfo("v1.2.3", "", "")}
	assert.Equal(t, "v1.2.3", resolveVersionString(withVersion))

	nilVersion := &p.Props{}
	assert.Empty(t, resolveVersionString(nilVersion))
}

// --- ensureMinimalConfig ---------------------------------------------------

func TestEnsureMinimalConfig_WritesFile(t *testing.T) {
	setup.ResetRegistryForTesting()
	t.Cleanup(setup.ResetRegistryForTesting)
	// Not parallel: GetDefaultConfigDir derives the path from HOME.
	home := t.TempDir()
	t.Setenv("HOME", home)

	fs := afero.NewMemMapFs()
	props := &p.Props{
		Logger: logger.NewBuffer(),
		FS:     fs,
		Tool:   p.Tool{Name: "covminimaltool"},
	}

	v := viper.New()
	v.SetFs(fs)

	require.NoError(t, ensureMinimalConfig(props, v, true))

	cfgPath := filepath.Join(setup.GetDefaultConfigDir(fs, props.Tool.Name), setup.DefaultConfigFilename)
	exists, err := afero.Exists(fs, cfgPath)
	require.NoError(t, err)
	assert.True(t, exists, "minimal config file must be written")

	loaded := viper.New()
	loaded.SetFs(fs)
	loaded.SetConfigFile(cfgPath)
	loaded.SetConfigType("yaml")
	require.NoError(t, loaded.ReadInConfig())
	assert.True(t, loaded.GetBool("telemetry.enabled"))
}

// TestEnsureMinimalConfig_NoConfigDir proves the empty-config-dir guard. When
// HOME is unresolvable GetDefaultConfigDir returns "" and ensureMinimalConfig
// errors rather than writing to a bogus path.
func TestEnsureMinimalConfig_NoConfigDir(t *testing.T) {
	setup.ResetRegistryForTesting()
	t.Cleanup(setup.ResetRegistryForTesting)
	// Not parallel: clears HOME so os.UserHomeDir fails.
	t.Setenv("HOME", "")
	t.Setenv("USERPROFILE", "") // windows fallback

	props := &p.Props{
		Logger: logger.NewBuffer(),
		FS:     afero.NewMemMapFs(),
		Tool:   p.Tool{Name: "covminimaltool"},
	}

	v := viper.New()
	v.SetFs(props.FS)

	err := ensureMinimalConfig(props, v, true)
	require.Error(t, err, "unresolvable config dir must error")
}

// --- NewCmdRootWithConfig --------------------------------------------------

func TestNewCmdRootWithConfig(t *testing.T) {
	setup.ResetRegistryForTesting()
	t.Cleanup(setup.ResetRegistryForTesting)
	// Not parallel: NewCmdRoot seals the process-global middleware registry.

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
	setup.ResetRegistryForTesting()
	t.Cleanup(setup.ResetRegistryForTesting)
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

	cfg := config.NewReaderContainer(fs,
		config.WithConfigFormat("yaml"),
		config.WithConfigReaders(strings.NewReader(cfgYAML)),
	)

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

func TestPromptTelemetryConsent_FeatureDisabled(t *testing.T) {
	setup.ResetRegistryForTesting()
	t.Cleanup(setup.ResetRegistryForTesting)
	t.Parallel()

	props := consentProps(t, "", false)
	assert.NotPanics(t, func() {
		promptTelemetryConsent(props, &FlagValues{})
	})
}

func TestPromptTelemetryConsent_ForceEnabled(t *testing.T) {
	setup.ResetRegistryForTesting()
	t.Cleanup(setup.ResetRegistryForTesting)
	t.Parallel()

	props := consentProps(t, "", true)
	props.Tool.Telemetry.ForceEnabled = true
	assert.NotPanics(t, func() {
		promptTelemetryConsent(props, &FlagValues{})
	})
}

func TestPromptTelemetryConsent_EnvSet(t *testing.T) {
	setup.ResetRegistryForTesting()
	t.Cleanup(setup.ResetRegistryForTesting)
	// Not parallel: sets TELEMETRY_ENABLED.
	t.Setenv("TELEMETRY_ENABLED", "true")

	props := consentProps(t, "", true)
	assert.NotPanics(t, func() {
		promptTelemetryConsent(props, &FlagValues{})
	})
}

func TestPromptTelemetryConsent_AlreadyConfigured(t *testing.T) {
	setup.ResetRegistryForTesting()
	t.Cleanup(setup.ResetRegistryForTesting)
	t.Parallel()

	props := consentProps(t, "telemetry:\n  enabled: true\n", true)
	require.True(t, props.Config.IsSet("telemetry.enabled"))
	assert.NotPanics(t, func() {
		promptTelemetryConsent(props, &FlagValues{})
	})
}

func TestPromptTelemetryConsent_CIFlagSkips(t *testing.T) {
	setup.ResetRegistryForTesting()
	t.Cleanup(setup.ResetRegistryForTesting)
	t.Parallel()

	props := consentProps(t, "", true)
	assert.NotPanics(t, func() {
		promptTelemetryConsent(props, &FlagValues{CI: true})
	})
}

// TestPromptTelemetryConsent_NonInteractiveFormFails exercises the body past
// the early returns: with no TTY the form fails immediately and the function
// logs and returns without persisting. Covers the form-error tail.
func TestPromptTelemetryConsent_NonInteractiveFormFails(t *testing.T) {
	setup.ResetRegistryForTesting()
	t.Cleanup(setup.ResetRegistryForTesting)
	t.Parallel()

	// Feature enabled, not force-enabled, env unset, config not set, not CI:
	// reaches form.Run(), which errors without a TTY.
	props := consentProps(t, "", true)
	assert.NotPanics(t, func() {
		promptTelemetryConsent(props, &FlagValues{})
	})
}

// --- registerFeatureCommands: ConfigCmd + TelemetryCmd enabled --------------

func TestRegisterFeatureCommands_ConfigAndTelemetryEnabled(t *testing.T) {
	setup.ResetRegistryForTesting()
	t.Cleanup(setup.ResetRegistryForTesting)
	// Not parallel: seals the process-global middleware registry.

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

// --- newRootPreRunE: init-skip and update-exit paths -----------------------

// preRunProps builds Props wired so the PersistentPreRunE closure runs end to
// end against an injected in-memory release source.
func preRunProps(t *testing.T, currentVersion string, provider *releasetest.Source) *p.Props {
	t.Helper()

	fs := afero.NewMemMapFs()

	return &p.Props{
		Logger:       logger.NewBuffer(),
		FS:           fs,
		Assets:       nil, // nil skips embedded-config load (no assets/init/config.yaml fixture)
		Config:       config.NewReaderContainer(fs),
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
	setup.ResetRegistryForTesting()
	t.Cleanup(setup.ResetRegistryForTesting)
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
	setup.ResetRegistryForTesting()
	t.Cleanup(setup.ResetRegistryForTesting)
	// Not parallel: update.Update writes the extracted binary to the
	// HOME-derived data dir on a real FS. Neutralise any ambient CI env so the
	// update check is not skipped (Config.GetBool("ci") reads CI via AutomaticEnv).
	t.Setenv("HOME", t.TempDir())
	t.Setenv("CI", "")

	const tool = "covprerunexit"

	asset := releasetest.TarGzAsset(tool, tool, "#!/bin/sh\necho new\n")
	provider := releasetest.New(releasetest.WithRelease("v2.0.0", asset))

	fs := afero.NewOsFs()
	props := &p.Props{
		Logger:       logger.NewBuffer(),
		FS:           fs,
		Assets:       nil, // nil skips embedded-config load (no assets/init/config.yaml fixture)
		Config:       config.NewReaderContainer(fs),
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

	mcpLogLevel := &slog.LevelVar{}
	state := newRootState()
	// Accept the update deterministically (no TTY).
	state.formCreator = func(runUpdate *bool) *huh.Form { *runUpdate = true; return nil }

	preRun := newRootPreRunE(props, nil, mcpLogLevel, state, map[string]*pflag.Flag{})

	cmd := preRunCmd(t)
	err := preRun(cmd, nil)

	require.ErrorIs(t, err, ErrUpdateComplete, "accepted update must return ErrUpdateComplete")
}

// TestNewRootPreRunE_UpToDate runs the full closure to completion against an
// up-to-date release source: config is loaded, telemetry collector wired, and
// the update check returns no exit.
func TestNewRootPreRunE_UpToDate(t *testing.T) {
	setup.ResetRegistryForTesting()
	t.Cleanup(setup.ResetRegistryForTesting)
	// Not parallel: the update check stamps a HOME-derived marker file.
	t.Setenv("HOME", t.TempDir())

	provider := releasetest.New(releasetest.WithRelease("v1.0.0"))
	props := preRunProps(t, "v1.0.0", provider)
	props.FS = afero.NewOsFs()
	props.Config = config.NewReaderContainer(props.FS)

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
	setup.ResetRegistryForTesting()
	t.Cleanup(setup.ResetRegistryForTesting)
	t.Parallel()

	fs := afero.NewMemMapFs()
	props := &p.Props{
		Logger:       logger.NewBuffer(),
		FS:           fs,
		Assets:       nil, // nil skips embedded-config load (no assets/init/config.yaml fixture)
		Config:       config.NewReaderContainer(fs),
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

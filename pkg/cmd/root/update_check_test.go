package root

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	forgetest "gitlab.com/phpboyscout/go/forge/test"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/internal/testutil"
	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	p "gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
	ver "gitlab.com/phpboyscout/go-tool-base/pkg/version"
)

// TestCheckForUpdates_UpToDate exercises the happy path where the running
// binary already matches the latest release: no error, no exit, and the latest
// version is reported via the logger.
func TestCheckForUpdates_UpToDate(t *testing.T) {
	t.Parallel()

	provider := forgetest.New(forgetest.WithRelease("v1.0.0"))
	props := newUpdateProps(t, "v1.0.0", provider)
	state := newRootState()
	result := checkForUpdates(context.Background(), mkUpdateCmd(t), props, state)

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
	t.Parallel()

	provider := forgetest.New(forgetest.WithRelease("v2.0.0"))
	props := newUpdateProps(t, "v1.0.0", provider)
	state := newRootState()
	result := checkForUpdates(context.Background(), mkUpdateCmd(t), props, state)

	require.NotNil(t, result)
	// Default policy is "disabled": available update is logged, not blocked.
	require.NoError(t, result.Error)
	assert.False(t, result.ShouldExit)
}

// TestCheckForUpdates_SkippedWhenDevelopment proves the skip path: a
// development version short-circuits before any provider call.
func TestCheckForUpdates_SkippedWhenDevelopment(t *testing.T) {
	t.Parallel()

	provider := forgetest.New(forgetest.WithRelease("v2.0.0"))
	props := newUpdateProps(t, "v0.0.0-dev", provider)
	state := newRootState()
	result := checkForUpdates(context.Background(), mkUpdateCmd(t), props, state)

	require.NotNil(t, result)
	require.NoError(t, result.Error)
	assert.False(t, result.ShouldExit)
}

// TestCheckForUpdates_EnabledPolicyBlocks proves that under the "enabled"
// policy an outdated binary with a declined (non-interactive) prompt becomes a
// hard error rather than a masked continue.
func TestCheckForUpdates_EnabledPolicyBlocks(t *testing.T) {
	// Not parallel: neutralises any ambient CI env so the update check is not
	// skipped (Config.GetBool("ci") reads the CI env via viper AutomaticEnv).
	t.Setenv("CI", "")

	provider := forgetest.New(forgetest.WithRelease("v2.0.0"))
	props := newUpdateProps(t, "v1.0.0", provider)
	props.Tool.UpdatePolicy = p.UpdatePolicyEnabled
	// Decline deterministically: nobody is at the terminal, so the prompt is
	// skipped and the update stays declined.
	props.IO = nonInteractiveIO()
	state := newRootState()

	result := checkForUpdates(context.Background(), mkUpdateCmd(t), props, state)

	require.NotNil(t, result)
	require.Error(t, result.Error, "enabled policy must block on a declined required update")
}

// TestWarnIfBehindCached covers the cached-version reminder path.
func TestWarnIfBehindCached(t *testing.T) {
	// Not parallel: writes the cached-version marker via HOME-derived dir.
	t.Setenv("HOME", t.TempDir())

	log := logger.NewBuffer()
	fs := afero.NewOsFs()
	props := &p.Props{
		Logger:  log,
		FS:      fs,
		Config:  testutil.StoreFromYAML(t, "{}\n"),
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
	// Not parallel: writes the marker via HOME-derived dir.
	t.Setenv("HOME", t.TempDir())

	provider := forgetest.New(forgetest.WithRelease("v2.0.0"))
	fs := afero.NewOsFs()
	props := &p.Props{
		Logger:  logger.NewBuffer(),
		FS:      fs,
		Config:  testutil.StoreFromYAML(t, "{}\n"),
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
	t.Setenv("HOME", t.TempDir())

	provider := forgetest.New(forgetest.WithRelease("v1.0.0"))
	fs := afero.NewOsFs()
	props := &p.Props{
		Logger:  logger.NewBuffer(),
		FS:      fs,
		Config:  testutil.StoreFromYAML(t, "{}\n"),
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
	// Not parallel: update.Update writes the extracted binary + markers to the
	// HOME-derived data dir on a real FS.
	t.Setenv("HOME", t.TempDir())

	const tool = "covupdtool"

	asset := forgetest.TarGzAsset(tool, tool, "#!/bin/sh\necho updated\n")
	provider := forgetest.New(forgetest.WithRelease("v2.0.0", asset))

	fs := afero.NewOsFs()
	props := &p.Props{
		Logger:  logger.NewBuffer(),
		FS:      fs,
		Config:  testutil.StoreFromYAML(t, "{}\n"),
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
	t.Setenv("HOME", t.TempDir())

	const tool = "covupderrtool"

	asset := forgetest.TarGzAsset(tool, tool, "binary")
	provider := forgetest.New(
		forgetest.WithRelease("v2.0.0", asset),
		forgetest.WithDownloadError(assertErr{}),
	)

	fs := afero.NewOsFs()
	props := &p.Props{
		Logger:  logger.NewBuffer(),
		FS:      fs,
		Config:  testutil.StoreFromYAML(t, "{}\n"),
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

// updateTool returns Tool metadata wired for hermetic self-update tests: a
// release source the injected provider satisfies, with the update command
// enabled and other prompt/network features off.
func updateTool(provider *forgetest.Source) p.Tool {
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

// TestCheckForUpdates_FailureStampsTheThrottle: a check that fails still
// records that it ran, so a broken release source is retried at the check
// interval rather than on every invocation. Before the stamp, every command
// re-ran the failing check, and with it re-resolved the credential, a
// keychain read per command for a keychain-backed configuration.
func TestCheckForUpdates_FailureStampsTheThrottle(t *testing.T) {
	// Not parallel: the marker lives under the HOME-derived config dir.
	t.Setenv("HOME", t.TempDir())
	t.Setenv("CI", "")

	provider := forgetest.New() // no release to report: the check fails
	props := newUpdateProps(t, "v1.0.0", provider)
	cmd := mkUpdateCmd(t)

	require.False(t, setup.SkipUpdateCheck(props.FS, props.Tool.Name, cmd, 24*time.Hour), "no marker yet")

	result := checkForUpdates(context.Background(), cmd, props, newRootState())
	require.NotNil(t, result)
	require.NoError(t, result.Error, "a failed passive check never fails the command")

	assert.True(t, setup.SkipUpdateCheck(props.FS, props.Tool.Name, cmd, 24*time.Hour),
		"the failed check is stamped, so the next invocation inside the interval skips it")
}

// TestCheckForUpdates_FailureKeepsTheCachedVersion: the stamp on failure must
// not lose the latest version an earlier check cached, or a transient failure
// would silence the out-of-date reminder.
func TestCheckForUpdates_FailureKeepsTheCachedVersion(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("CI", "")

	provider := forgetest.New()
	props := newUpdateProps(t, "v1.0.0", provider)
	cmd := mkUpdateCmd(t)

	// An old marker carrying a cached version: old enough that the check runs.
	require.NoError(t, setup.SetCheckedVersion(props.FS, props.Tool.Name, "v2.0.0"))
	marker := filepath.Join(setup.GetDefaultConfigDir(props.FS, props.Tool.Name), "last_checked")
	stale := time.Now().Add(-48 * time.Hour)
	require.NoError(t, props.FS.Chtimes(marker, stale, stale))
	require.False(t, setup.SkipUpdateCheck(props.FS, props.Tool.Name, cmd, 24*time.Hour), "the marker is stale, so the check runs")

	checkForUpdates(context.Background(), cmd, props, newRootState())

	assert.Equal(t, "v2.0.0", setup.GetCheckedVersion(props.FS, props.Tool.Name), "the cached version survives a failed check")
	assert.True(t, setup.SkipUpdateCheck(props.FS, props.Tool.Name, cmd, 24*time.Hour), "and the marker is fresh")
}

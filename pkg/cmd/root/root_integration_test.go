package root_test

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"gitlab.com/phpboyscout/go/config"

	"github.com/spf13/afero"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go/errorhandling"

	"gitlab.com/phpboyscout/go-tool-base/internal/testutil"
	"gitlab.com/phpboyscout/go-tool-base/pkg/cmd/root"
	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	p "gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
	ver "gitlab.com/phpboyscout/go-tool-base/pkg/version"
)

// commandNames returns the Use field of each child command in the cobra tree.
func commandNames(cmd *cobra.Command) map[string]bool {
	names := make(map[string]bool)
	for _, c := range cmd.Commands() {
		names[c.Use] = true
	}

	return names
}

func newTestProps(features ...p.FeatureState) *p.Props {
	return &p.Props{
		Tool: p.Tool{
			Name:     "test-tool",
			Features: p.SetFeatures(features...),
		},
		Logger:       logger.NewNoop(),
		FS:           afero.NewMemMapFs(),
		Assets:       p.NewAssets(),
		ErrorHandler: errorhandling.New(logger.ToSlog(logger.NewNoop()), nil),
	}
}

func TestFeatureFlags_DefaultsRegisterExpectedCommands(t *testing.T) {
	testutil.SkipIfNotIntegration(t, "cmd")
	setup.ResetRegistryForTesting()
	t.Cleanup(setup.ResetRegistryForTesting)

	props := newTestProps() // all defaults
	rootCmd := root.NewCmdRoot(props)
	names := commandNames(rootCmd.Command)

	// Default-enabled commands
	assert.True(t, names["update"], "update should be registered by default")
	assert.True(t, names["init"], "init should be registered by default")
	assert.True(t, names["mcp"], "mcp should be registered by default")
	assert.True(t, names["doctor"], "doctor should be registered by default")

	// Always present regardless of feature flags
	assert.True(t, names["version"], "version is always registered")
}

func TestFeatureFlags_DisableRemovesCommand(t *testing.T) {
	testutil.SkipIfNotIntegration(t, "cmd")
	setup.ResetRegistryForTesting()
	t.Cleanup(setup.ResetRegistryForTesting)

	props := newTestProps(
		p.Disable(p.UpdateCmd),
		p.Disable(p.InitCmd),
	)
	rootCmd := root.NewCmdRoot(props)
	names := commandNames(rootCmd.Command)

	assert.False(t, names["update"], "disabled update should not appear")
	assert.False(t, names["init"], "disabled init should not appear")

	// Other defaults still present
	assert.True(t, names["mcp"], "mcp still enabled")
	assert.True(t, names["doctor"], "doctor still enabled")
	assert.True(t, names["version"], "version always present")
}

func TestFeatureFlags_DisableAllFeatureCommands(t *testing.T) {
	testutil.SkipIfNotIntegration(t, "cmd")
	setup.ResetRegistryForTesting()
	t.Cleanup(setup.ResetRegistryForTesting)

	props := newTestProps(
		p.Disable(p.UpdateCmd),
		p.Disable(p.InitCmd),
		p.Disable(p.McpCmd),
		p.Disable(p.DocsCmd),
		p.Disable(p.DoctorCmd),
		p.Disable(p.ChangelogCmd),
	)
	rootCmd := root.NewCmdRoot(props)
	names := commandNames(rootCmd.Command)

	// Only version should remain (always registered, not feature-gated)
	assert.True(t, names["version"], "version is always present")
	assert.Len(t, names, 1, "only version should remain when all features disabled")
}

func TestFeatureFlags_CustomSubcommandsUnaffected(t *testing.T) {
	testutil.SkipIfNotIntegration(t, "cmd")
	setup.ResetRegistryForTesting()
	t.Cleanup(setup.ResetRegistryForTesting)

	customCmd := &cobra.Command{Use: "custom", Run: func(_ *cobra.Command, _ []string) {}}
	props := newTestProps(p.Disable(p.UpdateCmd))
	rootCmd := root.NewCmdRoot(props, setup.Wrap("", customCmd))
	names := commandNames(rootCmd.Command)

	assert.True(t, names["custom"], "custom subcommand always registered regardless of feature flags")
	assert.False(t, names["update"], "disabled update should not appear")
}

func TestFeatureFlags_SelectiveToggling(t *testing.T) {
	testutil.SkipIfNotIntegration(t, "cmd")
	setup.ResetRegistryForTesting()
	t.Cleanup(setup.ResetRegistryForTesting)

	// Disable all defaults, re-enable only doctor
	props := newTestProps(
		p.Disable(p.UpdateCmd),
		p.Disable(p.InitCmd),
		p.Disable(p.McpCmd),
		p.Disable(p.DocsCmd),
		p.Enable(p.DoctorCmd),
	)
	rootCmd := root.NewCmdRoot(props)
	names := commandNames(rootCmd.Command)

	assert.True(t, names["doctor"], "explicitly enabled doctor should appear")
	assert.True(t, names["version"], "version always present")
	assert.False(t, names["update"], "disabled")
	assert.False(t, names["init"], "disabled")
	assert.False(t, names["mcp"], "disabled")
}

func TestToolMetadata_PropagatedToRootCommand(t *testing.T) {
	testutil.SkipIfNotIntegration(t, "cmd")
	setup.ResetRegistryForTesting()
	t.Cleanup(setup.ResetRegistryForTesting)

	props := &p.Props{
		Tool: p.Tool{
			Name:        "mytool",
			Summary:     "A test tool",
			Description: "A longer description of the test tool",
			Features:    p.SetFeatures(),
		},
		Logger:       logger.NewNoop(),
		FS:           afero.NewMemMapFs(),
		Assets:       p.NewAssets(),
		ErrorHandler: errorhandling.New(logger.ToSlog(logger.NewNoop()), nil),
	}

	rootCmd := root.NewCmdRoot(props)

	assert.Equal(t, "mytool", rootCmd.Use)
	assert.Equal(t, "A test tool", rootCmd.Short)
	assert.Equal(t, "A longer description of the test tool", rootCmd.Long)
}

// TestConfigWatch_FileChangeReachesObserver is the migration spec's D6
// acceptance: watching became explicit in config v0.3.x, and this is the only
// change in the migration with no compiler error — done wrong, configuration
// silently stops reloading. A real file on disk is changed after the bootstrap
// and an observer registered on the live store must fire.
func TestConfigWatch_FileChangeReachesObserver(t *testing.T) {
	testutil.SkipIfNotIntegration(t, "cmd")
	setup.ResetRegistryForTesting()
	t.Cleanup(setup.ResetRegistryForTesting)

	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	require.NoError(t, os.WriteFile(cfgPath, []byte("log:\n  level: info\n"), 0o600))

	props := newTestProps(
		p.Disable(p.UpdateCmd),
		p.Disable(p.InitCmd),
		p.Disable(p.McpCmd),
		p.Disable(p.DocsCmd),
		p.Disable(p.DoctorCmd),
		p.Disable(p.TelemetryCmd),
	)
	props.FS = afero.NewOsFs() // the watcher needs paths the OS can watch
	props.Version = ver.NewInfo("v1.0.0", "", "")

	rootCmd := root.NewCmdRoot(props)
	rootCmd.SetArgs([]string{"--config", cfgPath, "version"})
	rootCmd.SetOut(io.Discard)
	rootCmd.SetErr(io.Discard)

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	require.NoError(t, rootCmd.ExecuteContext(ctx))
	require.NotNil(t, props.Config, "the bootstrap must have built the store")

	fired := make(chan struct{}, 1)
	props.Config.AddObserverFunc(func(config.Observed) error {
		select {
		case fired <- struct{}{}:
		default:
		}

		return nil
	})

	require.NoError(t, os.WriteFile(cfgPath, []byte("log:\n  level: debug\n"), 0o600))

	select {
	case <-fired:
	case <-time.After(10 * time.Second):
		t.Fatal("a config file changed on disk never reached an observer — watching is not wired")
	}

	assert.Equal(t, "debug", props.Config.View().GetString("log.level"),
		"the reloaded snapshot must carry the changed value")
}

package root_test

import (
	"testing"

	"github.com/spf13/afero"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"

	"github.com/phpboyscout/go-tool-base/internal/testutil"
	"github.com/phpboyscout/go-tool-base/pkg/cmd/root"
	"github.com/phpboyscout/go-tool-base/pkg/errorhandling"
	"github.com/phpboyscout/go-tool-base/pkg/logger"
	p "github.com/phpboyscout/go-tool-base/pkg/props"
	"github.com/phpboyscout/go-tool-base/pkg/setup"
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
		ErrorHandler: errorhandling.New(logger.NewNoop(), nil),
	}
}

func TestFeatureFlags_DefaultsRegisterExpectedCommands(t *testing.T) {
	t.Parallel()
	testutil.SkipIfNotIntegration(t, "cmd")
	setup.ResetRegistryForTesting()
	t.Cleanup(setup.ResetRegistryForTesting)

	props := newTestProps() // all defaults
	rootCmd := root.NewCmdRoot(props)
	names := commandNames(rootCmd)

	// Default-enabled commands
	assert.True(t, names["update"], "update should be registered by default")
	assert.True(t, names["init"], "init should be registered by default")
	assert.True(t, names["mcp"], "mcp should be registered by default")
	assert.True(t, names["doctor"], "doctor should be registered by default")

	// Always present regardless of feature flags
	assert.True(t, names["version"], "version is always registered")
}

func TestFeatureFlags_DisableRemovesCommand(t *testing.T) {
	t.Parallel()
	testutil.SkipIfNotIntegration(t, "cmd")
	setup.ResetRegistryForTesting()
	t.Cleanup(setup.ResetRegistryForTesting)

	props := newTestProps(
		p.Disable(p.UpdateCmd),
		p.Disable(p.InitCmd),
	)
	rootCmd := root.NewCmdRoot(props)
	names := commandNames(rootCmd)

	assert.False(t, names["update"], "disabled update should not appear")
	assert.False(t, names["init"], "disabled init should not appear")

	// Other defaults still present
	assert.True(t, names["mcp"], "mcp still enabled")
	assert.True(t, names["doctor"], "doctor still enabled")
	assert.True(t, names["version"], "version always present")
}

func TestFeatureFlags_DisableAllFeatureCommands(t *testing.T) {
	t.Parallel()
	testutil.SkipIfNotIntegration(t, "cmd")
	setup.ResetRegistryForTesting()
	t.Cleanup(setup.ResetRegistryForTesting)

	props := newTestProps(
		p.Disable(p.UpdateCmd),
		p.Disable(p.InitCmd),
		p.Disable(p.McpCmd),
		p.Disable(p.DocsCmd),
		p.Disable(p.DoctorCmd),
	)
	rootCmd := root.NewCmdRoot(props)
	names := commandNames(rootCmd)

	// Only version should remain (always registered, not feature-gated)
	assert.True(t, names["version"], "version is always present")
	assert.Len(t, names, 1, "only version should remain when all features disabled")
}

func TestFeatureFlags_CustomSubcommandsUnaffected(t *testing.T) {
	t.Parallel()
	testutil.SkipIfNotIntegration(t, "cmd")
	setup.ResetRegistryForTesting()
	t.Cleanup(setup.ResetRegistryForTesting)

	customCmd := &cobra.Command{Use: "custom", Run: func(_ *cobra.Command, _ []string) {}}
	props := newTestProps(p.Disable(p.UpdateCmd))
	rootCmd := root.NewCmdRoot(props, customCmd)
	names := commandNames(rootCmd)

	assert.True(t, names["custom"], "custom subcommand always registered regardless of feature flags")
	assert.False(t, names["update"], "disabled update should not appear")
}

func TestFeatureFlags_SelectiveToggling(t *testing.T) {
	t.Parallel()
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
	names := commandNames(rootCmd)

	assert.True(t, names["doctor"], "explicitly enabled doctor should appear")
	assert.True(t, names["version"], "version always present")
	assert.False(t, names["update"], "disabled")
	assert.False(t, names["init"], "disabled")
	assert.False(t, names["mcp"], "disabled")
}

func TestToolMetadata_PropagatedToRootCommand(t *testing.T) {
	t.Parallel()
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
		ErrorHandler: errorhandling.New(logger.NewNoop(), nil),
	}

	rootCmd := root.NewCmdRoot(props)

	assert.Equal(t, "mytool", rootCmd.Use)
	assert.Equal(t, "A test tool", rootCmd.Short)
	assert.Equal(t, "A longer description of the test tool", rootCmd.Long)
}

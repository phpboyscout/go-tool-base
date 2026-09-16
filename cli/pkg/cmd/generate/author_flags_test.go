package generate

import (
	"testing"

	"charm.land/huh/v2"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/cli/pkg/generator"
	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

// TestProjectCommand_AuthorSettingFlagsExist pins spec 0197 D4: every
// manifest-only setting has a flag, and the classification table's flag
// column names a real flag on the command.
func TestProjectCommand_AuthorSettingFlagsExist(t *testing.T) {
	t.Parallel()

	cmd := NewCmdSkeleton(&props.Props{FS: afero.NewMemMapFs(), Logger: logger.NewNoop()}, &SharedFlags{})

	for _, s := range generator.AuthorSettings() {
		if s.Flag == "" {
			continue
		}

		assert.NotNilf(t, cmd.Flags().Lookup(s.Flag), "the table names --%s for %s but the command has no such flag", s.Flag, s.Field)
	}
}

// TestSkeletonOptions_AuthorSettingsReachTheConfig: the new flags' values
// arrive in SkeletonConfig, validated where the manifest is.
func TestSkeletonOptions_AuthorSettingsReachTheConfig(t *testing.T) {
	t.Parallel()

	o := &SkeletonOptions{Name: "tool", Repo: "org/tool", ForgeBackend: "github",
		Features: append([]string{"telemetry"}, generator.DefaultSelectedFeatures...)}
	o.TelemetryEndpoint = "https://t.internal"
	o.TelemetryOTelEndpoint = "https://o.internal"
	o.Bootstrap.AutoInitialise = true
	o.Bootstrap.SkipConfigCheck = []string{"version"}
	o.Bootstrap.AuxiliaryCommands = []string{"completion"}
	o.ConfigLayers = []string{"flags", "env", "files", "defaults"}
	o.Signing = true
	o.SigningRequireSignature = true
	o.SigningRequireChecksum = true

	require.NoError(t, o.validateFields())

	cfg := o.skeletonConfig(nil)
	assert.Equal(t, "https://t.internal", cfg.TelemetryEndpoint)
	assert.Equal(t, "https://o.internal", cfg.TelemetryOTelEndpoint)
	assert.True(t, cfg.Bootstrap.AutoInitialise)
	assert.Equal(t, []string{"version"}, cfg.Bootstrap.SkipConfigCheck)
	assert.Equal(t, []string{"completion"}, cfg.Bootstrap.AuxiliaryCommands)
	assert.Equal(t, []string{"flags", "env", "files", "defaults"}, cfg.ConfigLayers)
	assert.True(t, cfg.Signing.RequireSignature)
	assert.True(t, cfg.Signing.RequireChecksum)

	o.ConfigLayers = []string{"flags", "bogus"}
	require.Error(t, o.validateFields(), "an unknown config layer is refused at the flag")

	o.ConfigLayers = nil
	o.TelemetryEndpoint = "not a url"
	require.Error(t, o.validateFields(), "a malformed endpoint is refused at the flag")
}

// TestWizard_TelemetryPage pins spec 0197 D5: the Telemetry page is asked
// only with the feature selected, and its answers clear when it is hidden.
func TestWizard_TelemetryPage(t *testing.T) {
	t.Parallel()

	t.Run("asked with the feature", func(t *testing.T) {
		t.Parallel()

		o := &SkeletonOptions{Features: []string{"update", "telemetry"}}
		f, m := startWizard(o)
		m = driveUntilKey(f, m, o, "telemetry-endpoint")
		require.Equal(t, "telemetry-endpoint", f.GetFocusedField().GetKey())

		m = typeAnswer(m, "https://t.internal")
		m, ok := advance(f, m)
		require.True(t, ok)
		require.Equal(t, "telemetry-otel-endpoint", f.GetFocusedField().GetKey())

		_, ok = advance(f, m)
		require.True(t, ok, "the OTel endpoint is optional")
		assert.Equal(t, "https://t.internal", o.TelemetryEndpoint)
	})

	t.Run("hidden without the feature and cleared", func(t *testing.T) {
		t.Parallel()

		o := &SkeletonOptions{Features: []string{"update"}, TelemetryEndpoint: "https://t.internal"}
		f, m := startWizard(o)
		m = driveUntilKey(f, m, o, "telemetry-endpoint")
		assert.NotEqual(t, "telemetry-endpoint", f.GetFocusedField().GetKey())

		for i := 0; i < 20 && f.State != huh.StateCompleted; i++ {
			m, _ = advance(f, m)
		}

		require.Equal(t, huh.StateCompleted, f.State)
		require.NoError(t, o.afterWizard())
		assert.Empty(t, o.TelemetryEndpoint)
	})
}

// TestWizard_SigningPageAsksChecksumOnFirstRun pins OQ4: the first run asks
// require_checksum and not require_signature; a revisit asks both.
func TestWizard_SigningPageAsksChecksumOnFirstRun(t *testing.T) {
	t.Parallel()

	first := &SkeletonOptions{Features: generator.DefaultSelectedFeatures, Signing: true}
	f, m := startWizard(first)
	m = driveUntilKey(f, m, first, "signing-require-checksum")
	require.Equal(t, "signing-require-checksum", f.GetFocusedField().GetKey())

	m, _ = m.Update(keypress('y'))
	_, ok := advance(f, m)
	require.True(t, ok)
	assert.True(t, first.SigningRequireChecksum)
	assert.NotEqual(t, "signing-require-signature", f.GetFocusedField().GetKey(), "not before the first signed release")

	revisit := &SkeletonOptions{Features: generator.DefaultSelectedFeatures, Signing: true, revisit: true}
	f, m = startWizard(revisit)
	driveUntilKey(f, m, revisit, "signing-require-signature")
	assert.Equal(t, "signing-require-signature", f.GetFocusedField().GetKey(), "a revisit asks both")
}

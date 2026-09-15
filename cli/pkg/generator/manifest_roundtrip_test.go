package generator

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/cli/pkg/generator/templates"
)

// TestSkeletonConfig_RoundTripsThroughTheManifest sets every author setting
// the generator accepts, writes it into the manifest the way generation does,
// and reads it back the way regenerate does. A field that generation renders
// but never persists is deleted by the first regenerate (#44 found the two
// telemetry endpoints that way), so every persisted field is asserted here
// and a new SkeletonConfig field has to be added to this table.
func TestSkeletonConfig_RoundTripsThroughTheManifest(t *testing.T) {
	t.Parallel()

	config := SkeletonConfig{
		Name:                  "tool",
		Repo:                  "org/tool",
		Host:                  "gitlab.example.com",
		Description:           "A tool",
		Features:              []ManifestFeature{{Name: "ai", Enabled: true}},
		Private:               true,
		HelpType:              "slack",
		SlackChannel:          "#help",
		SlackTeam:             "T123",
		TeamsChannel:          "help",
		TeamsTeam:             "team",
		TelemetryEndpoint:     "https://telemetry.example.com/v1",
		TelemetryOTelEndpoint: "https://otel.example.com:4318",
		EnvPrefix:             "TOOL",
		ConfigLayers:          []string{"flags", "env"},
		Signing:               ManifestSigning{Enabled: true, KeySource: "embedded"},
		Chat:                  ManifestChat{Providers: []string{"claude"}},
		Bootstrap:             ManifestBootstrap{AutoInitialise: true},
		UpdatePolicy:          "prompt",
		UpdateCheckInterval:   "12h",
		CIComponentSource:     "gitlab.com/acme/cicd",
		Templates:             []TemplateSource{{Name: "acme", Location: "acme/templates", Ref: "v1"}},
	}

	m := manifestFromSkeletonConfig(config, map[string]string{"go.mod": "abc"}, "v9.9.9")

	// Persisted as written.
	assert.Equal(t, config.Name, m.Properties.Name)
	assert.Equal(t, MultilineString(config.Description), m.Properties.Description)
	assert.Equal(t, config.EnvPrefix, m.Properties.EnvPrefix)
	assert.Equal(t, config.ConfigLayers, m.Properties.ConfigLayers)
	assert.Equal(t, config.UpdatePolicy, m.Properties.UpdatePolicy)
	assert.Equal(t, config.UpdateCheckInterval, m.Properties.UpdateCheckInterval)
	assert.Equal(t, config.HelpType, m.Properties.Help.Type)
	assert.Equal(t, config.SlackChannel, m.Properties.Help.SlackChannel)
	assert.Equal(t, config.SlackTeam, m.Properties.Help.SlackTeam)
	assert.Equal(t, config.TeamsChannel, m.Properties.Help.TeamsChannel)
	assert.Equal(t, config.TeamsTeam, m.Properties.Help.TeamsTeam)
	assert.Equal(t, config.TelemetryEndpoint, m.Properties.Telemetry.Endpoint, "telemetry.endpoint must persist (#44)")
	assert.Equal(t, config.TelemetryOTelEndpoint, m.Properties.Telemetry.OTelEndpoint, "telemetry.otel_endpoint must persist (#44)")
	assert.Equal(t, config.Signing, m.Properties.Signing)
	assert.Equal(t, config.Chat, m.Properties.Chat)
	assert.Equal(t, config.Bootstrap, m.Properties.Bootstrap)
	assert.Equal(t, config.CIComponentSource, m.Properties.CI.ComponentSource)
	assert.Equal(t, config.Templates, m.Properties.Templates)
	assert.Equal(t, config.Host, m.ReleaseSource.Host)
	assert.Equal(t, "org", m.ReleaseSource.Owner)
	assert.Equal(t, "tool", m.ReleaseSource.Repo)
	assert.True(t, m.ReleaseSource.Private)
	assert.Equal(t, "v9.9.9", m.Version.GoToolBase)

	// Read back into the root template the way regenerate does.
	data := buildSkeletonRootData(m, nil)
	require.IsType(t, templates.SkeletonRootData{}, data)
	assert.Equal(t, config.TelemetryEndpoint, data.TelemetryEndpoint)
	assert.Equal(t, config.TelemetryOTelEndpoint, data.TelemetryOTelEndpoint)
	assert.Equal(t, config.EnvPrefix, data.EnvPrefix)
	assert.Equal(t, config.UpdatePolicy, data.UpdatePolicy)
	assert.Equal(t, config.UpdateCheckInterval, data.UpdateCheckInterval)
	assert.Equal(t, config.HelpType, data.HelpType)
	assert.Equal(t, config.Host, data.Host)
	assert.True(t, data.Private)
}

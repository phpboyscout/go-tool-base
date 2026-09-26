package generate

import (
	"fmt"
	"testing"

	"charm.land/huh/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/cli/pkg/generator"
)

// TestOptionsFromManifest_RoundTrip pins spec 0197 D13: loading a manifest
// into the wizard's options and writing it back through skeletonConfig
// changes nothing, for every classified setting. Compared at the config
// level, where the generator's own table test already covers each field.
func TestOptionsFromManifest_RoundTrip(t *testing.T) {
	t.Parallel()

	o := &SkeletonOptions{
		Name: "tool", Description: "d", Repo: "org/tool", Host: "code.example.com", ForgeBackend: "gitlab",
		Private: true, Features: []string{"update", "init", "ai", "telemetry", "keychain"},
		ForgeCredentials: []string{"github"}, ReleaseChannel: generator.ReleaseChannelForge,
		HelpType: "slack", SlackChannel: "#h", SlackTeam: "T", EnvPrefix: "TOOL",
		UpdatePolicy: "prompt", UpdateCheckInterval: "168h", TelemetryEndpoint: "https://t.internal",
		ConfigLayers: []string{"defaults", "env", "files", "flags"}, GoVersion: "1.26",
		ConfigFormats: []string{"toml", "ini"}, ConfigFormat: "toml",
		ChatProviders: []string{"claude", "openai"}, CIComponentSource: "gitlab.example.com/mirror/cicd",
		Signing: true, SigningEmail: "rel@example.com", SigningKeySource: "external", SigningKeyID: "alias/k",
		SigningRequireChecksum: true, MCPMode: "direct",
	}
	o.ChatDefault.Provider = "openai"
	o.Bootstrap.AutoInitialise = true
	o.Bootstrap.AuxiliaryCommands = []string{"completion"}

	require.NoError(t, o.validateFields())

	m := generator.ManifestFromSkeletonConfig(o.skeletonConfig(nil), nil, "v1.0.0")
	back := optionsFromManifest(m)

	assert.True(t, back.revisit, "a manifest-loaded wizard is a revisit")
	require.NoError(t, back.validateFields(), "what came out of the manifest validates as it went in")

	// Compared as manifests: the feature list is delta-normalised there,
	// where the options carry the selected names.
	again := generator.ManifestFromSkeletonConfig(back.skeletonConfig(nil), nil, "v1.0.0")
	assert.Equal(t, m, again)
}

// TestWizard_RevisitShowsTheNameAndAsksNoPath: on a revisit the name is
// shown, not asked (renaming is not the wizard's), and the destination path
// is not asked at all.
func TestWizard_RevisitShowsTheNameAndAsksNoPath(t *testing.T) {
	t.Parallel()

	o := &SkeletonOptions{Name: "tool", Repo: "org/tool", ForgeBackend: "github", hosted: true,
		Features: generator.DefaultSelectedFeatures, revisit: true}
	f := o.wizardForm()
	f.Update(f.Init())

	view := fmt.Sprint(f.View())
	assert.Contains(t, view, "Project: tool", "the name is shown")
	assert.NotContains(t, view, "Project Name", "and not asked")
	assert.NotContains(t, view, "Destination Path")

	var m huh.Model = f
	for i := 0; i < 30 && f.State != huh.StateCompleted; i++ {
		m, _ = advance(f, m)
	}

	require.Equal(t, huh.StateCompleted, f.State, "accepting every pre-filled answer completes")
	require.NoError(t, o.afterWizard())
	assert.Equal(t, "tool", o.Name, "the name is untouched")
	assert.Equal(t, "org/tool", o.Repo)
}

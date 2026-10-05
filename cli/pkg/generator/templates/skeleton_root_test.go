package templates

import (
	"path"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup/forge"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup/keychain"
)

func rootSource(t *testing.T, data SkeletonRootData) string {
	t.Helper()

	if data.Name == "" {
		data.Name = "widget"
	}

	return renderGoFile(t, SkeletonRoot(data))
}

func TestSkeletonRoot_Minimal(t *testing.T) {
	t.Parallel()

	src := rootSource(t, SkeletonRootData{
		Description:     "does widgets",
		ReleaseProvider: "gitlab",
		Host:            "gitlab.com",
		Org:             "acme",
		RepoName:        "widget",
	})

	assert.Contains(t, src, "func NewCmdRoot(v version.Info) (*setup.Command, *props.Props, error) {")
	assert.Contains(t, src, `Description: "does widgets"`)
	assert.Contains(t, src, `Type:  "gitlab"`)
	assert.Contains(t, src, `Owner: "acme"`)
	assert.Contains(t, src, "rootCmd := gtbRoot.NewCmdRoot(p)")

	for _, absent := range []string{"Private:", "EnvPrefix", "Config:", "UpdatePolicy", "UpdateCheckInterval", "MCP:", "Help:", "Features:", "Telemetry:", "Signing:", "Bootstrap:", "otelAuth"} {
		assert.NotContainsf(t, src, absent, "an unstated %s emits nothing", absent)
	}
}

func TestSkeletonRoot_ReleaseSource(t *testing.T) {
	t.Parallel()

	static := rootSource(t, SkeletonRootData{ReleaseProvider: "static", ReleaseBaseURL: "https://pkg.example.org/widget"})
	assert.Contains(t, static, "Type:    props.ReleaseSourceStatic")
	assert.Contains(t, static, `BaseURL: "https://pkg.example.org/widget"`)
	assert.NotContains(t, static, "Owner:")

	private := rootSource(t, SkeletonRootData{ReleaseProvider: "github", Private: true})
	assert.Contains(t, private, "Private: true")
}

func TestSkeletonRoot_ToolOptions(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		data SkeletonRootData
		want []string
	}{
		{
			name: "env prefix",
			data: SkeletonRootData{EnvPrefix: "WIDGET"},
			want: []string{`EnvPrefix:   "WIDGET"`},
		},
		{
			name: "update policy prompt",
			data: SkeletonRootData{UpdatePolicy: "prompt"},
			want: []string{"UpdatePolicy: props.UpdatePolicyPrompt"},
		},
		{
			name: "update policy enabled",
			data: SkeletonRootData{UpdatePolicy: "enabled"},
			want: []string{"UpdatePolicy: props.UpdatePolicyEnabled"},
		},
		{
			name: "interval in hours",
			data: SkeletonRootData{UpdateCheckInterval: "168h"},
			want: []string{"UpdateCheckInterval: 168 * time.Hour"},
		},
		{
			name: "interval in minutes",
			data: SkeletonRootData{UpdateCheckInterval: "30m"},
			want: []string{"UpdateCheckInterval: 30 * time.Minute"},
		},
		{
			name: "interval in seconds",
			data: SkeletonRootData{UpdateCheckInterval: "45s"},
			want: []string{"UpdateCheckInterval: 45 * time.Second"},
		},
		{
			name: "sub-second interval",
			data: SkeletonRootData{UpdateCheckInterval: "1500ms"},
			want: []string{"UpdateCheckInterval: time.Duration(int64(1500000000))"},
		},
		{
			name: "direct MCP",
			data: SkeletonRootData{MCPMode: "direct"},
			want: []string{"props.MCPConfig{Mode: props.MCPDirect}"},
		},
		{
			name: "slack help",
			data: SkeletonRootData{HelpType: "slack", SlackChannel: "#widget", SlackTeam: "acme"},
			want: []string{`Help: props.SlackHelp{`, `Channel: "#widget"`, `Team:    "acme"`},
		},
		{
			name: "teams help",
			data: SkeletonRootData{HelpType: "teams", TeamsChannel: "Widget", TeamsTeam: "Acme"},
			want: []string{`Help: props.TeamsHelp{`, `Channel: "Widget"`, `Team:    "Acme"`},
		},
		{
			name: "plain telemetry endpoint",
			data: SkeletonRootData{TelemetryEndpoint: "https://t.example.org"},
			want: []string{`Telemetry: props.TelemetryConfig{Endpoint: "https://t.example.org"}`, "var otelAuth string"},
		},
		{
			name: "signing with checksum",
			data: SkeletonRootData{SigningEnabled: true, ModulePath: "example.com/acme/widget", RequireChecksum: true},
			want: []string{
				`trustkeys "example.com/acme/widget/internal/trustkeys"`,
				"EmbeddedKeys:    trustkeys.Keys()",
				"RequireChecksum: props.BoolPtr(true)",
			},
		},
		{
			name: "bootstrap policy",
			data: SkeletonRootData{AutoInitialise: true, SkipConfigCheck: []string{"version"}, AuxiliaryCommands: []string{"docs", "help"}},
			want: []string{
				"Bootstrap: props.BootstrapPolicy{",
				"AutoInitialise:    true",
				`SkipConfigCheck:   []string{"version"}`,
				`AuxiliaryCommands: []string{"docs", "help"}`,
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			src := rootSource(t, tc.data)
			for _, w := range tc.want {
				assert.Contains(t, src, w)
			}
		})
	}
}

func TestSkeletonRoot_IgnoredToolOptions(t *testing.T) {
	t.Parallel()

	for _, data := range []SkeletonRootData{
		{UpdatePolicy: "disabled"},
		{UpdateCheckInterval: "soon"},
		{UpdateCheckInterval: "-1h"},
		{MCPMode: "compact"},
		{HelpType: "email"},
		{SigningEnabled: true},
	} {
		src := rootSource(t, data)

		for _, absent := range []string{"UpdatePolicy", "UpdateCheckInterval", "MCP:", "Help:", "Signing:"} {
			assert.NotContainsf(t, src, absent, "%+v must leave %s to the framework default", data, absent)
		}
	}
}

func TestSkeletonRoot_ConfigSpec(t *testing.T) {
	t.Parallel()

	src := rootSource(t, SkeletonRootData{
		ConfigLayers: []string{"defaults", "files", "vault", "env", "flags"},
		ConfigFormat: "toml",
		ConfigSources: []ConfigSourceData{
			{Name: "vault", Kind: "vault", Required: new(true), Writable: new(false)},
			{Name: "plain", Kind: "file"},
		},
	})

	assert.Contains(t, src, "Config: props.ConfigSpec{")
	assert.Contains(t, src, `Format: "toml"`)
	assert.Contains(t, src,
		`[]props.ConfigLayer{props.LayerDefaults, props.LayerFiles, props.ConfigLayer("vault"), props.LayerEnv, props.LayerFlags}`,
		"built-in layers are constants; a source slot is its name")
	assert.Contains(t, src, `Kind:     "vault"`)
	assert.Contains(t, src, "Required: props.BoolPtr(true)")
	assert.Contains(t, src, "Writable: props.BoolPtr(false)")
	assert.Equal(t, 2, strings.Count(src, "props.BoolPtr("), "only the vault slot states required/writable")
	assert.Contains(t, src, `Name: "plain",`)
}

func TestSkeletonRoot_FormatOnlyConfigSpec(t *testing.T) {
	t.Parallel()

	src := rootSource(t, SkeletonRootData{ConfigFormat: "json"})

	assert.Contains(t, src, `props.ConfigSpec{Format: "json"}`)
}

func TestConfigLayerConstName(t *testing.T) {
	t.Parallel()

	for _, l := range []props.ConfigLayer{props.LayerDefaults, props.LayerFiles, props.LayerProject, props.LayerEnv, props.LayerFlags} {
		assert.Equal(t, "Layer"+string(l[0]-'a'+'A')+string(l[1:]), ConfigLayerConstName(string(l)))
	}
}

func TestSkeletonRoot_Features(t *testing.T) {
	t.Parallel()

	var builtin props.FeatureDescriptor

	for _, d := range Catalogue() {
		if d.Kind == props.KindBuiltin {
			builtin = d

			break
		}
	}

	require.NotEmpty(t, builtin.ID, "the catalogue has a built-in")

	forgeEntry, ok := CatalogueEntry(string(forge.GithubFeature))
	require.True(t, ok)

	src := rootSource(t, SkeletonRootData{
		DisabledFeatures: []string{string(builtin.ID), "no-such-feature"},
		EnabledFeatures:  []string{string(forge.GithubFeature), string(keychain.KeychainFeature)},
	})

	assert.Contains(t, src, "props.SetFeatures(")
	assert.Contains(t, src, "props.Disable(props."+builtin.ConstName+")")
	assert.Contains(t, src, "props.Enable("+path.Base(forgeEntry.ConstPackage)+"."+forgeEntry.ConstName+")",
		"a forge constant is qualified by its own package, not props")
	assert.NotContains(t, src, "no-such-feature")
	assert.NotContains(t, src, string(keychain.KeychainFeature), "a link kind is toggled by its file, not SetFeatures")
}

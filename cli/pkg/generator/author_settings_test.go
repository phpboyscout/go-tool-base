package generator

import (
	"reflect"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

// probes give every author setting a non-default value the round trip can
// recognise. A setting the table names but no probe sets fails the guard, so
// a new field is classified and exercised in the same change (spec 0197 D1).
var probes = map[string]func(c *SkeletonConfig){
	"Name":        func(c *SkeletonConfig) { c.Name = "probe-tool" },
	"Repo":        func(c *SkeletonConfig) { c.Repo = "probe-org/probe-tool" },
	"Host":        func(c *SkeletonConfig) { c.Host = "code.example.com" },
	"Description": func(c *SkeletonConfig) { c.Description = "a probe" },
	"GoVersion":   func(c *SkeletonConfig) { c.GoVersion = "1.26" },
	"Features": func(c *SkeletonConfig) {
		c.Features = []ManifestFeature{{Name: "ai", Enabled: true}, {Name: "gitlab", Enabled: true}}
	},
	"Private":                           func(c *SkeletonConfig) { c.Private = true },
	"HelpType":                          func(c *SkeletonConfig) { c.HelpType = "slack" },
	"SlackChannel":                      func(c *SkeletonConfig) { c.SlackChannel = "#probe" },
	"SlackTeam":                         func(c *SkeletonConfig) { c.SlackTeam = "Probe" },
	"TeamsChannel":                      func(c *SkeletonConfig) { c.TeamsChannel = "probe" },
	"TeamsTeam":                         func(c *SkeletonConfig) { c.TeamsTeam = "Probe" },
	"TelemetryEndpoint":                 func(c *SkeletonConfig) { c.TelemetryEndpoint = "https://t.internal" },
	"TelemetryOTelEndpoint":             func(c *SkeletonConfig) { c.TelemetryOTelEndpoint = "https://o.internal" },
	"EnvPrefix":                         func(c *SkeletonConfig) { c.EnvPrefix = "PROBE" },
	"ConfigLayers":                      func(c *SkeletonConfig) { c.ConfigLayers = []string{"env", "flags"} },
	"Signing.Enabled":                   func(c *SkeletonConfig) { c.Signing.Enabled = true },
	"Signing.ExternalKeyEmail":          func(c *SkeletonConfig) { c.Signing.ExternalKeyEmail = "rel@example.com" },
	"Signing.RequireSignature":          func(c *SkeletonConfig) { c.Signing.RequireSignature = true },
	"Signing.RequireChecksum":           func(c *SkeletonConfig) { c.Signing.RequireChecksum = true },
	"Signing.KeySource":                 func(c *SkeletonConfig) { c.Signing.KeySource = "external" },
	"Signing.RequireExternalCrosscheck": func(c *SkeletonConfig) { c.Signing.RequireExternalCrosscheck = true },
	"Signing.Backend":                   func(c *SkeletonConfig) { c.Signing.Backend = "aws-kms" },
	"Signing.KeyID":                     func(c *SkeletonConfig) { c.Signing.KeyID = "alias/probe" },
	"Signing.KMSRegion":                 func(c *SkeletonConfig) { c.Signing.KMSRegion = "eu-west-2" },
	"Signing.PublicKey":                 func(c *SkeletonConfig) { c.Signing.PublicKey = "keys/probe.asc" },
	"Chat.Providers":                    func(c *SkeletonConfig) { c.Chat.Providers = []string{"claude", "codex-local"} },
	"Chat.Default.Provider":             func(c *SkeletonConfig) { c.Chat.Default.Provider = "codex-local" },
	"Chat.Default.Model":                func(c *SkeletonConfig) { c.Chat.Default.Model = "m" },
	"Chat.Default.BaseURL":              func(c *SkeletonConfig) { c.Chat.Default.BaseURL = "https://llm.internal/v1" },
	"Chat.Default.APIVersion":           func(c *SkeletonConfig) { c.Chat.Default.APIVersion = "2024-10-21" },
	"Chat.Default.Project":              func(c *SkeletonConfig) { c.Chat.Default.Project = "p" },
	"Chat.Default.Location":             func(c *SkeletonConfig) { c.Chat.Default.Location = "eu" },
	"Bootstrap.AutoInitialise":          func(c *SkeletonConfig) { c.Bootstrap.AutoInitialise = true },
	"Bootstrap.SkipConfigCheck":         func(c *SkeletonConfig) { c.Bootstrap.SkipConfigCheck = []string{"version"} },
	"Bootstrap.AuxiliaryCommands":       func(c *SkeletonConfig) { c.Bootstrap.AuxiliaryCommands = []string{"completion"} },
	"UpdatePolicy":                      func(c *SkeletonConfig) { c.UpdatePolicy = "prompt" },
	"MCPMode":                           func(c *SkeletonConfig) { c.MCPMode = "direct" },
	"UpdateCheckInterval":               func(c *SkeletonConfig) { c.UpdateCheckInterval = "168h" },
	"CIComponentSource":                 func(c *SkeletonConfig) { c.CIComponentSource = "gitlab.example.com/mirror/cicd" },
	"Templates": func(c *SkeletonConfig) {
		c.Templates = []TemplateSource{{Name: "t", Type: TemplateSourceGit, Location: "org/tpl", Ref: "v1"}}
	},
	"ForgeBackend":     func(c *SkeletonConfig) { c.ForgeBackend = props.FeatureID("gitlab") },
	"ForgeCredentials": func(c *SkeletonConfig) { c.ForgeCredentials = []props.FeatureID{"github"} },
	"ModulePath":       func(c *SkeletonConfig) { c.ModulePath = "example.com/vanity/probe" },
	"ReleaseChannel":   func(c *SkeletonConfig) { c.ReleaseChannel = ReleaseChannelStatic },
	"ReleaseBaseURL": func(c *SkeletonConfig) {
		c.ReleaseChannel, c.ReleaseBaseURL = ReleaseChannelStatic, "https://pkg.acme.dev/tool"
	},
}

// TestEverySkeletonConfigFieldIsClassified pins spec 0197 D1: the manifest
// owns every author setting, and the table says how each is reached. A field
// added to SkeletonConfig without a row here fails; a row without a probe
// fails; a probe without a row fails.
func TestEverySkeletonConfigFieldIsClassified(t *testing.T) {
	t.Parallel()

	rows := map[string]AuthorSetting{}
	for _, s := range AuthorSettings() {
		rows[s.Field] = s
	}

	for _, field := range leafFields(reflect.TypeOf(SkeletonConfig{}), "") {
		row, ok := rows[field]
		require.Truef(t, ok, "SkeletonConfig.%s is not classified in AuthorSettings", field)
		assertRowShape(t, field, row)
	}

	for field := range probes {
		_, ok := rows[field]
		assert.Truef(t, ok, "probe %q names no classified field", field)
	}

	for _, s := range AuthorSettings() {
		if s.Manifest != "" {
			assert.Truef(t, strings.HasPrefix(s.Manifest, "properties.") || strings.HasPrefix(s.Manifest, "release_source.") || strings.HasPrefix(s.Manifest, "version."),
				"%s: manifest path %q is not rooted", s.Field, s.Manifest)
		}
	}
}

// TestAuthorSettingsRoundTrip pins spec 0197 D2: every setting survives
// generate → manifest → regenerate. The seam is manifestFromSkeletonConfig
// against skeletonConfigFromManifest, both pure.
func TestAuthorSettingsRoundTrip(t *testing.T) {
	t.Parallel()

	for _, s := range AuthorSettings() {
		if s.Kind != KindSetting {
			continue
		}

		t.Run(s.Field, func(t *testing.T) {
			t.Parallel()

			cfg := SkeletonConfig{Name: "tool", Repo: "org/tool", Host: "github.com", ForgeBackend: "github", ReleaseChannel: ReleaseChannelForge}
			probes[s.Field](&cfg)

			// The static channel is its base URL; nothing else changes.
			if s.Field == "ReleaseChannel" {
				cfg.ReleaseBaseURL = "https://pkg.acme.dev/tool"
			}

			m := manifestFromSkeletonConfig(cfg, nil, "v1.0.0")
			back := skeletonConfigFromManifest(m)

			want := fieldByPath(reflect.ValueOf(cfg), s.Field)
			got := fieldByPath(reflect.ValueOf(back), s.Field)
			assert.Equalf(t, want.Interface(), got.Interface(), "%s does not survive the round trip (manifest %s)", s.Field, s.Manifest)
		})
	}
}

func assertRowShape(t *testing.T, field string, row AuthorSetting) {
	t.Helper()

	switch row.Kind {
	case KindRunOption:
		assert.Emptyf(t, row.Manifest, "%s is a run option and has no manifest path", field)
	case KindSetting:
		assert.NotEmptyf(t, row.Manifest, "%s is a setting and needs a manifest path", field)
		assert.Containsf(t, probes, field, "%s has no round-trip probe", field)
	default:
		t.Errorf("%s has an unknown kind %q", field, row.Kind)
	}
}

// leafFields lists a struct's fields as dotted paths, descending into the
// manifest structs the config embeds by value.
func leafFields(t reflect.Type, prefix string) []string {
	var out []string

	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		name := prefix + f.Name

		if f.Type.Kind() == reflect.Struct && f.Type != reflect.TypeOf(MultilineString("")) {
			out = append(out, leafFields(f.Type, name+".")...)

			continue
		}

		out = append(out, name)
	}

	return out
}

func fieldByPath(v reflect.Value, path string) reflect.Value {
	for _, part := range strings.Split(path, ".") {
		v = v.FieldByName(part)
	}

	return v
}

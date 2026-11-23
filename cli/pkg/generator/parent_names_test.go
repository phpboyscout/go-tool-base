package generator

import (
	"bytes"
	"go/types"
	"reflect"
	"strings"
	"testing"

	"github.com/dave/dst/decorator"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/cli/pkg/generator/templates"
	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

// Regenerate renders the root from commandImportAlias alone, while generate
// also reads the file, so a name the root uses and the fixed set omits is
// aliased by one and not the other (#223).
func TestParentNames_CoverEveryNameTheRootUses(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	require.NoError(t, templates.SkeletonRoot(fullRootData(t)).Render(&buf))

	f, err := decorator.Parse(buf.String())
	require.NoError(t, err)

	// These two come from the manifest, which regenerate reads for itself.
	fromManifest := map[string]bool{"hello": true, "releasetools": true}

	for name := range usedNames(f) {
		// A command's name has no capitals.
		if fromManifest[name] || name != strings.ToLower(name) || types.Universe.Lookup(name) != nil || name == "init" {
			continue
		}

		assert.Truef(t, parentNames[name], "the root uses %s, so a command of that name needs an alias", name)
	}
}

// An external command's alias comes from the manifest, so the fixed set can't
// hold it: regenerate must take it from there to choose what generate does.
func TestBuildSkeletonSubcommands_AliasesAsGenerateDoes(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	require.NoError(t, afero.WriteFile(fs, "/work/go.mod", []byte("module example.com/acme/widget\n"), 0o644))

	externals := []ManifestExternalCommand{
		{Module: "example.com/release-tools", Attach: []ManifestExternalAttach{{Constructor: "NewCmdPublish"}}},
		{Module: "example.com/other", Alias: "deploy", Attach: []ManifestExternalAttach{{Constructor: "NewCmdOther"}}},
		{Module: "example.com/fourth", Alias: "deploycmd", Attach: []ManifestExternalAttach{{Constructor: "NewCmdFourth"}}},
		{Module: "example.com/third", Alias: "timecmd", Attach: []ManifestExternalAttach{{Constructor: "NewCmdThird"}}},
	}

	var buf bytes.Buffer

	data := fullRootData(t)
	data.ExternalCommands = buildSkeletonExternalCommands(externals)
	require.NoError(t, templates.SkeletonRoot(data).Render(&buf))

	f, err := decorator.Parse(buf.String())
	require.NoError(t, err)

	g := New(&props.Props{FS: fs, Logger: logger.NewNoop()}, &Config{Path: "/work"})

	for name, want := range map[string]string{
		"releasetools": "releasetoolscmd", "deploy": "deploycmdcmd", "time": "timecmdcmd", "status": "status",
	} {
		generated := parentImportAlias(f, "example.com/acme/widget/pkg/cmd/"+name, commandPackageName(name), commandImportAlias(name))

		subs, err := g.buildSkeletonSubcommands(&Manifest{
			Properties: ManifestProperties{ExternalCommands: externals},
			Commands:   []ManifestCommand{{Name: name}},
		})
		require.NoError(t, err)

		assert.Equal(t, want, generated, "generate")
		assert.Equal(t, want, subs[0].PkgAlias, "regenerate")
	}
}

// A suffix must not land on another command's alias.
func TestBuildSkeletonSubcommands_AliasesAreDistinct(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	require.NoError(t, afero.WriteFile(fs, "/work/go.mod", []byte("module example.com/acme/widget\n"), 0o644))

	g := New(&props.Props{FS: fs, Logger: logger.NewNoop()}, &Config{Path: "/work"})

	subs, err := g.buildSkeletonSubcommands(&Manifest{
		Properties: ManifestProperties{ExternalCommands: []ManifestExternalCommand{
			{Module: "example.com/other", Alias: "deploy", Attach: []ManifestExternalAttach{{Constructor: "NewCmdOther"}}},
		}},
		Commands: []ManifestCommand{{Name: "deploy"}, {Name: "deploycmd"}},
	})
	require.NoError(t, err)
	assert.Equal(t, "deploycmd", subs[0].PkgAlias)
	assert.Equal(t, "deploycmdcmd", subs[1].PkgAlias)
}

// fullRootData fills every field, so the render reaches every import the
// template has. A field added later fails here until it is filled too.
func fullRootData(t *testing.T) templates.SkeletonRootData {
	t.Helper()

	var features []string
	for _, e := range templates.Catalogue() {
		features = append(features, string(e.ID))
	}

	yes := true
	data := templates.SkeletonRootData{
		Name: "widget", Description: "d", ReleaseProvider: "static", ReleaseBaseURL: "https://pkg.example.com/widget",
		Host: "git.example.com", Org: "acme", RepoName: "widget", Private: true,
		DisabledFeatures: features, EnabledFeatures: features,
		HelpType: "slack", SlackChannel: "c", SlackTeam: "t", TeamsChannel: "c", TeamsTeam: "t",
		TelemetryEndpoint: "https://t.example.com", TelemetryOTelEndpoint: "https://o.example.com", EnvPrefix: "W",
		ConfigLayers: []string{"defaults", "flags"}, ConfigFormat: "toml", ConfigKeyCase: "exact", ConfigFileOnly: []string{"update.**"},
		ConfigSources: []templates.ConfigSourceData{{Name: "team", Kind: "consul", Required: &yes, Writable: &yes}},
		UpdatePolicy:  "prompt", UpdateCheckInterval: "36h", MCPMode: "direct", MCPWithhold: []string{"x"},
		SigningEnabled: true, ModulePath: "example.com/acme/widget", AutoInitialise: true,
		SkipConfigCheck: []string{"a"}, AuxiliaryCommands: []string{"b"}, RequireChecksum: true,
		Subcommands: []templates.SkeletonSubcommand{{
			ImportPath: "example.com/acme/widget/pkg/cmd/hello", PkgAlias: "hello", Constructor: "NewCmdHello",
		}},
		ExternalCommands: []templates.SkeletonExternalCommand{{
			ImportPath: "example.com/release-tools", PkgAlias: "releasetools", Constructor: "NewCmdPublish",
			Args: []string{"logger"}, Wrap: true,
		}},
		ExternalAdapter: true,
	}

	v := reflect.ValueOf(data)
	for i := range v.NumField() {
		require.Falsef(t, v.Field(i).IsZero(), "%s is not filled", v.Type().Field(i).Name)
	}

	return data
}

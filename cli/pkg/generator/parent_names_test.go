package generator

import (
	"bytes"
	"go/ast"
	"go/parser"
	"go/token"
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
	data.ExternalCommands = buildSkeletonExternalCommands(externals, nil)
	require.NoError(t, templates.SkeletonRoot(data).Render(&buf))

	f, err := decorator.Parse(buf.String())
	require.NoError(t, err)

	g := New(&props.Props{FS: fs, Logger: logger.NewNoop()}, &Config{Path: "/work"})

	for name, want := range map[string]string{
		"releasetools": "releasetoolscmd", "deploy": "deploycmdcmd", "time": "timecmdcmd", "status": "status",
	} {
		generated := parentImportAlias(f, "example.com/acme/widget/pkg/cmd/"+name, commandPackageName(name), commandImportAlias(name), nil)

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

// jennifer keeps imports clear of each other but not of what the file declares,
// so every declaration NewCmdRoot's calls can see must be in rootDeclares.
func TestRootDeclares_CoversEveryDeclaration(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	require.NoError(t, templates.SkeletonRoot(fullRootData(t)).Render(&buf))

	f, err := parser.ParseFile(token.NewFileSet(), "cmd.go", buf.Bytes(), parser.SkipObjectResolution)
	require.NoError(t, err)

	declared := map[string]bool{}

	for _, decl := range f.Decls {
		addDeclared(declared, decl)

		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Name.Name != "NewCmdRoot" {
			continue
		}

		for _, param := range fn.Type.Params.List {
			for _, name := range param.Names {
				declared[name.Name] = true
			}
		}

		for _, stmt := range fn.Body.List {
			if assign, ok := stmt.(*ast.AssignStmt); ok {
				for _, lhs := range assign.Lhs {
					declared[lhs.(*ast.Ident).Name] = true
				}
			}
		}
	}

	// rootCmd is in scope only after the calls, and jennifer renames err.
	delete(declared, "rootCmd")
	delete(declared, "err")

	assert.Equal(t, rootDeclares, declared)
}

func TestBuildSkeletonExternalCommands_KeepsClearOfTheRoot(t *testing.T) {
	t.Parallel()

	declared := map[string]bool{"deploy": true, "Publish": true, "deploycmd": true}

	tests := map[string]struct {
		module, alias string
		want          string
	}{
		"a root local":             {"example.com/x", "tool", "toolcmd"},
		"a short root local":       {"example.com/x", "p", "pcmd"},
		"init":                     {"example.com/x", "init", "initcmd"},
		"the root's constructor":   {"example.com/x", "NewCmdRoot", "NewCmdRootcmd"},
		"another file's name":      {"example.com/x", "Publish", "Publishcmd"},
		"both candidates":          {"example.com/x", "deploy", "deploycmdcmd"},
		"derived from the module":  {"example.com/tool", "", "toolcmd"},
		"a name jennifer renames":  {"example.com/x", "props", "props"},
		"a free name":              {"example.com/x", "release", "release"},
		"free, derived from a dir": {"example.com/release-tools", "", "releasetools"},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got := buildSkeletonExternalCommands([]ManifestExternalCommand{{
				Module: tc.module, Alias: tc.alias, Attach: []ManifestExternalAttach{{Constructor: "NewCmdX"}},
			}}, declared)
			require.Len(t, got, 1)
			assert.Equal(t, tc.want, got[0].PkgAlias)
		})
	}
}

// The command's alias is chosen against the one the attachment ends up with.
func TestBuildSkeletonSubcommands_AvoidsASuffixedExternalAlias(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	require.NoError(t, afero.WriteFile(fs, "/work/go.mod", []byte("module example.com/acme/widget\n"), 0o644))
	require.NoError(t, afero.WriteFile(fs, "/work/pkg/cmd/root/publish.go", []byte("package root\n\nfunc publish() {}\n"), 0o644))

	g := New(&props.Props{FS: fs, Logger: logger.NewNoop()}, &Config{Path: "/work"})

	subs, err := g.buildSkeletonSubcommands(&Manifest{
		Properties: ManifestProperties{ExternalCommands: []ManifestExternalCommand{
			{Module: "example.com/other", Alias: "tool", Attach: []ManifestExternalAttach{{Constructor: "NewCmdOther"}}},
			{Module: "example.com/third", Alias: "publish", Attach: []ManifestExternalAttach{{Constructor: "NewCmdThird"}}},
		}},
		Commands: []ManifestCommand{{Name: "tool"}, {Name: "publish"}},
	})
	require.NoError(t, err)
	assert.Equal(t, "toolcmdcmd", subs[0].PkgAlias)
	assert.Equal(t, "publishcmdcmd", subs[1].PkgAlias, "publish is declared beside the root, so the attachment has publishcmd")
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

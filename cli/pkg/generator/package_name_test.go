package generator

import (
	"path"
	"path/filepath"
	"testing"

	"github.com/dave/dst/decorator"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

func TestCommandPackageName(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"deploy":     "deploy",
		"my-cmd":     "my_cmd",
		"import":     "importcmd",
		"type":       "typecmd",
		"go":         "gocmd",
		"main":       "maincmd",
		"init":       "init",
		"importcmd":  "importcmd",
		"my-import":  "my_import",
		"snake_case": "snake_case",

		"documentation": "documentationcmd",
	}

	for name, want := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, want, commandPackageName(name))
		})
	}
}

// A name a parent's cmd.go already uses would shadow it or clash with it, so
// the parent imports the command under a suffixed alias. The package keeps its
// name (#189).
func TestCommandImportAlias(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"deploy":   "deploy",
		"import":   "importcmd",
		"error":    "errorcmd",
		"init":     "initcmd",
		"main":     "maincmd",
		"string":   "stringcmd",
		"nil":      "nilcmd",
		"len":      "lencmd",
		"props":    "propscmd",
		"cmd":      "cmdcmd",
		"assets":   "assetscmd",
		"cobra":    "cobracmd",
		"setup":    "setupcmd",
		"tool":     "toolcmd",
		"err":      "errcmd",
		"os":       "oscmd",
		"afero":    "aferocmd",
		"base64":   "base64cmd",
		"external": "externalcmd",
		"time":     "timecmd",
		"forge":    "forgecmd",
		"config":   "config",
		"my-cmd":   "my_cmd",
	}

	for name, want := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, want, commandImportAlias(name))
		})
	}
}

func TestParentImportAlias(t *testing.T) {
	t.Parallel()

	const src = `package root

import (
	"github.com/spf13/cobra"
	aferocmd "example.com/tool/pkg/cmd/aferocmd"
	len "example.com/tool/pkg/cmd/len"
	"example.com/tool/pkg/cmd/my-cmd"
	"gopkg.in/yaml.v3"
	"example.com/lib/semver/v2"
	"k8s.io/api/core/v1"
	"github.com/example/go-redis"
	"example.com/lib/oddly-named"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
)

func NewCmdRoot() (*cobra.Command, *setup.Command) {
	var pod v1.Pod

	release := semver.MustParse(yaml.Version)
	probe, probecmd := 1, 2
	use(probe, probecmd)
	use(redis.NewClient(), quite.Different(pod), release, len.New(), my_cmd.New(), aferocmd.New())

	return nil, nil
}
`

	f, err := decorator.Parse(src)
	require.NoError(t, err)

	tests := []struct {
		name, importPath, alias, want string
	}{
		{"a free alias", "example.com/tool/pkg/cmd/deploy", "deploy", "deploy"},
		{"an import the parent already has", "example.com/tool/pkg/cmd/cobra", "cobra", "cobracmd"},
		{"an alias another command already holds", "example.com/tool/pkg/cmd/other/aferocmd", "aferocmd", "aferocmdcmd"},
		{"an existing import keeps its alias", "example.com/tool/pkg/cmd/len", "lencmd", "len"},
		{"an existing import with no alias keeps its package name", "example.com/tool/pkg/cmd/my-cmd", "my_cmd", "my_cmd"},
		{"the setup import", "example.com/tool/pkg/cmd/setup", "setupcmd", "setupcmd"},
		{"a gopkg.in import binds its name without the version", "example.com/tool/pkg/cmd/yaml", "yaml", "yamlcmd"},
		{"a major-version import binds the element before it", "example.com/tool/pkg/cmd/semver", "semver", "semvercmd"},
		{"a package named after its version element", "example.com/tool/pkg/cmd/v1", "v1", "v1cmd"},
		{"the element before a version-named package is free", "example.com/tool/pkg/cmd/core", "core", "core"},
		{"a package whose path has a go- prefix", "example.com/tool/pkg/cmd/redis", "redis", "rediscmd"},
		{"a package named nothing like its path", "example.com/tool/pkg/cmd/quite", "quite", "quitecmd"},
		{"a local variable", "example.com/tool/pkg/cmd/release", "release", "releasecmd"},
		{"a name whose suffixed form is taken too", "example.com/tool/pkg/cmd/probe", "probe", "probecmdcmd"},
		{"the parent's own package name is free", "example.com/tool/pkg/cmd/root/root", "root", "root"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, parentImportAlias(f, tc.importPath, commandPackageName(path.Base(tc.importPath)), tc.alias, nil))
		})
	}
}

// registerInParent registers name under a parent whose cmd.go is parentSrc,
// and returns the parent file afterwards.
func registerInParent(t *testing.T, name, parentSrc string) string {
	t.Helper()

	return wireInParent(t, name, parentSrc, (*Generator).RegisterSubcommand)
}

// removeFromParent deregisters name from a parent whose cmd.go is parentSrc,
// and returns the parent file afterwards.
func removeFromParent(t *testing.T, name, parentSrc string) string {
	t.Helper()

	return wireInParent(t, name, parentSrc, (*Generator).DeregisterSubcommand)
}

func wireInParent(t *testing.T, name, parentSrc string, wire func(*Generator) error) string {
	t.Helper()

	return wireInParentOn(t, afero.NewMemMapFs(), name, parentSrc, wire)
}

func wireInParentOn(t *testing.T, fs afero.Fs, name, parentSrc string, wire func(*Generator) error) string {
	t.Helper()

	parentFile := filepath.Join("/work", "pkg/cmd/parent/cmd.go")
	require.NoError(t, fs.MkdirAll(filepath.Join("/work", "pkg/cmd/parent", name), 0o755))
	require.NoError(t, afero.WriteFile(fs, "/work/go.mod", []byte("module test-tool\n"), 0o644))
	require.NoError(t, afero.WriteFile(fs, parentFile, []byte(parentSrc), 0o644))

	manifest, err := yaml.Marshal(Manifest{Commands: []ManifestCommand{{Name: "parent"}}})
	require.NoError(t, err)
	require.NoError(t, fs.MkdirAll("/work/.gtb", 0o755))
	require.NoError(t, afero.WriteFile(fs, "/work/.gtb/manifest.yaml", manifest, 0o644))

	g := New(&props.Props{FS: fs, Logger: logger.NewNoop(), Tool: props.Tool{Name: "test-tool"}},
		&Config{Path: "/work", Name: name, Parent: "parent"})
	require.NoError(t, wire(g))

	out, err := afero.ReadFile(fs, parentFile)
	require.NoError(t, err)

	return string(out)
}

func TestRegisterSubcommand_ImportsAClashingNameUnderAnAlias(t *testing.T) {
	t.Parallel()

	out := registerInParent(t, "cobra", `package parent

import (
	"github.com/spf13/cobra"
)

func NewCmdParent(props *props.Props) *cobra.Command {
	cmd := &cobra.Command{Use: "parent"}
	return cmd
}
`)

	assert.Contains(t, out, `cobracmd "test-tool/pkg/cmd/parent/cobra"`)
	assert.Contains(t, out, "cmd.Register(cobracmd.NewCmdCobra(props))")
}

// A parent written before cli/v0.41.0 imports a child with no alias, so the
// package clause names it, not the dashed directory.
func TestRegisterSubcommand_FindsAnUnaliasedExistingImport(t *testing.T) {
	t.Parallel()

	const parent = `package parent

import (
	"github.com/spf13/cobra"
	"test-tool/pkg/cmd/parent/my-cmd"
)

func NewCmdParent(props *props.Props) *cobra.Command {
	cmd := &cobra.Command{Use: "parent"}
	cmd.Register(my_cmd.NewCmdMyCmd(props))
	return cmd
}
`

	assert.Equal(t, parent, registerInParent(t, "my-cmd", parent))
}

func TestBuildSkeletonSubcommands_AliasesAClashingName(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	require.NoError(t, afero.WriteFile(fs, "/work/go.mod", []byte("module test-tool\n"), 0o644))

	g := New(&props.Props{FS: fs, Logger: logger.NewNoop()}, &Config{Path: "/work"})
	subs, err := g.buildSkeletonSubcommands(&Manifest{Commands: []ManifestCommand{{Name: "error"}, {Name: "deploy"}}})
	require.NoError(t, err)
	require.Len(t, subs, 2)
	assert.Equal(t, "errorcmd", subs[0].PkgAlias)
	assert.Equal(t, "deploy", subs[1].PkgAlias)
}

// mainEraVersionParent registers a version child the way gtb did before
// version took a cmd suffix.
const mainEraVersionParent = `package parent

import (
	"github.com/spf13/cobra"
	version "test-tool/pkg/cmd/parent/version"
)

func NewCmdParent(props *props.Props) *cobra.Command {
	cmd := &cobra.Command{Use: "parent"}
	cmd.Register(version.NewCmdVersion(props))
	return cmd
}
`

func TestRegisterSubcommand_KeepsAnExistingAliasForAFixedName(t *testing.T) {
	t.Parallel()

	assert.Equal(t, mainEraVersionParent, registerInParent(t, "version", mainEraVersionParent))
}

func TestDeregisterSubcommand_RemovesUnderTheParentsAlias(t *testing.T) {
	t.Parallel()

	out := removeFromParent(t, "version", mainEraVersionParent)
	assert.NotContains(t, out, "pkg/cmd/parent/version")
	assert.NotContains(t, out, "NewCmdVersion")
}

// A clash with an import only this parent has is found in the file, not in
// the fixed set, so remove must find it there too.
func TestRegisterAndRemove_AClashWithAHandAddedImport(t *testing.T) {
	t.Parallel()

	const parent = `package parent

import (
	"example.com/lib/deploy"
	"github.com/spf13/cobra"
)

func NewCmdParent(props *props.Props) *cobra.Command {
	cmd := &cobra.Command{Use: "parent"}
	deploy.Prepare()
	return cmd
}
`

	registered := registerInParent(t, "deploy", parent)
	assert.Contains(t, registered, `deploycmd "test-tool/pkg/cmd/parent/deploy"`)
	assert.Contains(t, registered, "cmd.Register(deploycmd.NewCmdDeploy(props))")

	removed := removeFromParent(t, "deploy", registered)
	assert.NotContains(t, removed, "deploycmd")
	assert.Contains(t, removed, "deploy.Prepare()")
}

// A name another file of the parent's package declares cannot be an import in
// cmd.go as well (#227).
func TestParentImportAlias_AvoidsWhatOtherFilesDeclare(t *testing.T) {
	t.Parallel()

	const parent = `package parent

import "github.com/spf13/cobra"

func NewCmdParent(props *props.Props) *cobra.Command {
	cmd := &cobra.Command{Use: "parent"}
	return cmd
}
`

	tests := map[string]struct {
		file, src string
		want      string
	}{
		"func":               {"helpers.go", "package parent\n\nfunc deploy() {}\n", "deploycmd"},
		"var in a group":     {"helpers.go", "package parent\n\nvar (\n\ta, deploy int\n)\n", "deploycmd"},
		"const":              {"helpers.go", "package parent\n\nconst deploy = 1\n", "deploycmd"},
		"type":               {"helpers.go", "package parent\n\ntype deploy struct{}\n", "deploycmd"},
		"both candidates":    {"helpers.go", "package parent\n\nvar deploy, deploycmd int\n", "deploycmdcmd"},
		"internal test":      {"helpers_test.go", "package parent\n\nfunc deploy() {}\n", "deploycmd"},
		"method":             {"helpers.go", "package parent\n\ntype x int\n\nfunc (x) deploy() {}\n", "deploy"},
		"local":              {"helpers.go", "package parent\n\nfunc x() { deploy := 1; _ = deploy }\n", "deploy"},
		"import":             {"helpers.go", "package parent\n\nimport deploy \"fmt\"\n\nvar _ = deploy.Sprint\n", "deploy"},
		"external test":      {"helpers_test.go", "package parent_test\n\nfunc deploy() {}\n", "deploy"},
		"file that is no Go": {"helpers.go", "not go\n", "deploy"},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			fs := afero.NewMemMapFs()
			require.NoError(t, afero.WriteFile(fs, "/work/pkg/cmd/parent/"+tc.file, []byte(tc.src), 0o644))

			out := wireInParentOn(t, fs, "deploy", parent, (*Generator).RegisterSubcommand)
			assert.Contains(t, out, tc.want+` "test-tool/pkg/cmd/parent/deploy"`)
			assert.Contains(t, out, "cmd.Register("+tc.want+".NewCmdDeploy(props))")
		})
	}
}

func TestDeclaredBeside_ReadsEveryFileAndDeclaration(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	for name, src := range map[string]string{
		"a.go":      "not go\n",
		"a_test.go": "package parent_test\n\nfunc external() {}\n",
		"b.go":      "package parent\n\nfunc first() {}\n\nfunc (x) method() {}\n\ntype x int\n",
		"c.go":      "package parent\n\nvar last int\n",
		"cmd.go":    "package parent\n\nfunc own() {}\n",
		"notes.txt": "package parent\n\nfunc text() {}\n",
	} {
		require.NoError(t, afero.WriteFile(fs, "/work/pkg/cmd/parent/"+name, []byte(src), 0o644))
	}

	g := New(&props.Props{FS: fs, Logger: logger.NewNoop()}, &Config{Path: "/work"})

	assert.Equal(t, map[string]bool{"first": true, "x": true, "last": true}, g.declaredBeside("/work/pkg/cmd/parent/cmd.go"))
}

func TestBuildSkeletonSubcommands_AvoidsWhatOtherRootFilesDeclare(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	const project = "/work [old]"

	require.NoError(t, afero.WriteFile(fs, project+"/go.mod", []byte("module test-tool\n"), 0o644))
	require.NoError(t, afero.WriteFile(fs, project+"/pkg/cmd/root/helpers.go", []byte("package root\n\nfunc deploy() {}\n"), 0o644))
	require.NoError(t, afero.WriteFile(fs, project+"/pkg/cmd/root/cmd.go", []byte("package root\n\nfunc publish() {}\n"), 0o644))

	g := New(&props.Props{FS: fs, Logger: logger.NewNoop()}, &Config{Path: project})
	subs, err := g.buildSkeletonSubcommands(&Manifest{Commands: []ManifestCommand{{Name: "deploy"}, {Name: "publish"}}})
	require.NoError(t, err)
	require.Len(t, subs, 2)
	assert.Equal(t, "deploycmd", subs[0].PkgAlias)
	assert.Equal(t, "publish", subs[1].PkgAlias, "regenerate rewrites cmd.go, so its own names are free")
}

func TestPrepareAndVerify_RefusesAChildOfVendor(t *testing.T) {
	t.Parallel()

	tests := map[string]bool{
		"":               false,
		"root":           false,
		"vendors":        false,
		"deploy":         false,
		"vendor":         true,
		"/vendor/":       true,
		"deploy/vendor":  true,
		"vendor/widgets": true,
	}

	for parent, refused := range tests {
		t.Run(parent, func(t *testing.T) {
			t.Parallel()

			fs := afero.NewMemMapFs()
			require.NoError(t, fs.MkdirAll(filepath.Join("/work/pkg/cmd", parent), 0o755))

			raw, err := yaml.Marshal(Manifest{Commands: []ManifestCommand{
				{Name: "vendors"},
				{Name: "deploy", Commands: []ManifestCommand{{Name: "vendor"}}},
				{Name: "vendor", Commands: []ManifestCommand{{Name: "widgets"}}},
			}})
			require.NoError(t, err)
			require.NoError(t, afero.WriteFile(fs, "/work/.gtb/manifest.yaml", raw, 0o644))

			g := New(&props.Props{FS: fs, Logger: logger.NewNoop(), Config: emptyTestStore(t)},
				&Config{Path: "/work", Name: "vendor", Parent: parent})

			_, err = g.prepareAndVerify()
			if refused {
				require.ErrorIs(t, err, ErrVendorParent)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

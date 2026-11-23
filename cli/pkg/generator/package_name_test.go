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
		"importcmd":  "importcmd",
		"my-import":  "my_import",
		"snake_case": "snake_case",
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
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
)
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
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, parentImportAlias(f, tc.importPath, commandPackageName(path.Base(tc.importPath)), tc.alias))
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

	fs := afero.NewMemMapFs()
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
	subs, err := g.buildSkeletonSubcommands([]ManifestCommand{{Name: "error"}, {Name: "deploy"}})
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

package generator

import (
	"context"
	"errors"
	"os"
	"path"
	"strings"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	gterrors "gitlab.com/phpboyscout/go/errors"
)

const unreadableManifest = `properties:
  name: test-tool
commands:
  - name: approve
    description: Approve
`

func TestRegenerateManifest_StopsOnASourceItCannotParse(t *testing.T) {
	t.Parallel()

	for name, path := range map[string]string{
		"the command":          "/work/pkg/cmd/approve/cmd.go",
		"the root":             "/work/pkg/cmd/root/cmd.go",
		"a nested command":     "/work/pkg/cmd/approve/nested/cmd.go",
		"a file beside one":    "/work/pkg/cmd/approve/helpers.go",
		"a name like testdata": "/work/pkg/cmd/approve/testdatas/cmd.go",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			fs, g := roundTripProject(t, "approve", approveSource, unreadableManifest)
			require.NoError(t, afero.WriteFile(fs, path, []byte("package approve\n\nfunc {\n"), 0o644))

			err := g.RegenerateManifest(context.Background())
			require.ErrorIs(t, err, ErrCommandSourceUnreadable)
			require.ErrorContains(t, err, path)
			assert.Contains(t, gterrors.FlattenHints(err), path)

			raw, _ := readRoundTripManifest(t, fs)
			assert.Equal(t, unreadableManifest, raw)
		})
	}
}

// openErrFs fails to open one path.
type openErrFs struct {
	afero.Fs
	path string
}

var errOpenRefused = errors.New("open refused")

func (f openErrFs) Open(name string) (afero.File, error) {
	if name == f.path {
		return nil, errOpenRefused
	}

	return f.Fs.Open(name)
}

func (f openErrFs) OpenFile(name string, flag int, perm os.FileMode) (afero.File, error) {
	if name == f.path {
		return nil, errOpenRefused
	}

	return f.Fs.OpenFile(name, flag, perm)
}

func TestRegenerateManifest_StopsOnASourceItCannotOpen(t *testing.T) {
	t.Parallel()

	for name, path := range map[string]string{
		"a file":      "/work/pkg/cmd/approve/cmd.go",
		"a directory": "/work/pkg/cmd/approve",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			fs, g := roundTripProject(t, "approve", approveSource, unreadableManifest)
			g.props.FS = openErrFs{Fs: fs, path: path}

			err := g.RegenerateManifest(context.Background())
			require.ErrorIs(t, err, ErrCommandSourceUnreadable)
			require.ErrorIs(t, err, errOpenRefused)
			require.ErrorContains(t, err, path)

			raw, _ := readRoundTripManifest(t, fs)
			assert.Equal(t, unreadableManifest, raw)
		})
	}
}

// The go command leaves testdata out of patterns, and builds one that is imported.
func TestRegenerateManifest_ACommandNamedTestdata(t *testing.T) {
	t.Parallel()

	const manifest = "properties:\n  name: test-tool\ncommands:\n  - name: testdata\n    description: Data\n"

	source := strings.ReplaceAll(strings.ReplaceAll(approveSource, "approve", "testdata"), "Approve", "Testdata")

	for _, name := range []string{"testdata", "testdatas"} {
		t.Run("a registered "+name+" is read", func(t *testing.T) {
			t.Parallel()

			fs, g := roundTripProject(t, name, strings.ReplaceAll(strings.ReplaceAll(source, "testdata", name), "Testdata", PascalCase(name)), manifest)
			require.NoError(t, g.RegenerateManifest(context.Background()))

			_, m := readRoundTripManifest(t, fs)
			require.Len(t, m.Commands, 1)
			assert.Equal(t, name, m.Commands[0].Name)
		})

		t.Run("a registered "+name+" that does not parse stops the run", func(t *testing.T) {
			t.Parallel()

			fs, g := roundTripProject(t, name, "package "+name+"\n\nfunc {\n", manifest)
			require.ErrorIs(t, g.RegenerateManifest(context.Background()), ErrCommandSourceUnreadable)

			raw, _ := readRoundTripManifest(t, fs)
			assert.Equal(t, manifest, raw)
		})
	}

	t.Run("one beneath it is registered", func(t *testing.T) {
		t.Parallel()

		fs, g := roundTripProject(t, "testdata", "package testdata\n\nfunc {\n", manifest)
		root, err := afero.ReadFile(fs, "/work/pkg/cmd/root/cmd.go")
		require.NoError(t, err)

		nested := strings.Replace(string(root), "pkg/cmd/testdata", "pkg/cmd/testdata/nested", 1)
		nested = strings.Replace(nested, "testdata.NewCmd", "nested.NewCmd", 1)
		require.NoError(t, afero.WriteFile(fs, "/work/pkg/cmd/root/cmd.go", []byte(nested), 0o644))

		require.ErrorIs(t, g.RegenerateManifest(context.Background()), ErrCommandSourceUnreadable)
	})
}

// legacyProject has the root register, from dir, a command named for it.
func legacyProject(t *testing.T, dir, source string) (afero.Fs, *Generator) {
	t.Helper()

	constructor := "NewCmd" + PascalCase(path.Base(dir))

	fs, g := roundTripProject(t, "approve", approveSource, unreadableManifest)
	require.NoError(t, fs.RemoveAll("/work/pkg/cmd/approve"))
	require.NoError(t, afero.WriteFile(fs, "/work/pkg/cmd/"+dir+"/cmd.go", []byte(strings.Replace(source, "NewCmdApprove", constructor, 1)), 0o644))

	root, err := afero.ReadFile(fs, "/work/pkg/cmd/root/cmd.go")
	require.NoError(t, err)

	moved := strings.NewReplacer(`"test-tool/pkg/cmd/approve"`, `approve "test-tool/pkg/cmd/`+dir+`"`, "NewCmdApprove", constructor).Replace(string(root))
	require.NoError(t, afero.WriteFile(fs, "/work/pkg/cmd/root/cmd.go", []byte(moved), 0o644))

	return fs, g
}

// The go command leaves these out of patterns too, and builds one that is
// imported, so the scan reads it. regenerate project would scaffold it at
// pkg/cmd/approve, so the rebuild then refuses it there (#262).
func TestRegenerateManifest_ACommandInADirectoryOutsidePatterns(t *testing.T) {
	t.Parallel()

	const child = `package now
import (
	"github.com/spf13/cobra"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
)
func NewCmdNow(props *props.Props) *setup.Command {
	return setup.Wrap("now", &cobra.Command{Use: "now", Short: "Now"})
}`

	parent := strings.Replace(approveSource, "import (", "import (\n\t\"test-tool/pkg/cmd/DIR/now\"", 1)
	parent = strings.Replace(parent, "return setup.Wrap(", "cmd := setup.Wrap(", 1)
	parent = strings.Replace(parent, "\t})\n}", "\t})\n\tcmd.AddCommand(now.NewCmdNow(props))\n\treturn cmd\n}", 1)

	for _, dir := range []string{"_legacy", ".legacy/deploy", "testdata", "hook/_legacy", "hook/testdata", "_legacy/nested"} {
		t.Run(dir+" is read and refused where it is", func(t *testing.T) {
			t.Parallel()

			fs, g := legacyProject(t, dir, strings.Replace(parent, "DIR", dir, 1))
			require.NoError(t, afero.WriteFile(fs, "/work/pkg/cmd/"+dir+"/now/cmd.go", []byte(child), 0o644))

			err := g.RegenerateManifest(context.Background())
			require.ErrorIs(t, err, ErrCommandMisplaced)
			assert.Contains(t, gterrors.FlattenHints(err), "at /work/pkg/cmd/approve. Move /work/pkg/cmd/"+dir+" there with its constructor renamed from NewCmd")
			assert.NotContains(t, gterrors.FlattenHints(err), "Use back", "a directory go leaves out is no place to keep a command")

			raw, _ := readRoundTripManifest(t, fs)
			assert.Equal(t, unreadableManifest, raw)
		})

		t.Run(dir+" that does not parse stops the run", func(t *testing.T) {
			t.Parallel()

			fs, g := legacyProject(t, dir, "package approve\n\nfunc {\n")
			require.ErrorIs(t, g.RegenerateManifest(context.Background()), ErrCommandSourceUnreadable)

			raw, _ := readRoundTripManifest(t, fs)
			assert.Equal(t, unreadableManifest, raw)
		})
	}

	t.Run("a child that does not parse stops the run", func(t *testing.T) {
		t.Parallel()

		hook := strings.Replace(parent, `"test-tool/pkg/cmd/DIR/now"`, `now "test-tool/pkg/cmd/_now"`, 1)
		fs, g := legacyProject(t, "hook", hook)
		require.NoError(t, afero.WriteFile(fs, "/work/pkg/cmd/_now/cmd.go", []byte("func {\n"), 0o644))
		require.ErrorIs(t, g.RegenerateManifest(context.Background()), ErrCommandSourceUnreadable)
	})

	t.Run("one that only an unregistered command registers is left out", func(t *testing.T) {
		t.Parallel()

		fs, g := roundTripProject(t, "approve", approveSource, unreadableManifest)
		orphan := strings.Replace(parent, `"test-tool/pkg/cmd/DIR/now"`, `now "test-tool/pkg/cmd/_old/testdata"`, 1)
		orphan = strings.NewReplacer("approve", "old", "Approve", "Old").Replace(orphan)
		require.NoError(t, afero.WriteFile(fs, "/work/pkg/cmd/_old/cmd.go", []byte(orphan), 0o644))
		require.NoError(t, afero.WriteFile(fs, "/work/pkg/cmd/_old/testdata/cmd.go", []byte("func {\n"), 0o644))
		require.NoError(t, g.RegenerateManifest(context.Background()))

		_, m := readRoundTripManifest(t, fs)
		require.Len(t, m.Commands, 1)
	})
}

// The scan reads no command from a file Go never builds, so a registration of
// the package holding only that file finds nothing, and the rebuild stops
// rather than drop the command.
func TestRegenerateManifest_AFileGoNeverBuildsIsNoCommand(t *testing.T) {
	t.Parallel()

	for _, name := range []string{"_cmd.go", ".cmd.go"} {
		fs, g := roundTripProject(t, "approve", approveSource, unreadableManifest)
		require.NoError(t, afero.WriteFile(fs, "/work/pkg/cmd/approve/"+name, []byte(approveSource), 0o644))
		require.NoError(t, fs.Remove("/work/pkg/cmd/approve/cmd.go"))
		before, _ := readRoundTripManifest(t, fs)

		require.ErrorIs(t, g.RegenerateManifest(context.Background()), ErrCommandNotFound)

		after, _ := readRoundTripManifest(t, fs)
		assert.Equal(t, before, after, "nothing is written")
	}
}

func TestRegenerateManifest_LeavesOutWhatGoLeavesOut(t *testing.T) {
	t.Parallel()

	const broken = "func {\n"

	for _, path := range []string{
		"/work/pkg/cmd/approve/testdata/broken.go",
		"/work/pkg/cmd/approve/_old/cmd.go",
		"/work/pkg/cmd/approve/.cache/cmd.go",
		"/work/pkg/cmd/approve/_old.go",
		"/work/pkg/cmd/approve/.#cmd.go",
		"/work/pkg/cmd/approve/cmd_test.go",
		"/work/pkg/cmd/approve/notes.txt",
		"/work/pkg/cmd/approve/assets/broken.go",
	} {
		t.Run(path, func(t *testing.T) {
			t.Parallel()

			fs, g := roundTripProject(t, "approve", approveSource, unreadableManifest)
			require.NoError(t, afero.WriteFile(fs, path, []byte(broken), 0o644))
			require.NoError(t, g.RegenerateManifest(context.Background()))

			_, m := readRoundTripManifest(t, fs)
			require.Len(t, m.Commands, 1)
			assert.Equal(t, "approve", m.Commands[0].Name)
		})
	}
}

func TestRegenerateManifest_SkipsAFileWithNoCommand(t *testing.T) {
	t.Parallel()

	for _, path := range []string{
		"/work/pkg/cmd/approve/helpers.go",
		"/work/pkg/cmd/approve/testdata.go",
	} {
		fs, g := roundTripProject(t, "approve", approveSource, unreadableManifest)
		require.NoError(t, afero.WriteFile(fs, path, []byte("package approve\n"), 0o644))
		require.NoError(t, g.RegenerateManifest(context.Background()))

		_, m := readRoundTripManifest(t, fs)
		require.Len(t, m.Commands, 1)
	}
}

// registeringSource is a command at dir that registers each child, given as a
// directory under pkg/cmd.
func registeringSource(dir string, children ...string) string {
	pkg := strings.TrimLeft(path.Base(dir), "_.")

	var imports, adds strings.Builder

	for i, child := range children {
		alias := "c" + string(rune('a'+i))
		imports.WriteString("\t" + alias + ` "test-tool/pkg/cmd/` + child + "\"\n")
		adds.WriteString("\tcmd.AddCommand(" + alias + ".NewCmd" + PascalCase(strings.TrimLeft(path.Base(child), "_.")) + "(props))\n")
	}

	return "package " + pkg + `
import (
	"github.com/spf13/cobra"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
` + imports.String() + `)
func NewCmd` + PascalCase(pkg) + `(props *props.Props) *setup.Command {
	cmd := setup.Wrap("` + pkg + `", &cobra.Command{Use: "` + pkg + `", Short: "` + pkg + `"})
` + adds.String() + `	return cmd
}`
}

// Only _deep is registered, by a grandchild in the middle of its siblings and in
// the middle of its constructors, with an unregistered unread directory and an
// orphaned command sorting on either side, so the scan has to look at all of them.
func TestRegenerateManifest_AnUnreadDirectoryRegisteredAmongOthers(t *testing.T) {
	t.Parallel()

	fs, g := roundTripProject(t, "approve", approveSource, unreadableManifest)
	require.NoError(t, fs.RemoveAll("/work/pkg/cmd/approve"))

	sources := map[string]string{
		"first":           registeringSource("first"),
		"hook":            registeringSource("hook", "hook/left", "hook/kid", "hook/right"),
		"hook/left":       registeringSource("hook/left"),
		"hook/kid":        registeringSource("hook/kid", "hook/kid/before", "_deep", "hook/kid/after"),
		"hook/kid/before": registeringSource("hook/kid/before"),
		"hook/kid/after":  registeringSource("hook/kid/after"),
		"hook/right":      registeringSource("hook/right"),
		"last":            registeringSource("last"),
		"alpha":           registeringSource("alpha"),
		"zulu":            registeringSource("zulu"),
		"_a":              "func {\n",
		"_deep":           "func {\n",
		"_z":              "func {\n",
	}
	for dir, source := range sources {
		require.NoError(t, afero.WriteFile(fs, "/work/pkg/cmd/"+dir+"/cmd.go", []byte(source), 0o644))
	}

	root := `package root
import (
	gtbRoot "gitlab.com/phpboyscout/go-tool-base/pkg/cmd/root"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
	"test-tool/pkg/cmd/first"
	"test-tool/pkg/cmd/hook"
	"test-tool/pkg/cmd/last"
)
func NewCmdRoot(p *props.Props) *setup.Command {
	return gtbRoot.NewCmdRoot(p, first.NewCmdFirst(p), hook.NewCmdHook(p), last.NewCmdLast(p))
}`
	require.NoError(t, afero.WriteFile(fs, "/work/pkg/cmd/root/cmd.go", []byte(root), 0o644))

	err := g.RegenerateManifest(context.Background())
	require.ErrorIs(t, err, ErrCommandSourceUnreadable)
	require.ErrorContains(t, err, "/work/pkg/cmd/_deep/cmd.go")

	raw, _ := readRoundTripManifest(t, fs)
	assert.Equal(t, unreadableManifest, raw)

	require.NoError(t, afero.WriteFile(fs, "/work/pkg/cmd/_deep/cmd.go", []byte(registeringSource("_deep")), 0o644))
	err = g.RegenerateManifest(context.Background())
	require.ErrorIs(t, err, ErrCommandOutsideParent, "readable, _deep is still registered from another subtree")
	require.ErrorContains(t, err, "/work/pkg/cmd/_deep")
}

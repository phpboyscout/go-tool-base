package generator

import (
	"context"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const keywordTestModule = "github.com/test/test-project"

func TestGenerate_KeywordCommandNames(t *testing.T) {
	t.Setenv("GTB_NON_INTERACTIVE", "true")

	path := "/work"
	p := setupTestProject(t, path)

	generateCmd(t, p, path, "handover", "root")
	generateCmd(t, p, path, "import", "handover")
	generateCmd(t, p, path, "go", "root")

	assertPackageClause(t, p.FS, filepath.Join(path, "pkg/cmd/handover/import/cmd.go"), "importcmd")
	assertPackageClause(t, p.FS, filepath.Join(path, "pkg/cmd/go/cmd.go"), "gocmd")
	assertAliasedImport(t, p.FS, filepath.Join(path, "pkg/cmd/handover/cmd.go"),
		keywordTestModule+"/pkg/cmd/handover/import", "importcmd")
	assertFileContains(t, p.FS, filepath.Join(path, "pkg/cmd/handover/cmd.go"), "importcmd.NewCmdImport(")

	// The root's own file is left out: its call layout shifts on regenerate
	// whatever the names, because this setup stubs out the formatter.
	commandDirs := []string{"pkg/cmd/handover", "pkg/cmd/go"}
	before := snapshotTrees(t, p.FS, path, commandDirs)

	g := New(p, &Config{Path: path, Name: "test-project"})
	require.NoError(t, g.RegenerateProject(context.Background()))

	assertAliasedImport(t, p.FS, filepath.Join(path, "pkg/cmd/root/cmd.go"),
		keywordTestModule+"/pkg/cmd/go", "gocmd")
	assertFileContains(t, p.FS, filepath.Join(path, "pkg/cmd/root/cmd.go"), "gocmd.NewCmdGo(")
	assert.Equal(t, before, snapshotTrees(t, p.FS, path, commandDirs),
		"regenerating a project with keyword-named commands must change nothing")
}

func assertPackageClause(t *testing.T, fs afero.Fs, file, want string) {
	t.Helper()

	src, err := afero.ReadFile(fs, file)
	require.NoError(t, err, file)

	f, err := parser.ParseFile(token.NewFileSet(), file, src, parser.PackageClauseOnly)
	require.NoError(t, err, file)
	assert.Equal(t, want, f.Name.Name, file)
}

func assertAliasedImport(t *testing.T, fs afero.Fs, file, importPath, alias string) {
	t.Helper()

	src, err := afero.ReadFile(fs, file)
	require.NoError(t, err, file)

	f, err := parser.ParseFile(token.NewFileSet(), file, src, parser.ImportsOnly)
	require.NoError(t, err, file)

	for _, imp := range f.Imports {
		if path, _ := strconv.Unquote(imp.Path.Value); path != importPath {
			continue
		}

		require.NotNil(t, imp.Name, "%s imports %s without an alias", file, importPath)
		assert.Equal(t, alias, imp.Name.Name, file)

		return
	}

	assert.Fail(t, "import not found", "%s does not import %s", file, importPath)
}

func assertFileContains(t *testing.T, fs afero.Fs, file, want string) {
	t.Helper()

	src, err := afero.ReadFile(fs, file)
	require.NoError(t, err, file)
	assert.Contains(t, string(src), want, file)
}

func snapshotTrees(t *testing.T, fs afero.Fs, base string, dirs []string) map[string]string {
	t.Helper()

	files := map[string]string{}

	for _, dir := range dirs {
		require.NoError(t, afero.Walk(fs, filepath.Join(base, dir), func(path string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() {
				return err
			}

			b, err := afero.ReadFile(fs, path)
			files[path] = string(b)

			return err
		}))
	}

	return files
}

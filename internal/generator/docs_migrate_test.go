package generator

import (
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/pkg/config"
	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

func TestMigrateFlatDocsToDataxis(t *testing.T) {
	t.Parallel()

	const root = "/work"
	fs := afero.NewMemMapFs()

	manifest := `properties:
  name: mytool
  docs_layout: flat
commands:
  - name: deploy
  - name: a
    commands:
      - name: run
`
	write := func(rel, content string) {
		require.NoError(t, fs.MkdirAll(filepath.Join(root, filepath.Dir(rel)), 0o755))
		require.NoError(t, afero.WriteFile(fs, filepath.Join(root, rel), []byte(content), 0o644))
	}
	write(".gtb/manifest.yaml", manifest)
	write("docs/commands/index.md", "INDEX")
	write("docs/commands/deploy/index.md", "DEPLOY DOC")
	write("docs/commands/a/index.md", "A DOC")
	write("docs/commands/a/run/index.md", "RUN DOC")
	write("docs/packages/index.md", "PKG INDEX")
	write("docs/packages/config/index.md", "CONFIG DOC")

	g := &Generator{
		props:  &props.Props{FS: fs, Logger: logger.NewNoop(), Config: config.NewFilesContainer(fs)},
		config: &Config{Path: root},
	}

	require.NoError(t, g.migrateFlatDocsToDiataxis())

	// Content is preserved at the new Diátaxis paths (leaf-flat / parent-subsection).
	for path, want := range map[string]string{
		"docs/reference/cli/index.md":           "INDEX",
		"docs/reference/cli/deploy.md":          "DEPLOY DOC", // leaf
		"docs/reference/cli/a/index.md":         "A DOC",      // parent (has child run)
		"docs/reference/cli/a/run.md":           "RUN DOC",    // leaf under a
		"docs/explanation/components/index.md":  "PKG INDEX",
		"docs/explanation/components/config.md": "CONFIG DOC",
	} {
		got, err := afero.ReadFile(fs, filepath.Join(root, path))
		require.NoErrorf(t, err, "expected migrated doc: %s", path)
		assert.Equalf(t, want, string(got), "content preserved at %s", path)
	}

	// The old flat trees are gone.
	for _, dir := range []string{"docs/commands", "docs/packages"} {
		exists, _ := afero.DirExists(fs, filepath.Join(root, dir))
		assert.Falsef(t, exists, "%s must be removed after migration", dir)
	}

	// The manifest now records the diataxis layout.
	man, err := afero.ReadFile(fs, filepath.Join(root, ".gtb/manifest.yaml"))
	require.NoError(t, err)
	assert.Contains(t, string(man), "docs_layout: diataxis")
}

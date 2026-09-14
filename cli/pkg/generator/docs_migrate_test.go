package generator

import (
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

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
		props:  &props.Props{FS: fs, Logger: logger.NewNoop(), Config: emptyTestStore(t)},
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

// TestMigrateFlatDocsToDiataxis_PartialTrees covers projects that have only a
// command tree or only a package tree — the moveFlatDocTree early-return when a
// flat tree is absent must not fail or spuriously create the other quadrant.
func TestMigrateFlatDocsToDiataxis_PartialTrees(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name        string
		files       map[string]string
		wantPresent string
		wantAbsent  []string
	}{
		{
			name: "command-only project (no docs/packages)",
			files: map[string]string{
				".gtb/manifest.yaml":            "properties:\n  name: t\n  docs_layout: flat\ncommands:\n  - name: deploy\n",
				"docs/commands/deploy/index.md": "DEPLOY",
			},
			wantPresent: "docs/reference/cli/deploy.md",
			wantAbsent:  []string{"docs/commands", "docs/explanation/components"},
		},
		{
			name: "package-only project (no docs/commands)",
			files: map[string]string{
				".gtb/manifest.yaml":            "properties:\n  name: t\n  docs_layout: flat\n",
				"docs/packages/config/index.md": "CONFIG",
			},
			wantPresent: "docs/explanation/components/config.md",
			wantAbsent:  []string{"docs/packages", "docs/reference/cli"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			const root = "/work"
			fs := afero.NewMemMapFs()

			for rel, content := range tc.files {
				require.NoError(t, fs.MkdirAll(filepath.Join(root, filepath.Dir(rel)), 0o755))
				require.NoError(t, afero.WriteFile(fs, filepath.Join(root, rel), []byte(content), 0o644))
			}

			g := &Generator{
				props:  &props.Props{FS: fs, Logger: logger.NewNoop(), Config: emptyTestStore(t)},
				config: &Config{Path: root},
			}

			require.NoError(t, g.migrateFlatDocsToDiataxis())

			present, _ := afero.Exists(fs, filepath.Join(root, tc.wantPresent))
			assert.Truef(t, present, "expected migrated doc: %s", tc.wantPresent)

			for _, p := range tc.wantAbsent {
				exists, _ := afero.DirExists(fs, filepath.Join(root, p))
				assert.Falsef(t, exists, "%s must be absent", p)
			}

			man, err := afero.ReadFile(fs, filepath.Join(root, ".gtb/manifest.yaml"))
			require.NoError(t, err)
			assert.Contains(t, string(man), "docs_layout: diataxis")
		})
	}
}

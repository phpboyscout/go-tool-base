package generator

import (
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCommandDocRelPath(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name        string
		rel         string
		hasChildren bool
		diataxis    bool
		want        string
	}{
		{"diataxis leaf", "deploy", false, true, "deploy.md"},
		{"diataxis parent", "a", true, true, filepath.Join("a", "index.md")},
		{"diataxis nested leaf", filepath.Join("a", "run"), false, true, filepath.Join("a", "run") + ".md"},
		{"flat leaf", "deploy", false, false, filepath.Join("deploy", "index.md")},
		{"flat parent", "a", true, false, filepath.Join("a", "index.md")},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, commandDocRelPath(tc.rel, tc.hasChildren, tc.diataxis))
		})
	}
}

// TestBuildCommandsIndexContent_Diataxis guards HIGH-2: leaf commands must link to
// <name>.md, not <name>/index.md, in the Diátaxis layout.
func TestBuildCommandsIndexContent_Diataxis(t *testing.T) {
	t.Parallel()

	g := newPromptGenerator(t, "", false)
	cmds := []ManifestCommand{
		{Name: "deploy", Description: "Deploy it"},
		{Name: "a", Commands: []ManifestCommand{{Name: "run"}}},
	}

	out := g.buildCommandsIndexContent(cmds, true)
	assert.Contains(t, out, "(deploy.md)", "leaf links to flat <name>.md")
	assert.Contains(t, out, "(a/index.md)", "parent links to subsection index")
	assert.Contains(t, out, "(a/run.md)", "nested leaf links to flat file")
	assert.NotContains(t, out, "deploy/index.md", "leaf must NOT use the directory form (HIGH-2)")

	flat := g.buildCommandsIndexContent(cmds, false)
	assert.Contains(t, flat, "(deploy/index.md)", "flat layout keeps the directory form")
}

// TestGenerateCommandsIndex_Diataxis exercises the full index writer end-to-end.
func TestGenerateCommandsIndex_Diataxis(t *testing.T) {
	t.Parallel()

	manifest := `properties:
  name: tool
  docs_layout: diataxis
commands:
  - name: deploy
    description: Deploy it
  - name: a
    commands:
      - name: run
`
	g := newPromptGenerator(t, manifest, false)
	require.NoError(t, g.generateCommandsIndex())

	idx, err := afero.ReadFile(g.props.FS, filepath.Join(g.config.Path, "docs/reference/cli/index.md"))
	require.NoError(t, err)
	s := string(idx)

	assert.Contains(t, s, "(deploy.md)")
	assert.Contains(t, s, "(a/index.md)")
	assert.Contains(t, s, "(a/run.md)")
	assert.NotContains(t, s, "deploy/index.md")
}

// TestGeneratePackagesIndex_Diataxis guards HIGH-1: the components index must list
// the flat <name>.md files, not be empty (it previously only counted directories).
func TestGeneratePackagesIndex_Diataxis(t *testing.T) {
	t.Parallel()

	manifest := "properties:\n  name: tool\n  docs_layout: diataxis\n"
	g := newPromptGenerator(t, manifest, false)
	root := g.config.Path

	write := func(rel, content string) {
		require.NoError(t, g.props.FS.MkdirAll(filepath.Join(root, filepath.Dir(rel)), 0o755))
		require.NoError(t, afero.WriteFile(g.props.FS, filepath.Join(root, rel), []byte(content), 0o644))
	}
	write("docs/explanation/components/config.md", "---\ndescription: Config pkg\n---\n# config\n")
	write("docs/explanation/components/vcs/release.md", "---\ndescription: Release pkg\n---\n# release\n")

	require.NoError(t, g.generatePackagesIndex())

	idx, err := afero.ReadFile(g.props.FS, filepath.Join(root, "docs/explanation/components/index.md"))
	require.NoError(t, err)
	s := string(idx)

	assert.Contains(t, s, "(config.md)", "flat component file is listed (HIGH-1)")
	assert.Contains(t, s, "(vcs/release.md)", "nested component file is listed")
	assert.Contains(t, s, "Config pkg", "description read from frontmatter")
	assert.NotContains(t, s, "(config/)", "must not use the directory-link form")
}

func TestShouldMigrateDocsToDiataxis(t *testing.T) {
	t.Parallel()

	mk := func(manifest string, force bool) *Generator {
		g := newPromptGenerator(t, manifest, false)
		g.config.Force = force
		return g
	}

	flat := "properties:\n  name: t\n  docs_layout: flat\n"
	dia := "properties:\n  name: t\n  docs_layout: diataxis\n"

	assert.True(t, mk(flat, true).shouldMigrateDocsToDiataxis(), "--force on a flat project migrates")
	assert.False(t, mk(flat, false).shouldMigrateDocsToDiataxis(), "without --force, a flat project stays flat")
	assert.False(t, mk(dia, true).shouldMigrateDocsToDiataxis(), "already diataxis: skip migration")
}

func TestPersistModulePublished(t *testing.T) {
	t.Parallel()

	// --public-api stamps module_published so the choice survives regeneration.
	g := newPromptGenerator(t, "properties:\n  name: t\n", true)
	g.persistModulePublished()
	data, err := afero.ReadFile(g.props.FS, filepath.Join(g.config.Path, ".gtb/manifest.yaml"))
	require.NoError(t, err)
	assert.Contains(t, string(data), "module_published: true")

	// Without --public-api, the manifest is untouched.
	g2 := newPromptGenerator(t, "properties:\n  name: t\n", false)
	g2.persistModulePublished()
	data2, err := afero.ReadFile(g2.props.FS, filepath.Join(g2.config.Path, ".gtb/manifest.yaml"))
	require.NoError(t, err)
	assert.NotContains(t, string(data2), "module_published: true")
}

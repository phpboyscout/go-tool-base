package generator

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

func newDocsPathGenerator(t *testing.T, manifestYAML string) (*Generator, string) {
	t.Helper()

	const root = "/work"
	fs := afero.NewMemMapFs()
	require.NoError(t, fs.MkdirAll(filepath.Join(root, ".gtb"), 0o755))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(root, ".gtb", "manifest.yaml"), []byte(manifestYAML), 0o644))

	g := &Generator{
		props:  &props.Props{FS: fs, Logger: logger.NewNoop(), Tool: props.Tool{Name: "mytool"}},
		config: &Config{Path: root},
	}

	return g, root
}

const diataxisPathManifest = `properties:
  name: mytool
  docs_layout: diataxis
commands:
  - name: version
  - name: deploy
    commands:
      - name: start
`

func TestPrepareDocsContext_DiataxisLayout(t *testing.T) {
	t.Parallel()

	g, root := newDocsPathGenerator(t, diataxisPathManifest)

	tests := []struct {
		name      string
		cmd       string
		relPath   string
		isPackage bool
		want      string
	}{
		{name: "package -> explanation/components flat", cmd: "config", relPath: "config", isPackage: true,
			want: filepath.Join(root, "docs/explanation/components/config.md")},
		{name: "nested package keeps relpath", cmd: "release", relPath: "vcs/release", isPackage: true,
			want: filepath.Join(root, "docs/explanation/components/vcs/release.md")},
		{name: "leaf command -> reference/cli flat file", cmd: "version", relPath: "version", isPackage: false,
			want: filepath.Join(root, "docs/reference/cli/version.md")},
		{name: "parent command -> reference/cli subsection index", cmd: "deploy", relPath: "deploy", isPackage: false,
			want: filepath.Join(root, "docs/reference/cli/deploy/index.md")},
		{name: "child command -> reference/cli beside parent", cmd: "start", relPath: "deploy/start", isPackage: false,
			want: filepath.Join(root, "docs/reference/cli/deploy/start.md")},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			_, got := g.prepareDocsContext(tc.cmd, tc.relPath, tc.isPackage)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestPrepareDocsContext_FlatLayoutUnchanged(t *testing.T) {
	t.Parallel()

	// No docs_layout (or "flat") must keep the legacy paths for back-compat.
	g, root := newDocsPathGenerator(t, "properties:\n  name: mytool\ncommands:\n  - name: version\n")

	_, cmdPath := g.prepareDocsContext("version", "version", false)
	assert.Equal(t, filepath.Join(root, "docs/commands/version/index.md"), cmdPath)

	_, pkgPath := g.prepareDocsContext("config", "config", true)
	assert.Equal(t, filepath.Join(root, "docs/packages/config/index.md"), pkgPath)
}

// TestCleanupDocumentation_DiataxisNestedCommand is the 2.1.2 guard: removing a
// nested command must delete its Diátaxis reference doc (docs/reference/cli/
// <parent>/<leaf>.md), not the hardcoded legacy flat path (which leaves the
// real doc stranded).
func TestCleanupDocumentation_DiataxisNestedCommand(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	workDir := "/work"

	require.NoError(t, afero.WriteFile(fs, filepath.Join(workDir, "go.mod"), []byte("module github.com/acme/demo\n"), 0o644))
	require.NoError(t, fs.MkdirAll(filepath.Join(workDir, "pkg/cmd/parent/child"), 0o755))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(workDir, "pkg/cmd/parent/child/cmd.go"), []byte("package child\n"), 0o644))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(workDir, "pkg/cmd/parent/child/main.go"), []byte("package main\n"), 0o644))

	parentCode := `package parent
import (
	"github.com/acme/demo/pkg/cmd/parent/child"
	"github.com/spf13/cobra"
)
func NewCmdParent(props *props.Props) *cobra.Command {
	cmd := &cobra.Command{Use: "parent"}
	cmd.AddCommand(child.NewCmdChild(props))
	return cmd
}`
	require.NoError(t, afero.WriteFile(fs, filepath.Join(workDir, "pkg/cmd/parent/cmd.go"), []byte(parentCode), 0o644))

	// Diátaxis-layout project: nested command doc lives under reference/cli.
	docPath := filepath.Join(workDir, "docs/reference/cli/parent/child.md")
	require.NoError(t, afero.WriteFile(fs, docPath, []byte("# demo parent child\n"), 0o644))

	m := Manifest{
		Properties: ManifestProperties{Name: "demo", DocsLayout: DocsLayoutDiataxis},
		Commands: []ManifestCommand{
			{Name: "parent", Commands: []ManifestCommand{{Name: "child"}}},
		},
	}
	data, _ := yaml.Marshal(m)
	require.NoError(t, fs.MkdirAll(filepath.Join(workDir, ".gtb"), 0o755))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(workDir, ".gtb/manifest.yaml"), data, 0o644))

	p := &props.Props{FS: fs, Logger: logger.NewNoop(), Tool: props.Tool{Name: "demo"}}
	g := New(p, &Config{Path: workDir, Name: "child", Parent: "parent"})

	require.NoError(t, g.Remove(context.Background()))

	exists, _ := afero.Exists(fs, docPath)
	assert.False(t, exists, "nested command's Diátaxis reference doc must be removed")
}

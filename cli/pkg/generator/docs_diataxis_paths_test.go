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

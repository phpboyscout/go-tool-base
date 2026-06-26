package generator

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/pkg/config"
	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

func TestGenerateSkeleton_DiataxisDocsTree(t *testing.T) {
	t.Parallel()

	const root = "/work"
	fs := afero.NewMemMapFs()
	l := logger.NewNoop()
	p := &props.Props{FS: fs, Logger: l, Config: config.NewFilesContainer(fs, config.WithLogger(l))}

	g := New(p, &Config{})
	g.runCommand = func(_ context.Context, _, _ string, _ ...string) ([]byte, error) {
		return []byte("done"), nil
	}

	require.NoError(t, g.GenerateSkeleton(context.Background(), SkeletonConfig{
		Name:        "mytool",
		Repo:        "me/mytool",
		Host:        "github.com",
		Description: "A neutral test tool",
		Path:        root,
	}))

	// The full neutral Diátaxis tree is scaffolded.
	for _, rel := range []string{
		"docs/index.md",
		"docs/getting-started.md",
		"docs/how-to/index.md",
		"docs/reference/index.md",
		"docs/reference/cli/index.md",
		"docs/reference/config/index.md",
		"docs/explanation/index.md",
		"docs/explanation/components/index.md",
		"docs/explanation/concepts/index.md",
		"docs/tutorials/index.md",
	} {
		exists, err := afero.Exists(fs, filepath.Join(root, rel))
		require.NoError(t, err)
		assert.True(t, exists, "expected scaffolded doc: %s", rel)
	}

	// No marketing about/ section is presumed (left for the author to fill).
	aboutExists, _ := afero.Exists(fs, filepath.Join(root, "docs/about/index.md"))
	assert.False(t, aboutExists, "scaffold must not presume a marketing about/ section")

	// The landing renders the tool name and links the quadrants.
	landing, err := afero.ReadFile(fs, filepath.Join(root, "docs/index.md"))
	require.NoError(t, err)
	assert.Contains(t, string(landing), "# mytool")
	assert.Contains(t, string(landing), "Diátaxis")
	assert.Contains(t, string(landing), "how-to/index.md")

	// The manifest records the diataxis layout for the new project.
	manifest, err := afero.ReadFile(fs, filepath.Join(root, ".gtb/manifest.yaml"))
	require.NoError(t, err)
	assert.Contains(t, string(manifest), "docs_layout: diataxis")
}

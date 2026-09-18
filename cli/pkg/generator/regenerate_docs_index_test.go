package generator

import (
	"context"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const diataxisWithServe = "properties:\n  name: mytool\n  docs_layout: diataxis\n  features: []\nrelease_source:\n  type: github\n  backend: github\n  host: github.com\n  owner: acme\n  repo: mytool\nversion:\n  gtb: v1.0.0\n  go: 1.27.1\ncommands:\n  - name: serve\n    description: Run the server\n    long_description: Run the server\n"

// TestRegenerateProject_KeepsTheCommandTableInTheDiataxisIndex (F11 of the
// v0.43.0 manual round): the regenerate's skeleton pass re-rendered
// docs/reference/cli/index.md from the asset, whose command table is empty,
// and the boilerplate-only docs pass skipped a page that already existed, so
// every regenerate wiped the table. The table is refilled from the manifest
// once the skeleton pass has run.
func TestRegenerateProject_KeepsTheCommandTableInTheDiataxisIndex(t *testing.T) {
	t.Parallel()

	g, fs, _ := newPerimeterTestProject(t, diataxisWithServe)
	g.config.Overwrite = OverwriteAllow
	g.config.NoVerify = true

	require.NoError(t, g.RegenerateProject(context.Background()))

	index, err := afero.ReadFile(fs, "/work/docs/reference/cli/index.md")
	require.NoError(t, err)
	assert.Contains(t, string(index), "| [serve](serve.md) | Run the server |", "the table lists the manifest's commands after a regenerate")

	legacy, _ := afero.Exists(fs, "/work/docs/commands/serve/index.md")
	assert.False(t, legacy, "a Diátaxis project gets no legacy docs/commands tree")

	page, _ := afero.Exists(fs, "/work/docs/reference/cli/serve.md")
	assert.True(t, page, "the command's page is under reference/cli")
}

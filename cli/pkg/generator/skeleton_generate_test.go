package generator

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func renderGenerateFile(t *testing.T, disabled []string) string {
	t.Helper()

	var buf bytes.Buffer
	require.NoError(t, skeletonGenerateFile(disabled).Render(&buf))

	return buf.String()
}

// TestSkeletonGenerateFile_EmbedsDocsAndChangelog: a generated tool's docs
// command reads assets/docs, and its release pipeline runs go generate, so
// the scaffold must emit the directive that fills assets/docs as it does for
// the changelog. Without it every build of a generated tool reported "no
// embedded documentation".
func TestSkeletonGenerateFile_EmbedsDocsAndChangelog(t *testing.T) {
	t.Parallel()

	both := renderGenerateFile(t, nil)
	assert.Contains(t, both, "//go:generate go tool docs --project-root ../../.. --target-dir pkg/cmd/root/assets")
	assert.Contains(t, both, "//go:generate go tool changelog generate --output assets/CHANGELOG.md")

	noDocs := renderGenerateFile(t, []string{"docs"})
	assert.NotContains(t, noDocs, "go tool docs", "a tool without the docs feature embeds none")
	assert.Contains(t, noDocs, "go tool changelog")

	neither := renderGenerateFile(t, []string{"docs", "changelog"})
	assert.NotContains(t, neither, "go:generate")
}

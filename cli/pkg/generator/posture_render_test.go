package generator

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestGenerateSkeleton_RendersAuxiliaryCommandsAndRequireChecksum pins spec
// 0197 D4: the two settings that had no home reach the generated root, and
// the source-inspection recovery reads them back.
func TestGenerateSkeleton_RendersAuxiliaryCommandsAndRequireChecksum(t *testing.T) {
	t.Parallel()

	path := t.TempDir()
	g := newSkeletonGeneratorForTest(t, afero.NewOsFs())

	cfg := bootstrapSkeletonConfig(path, ManifestBootstrap{AuxiliaryCommands: []string{"completion", "version"}})
	cfg.Signing.RequireChecksum = true
	require.NoError(t, g.GenerateSkeleton(context.Background(), cfg))

	cmdGo, err := os.ReadFile(filepath.Join(path, "pkg", "cmd", "root", "cmd.go"))
	require.NoError(t, err)

	src := normWS(string(cmdGo))
	assert.Contains(t, src, `AuxiliaryCommands: []string{"completion", "version"}`)
	assert.Contains(t, src, "RequireChecksum: props.BoolPtr(true)")

	recovered, _, err := g.extractProjectProperties(filepath.Join(path, "pkg", "cmd", "root", "cmd.go"))
	require.NoError(t, err)
	assert.Equal(t, []string{"completion", "version"}, recovered.Bootstrap.AuxiliaryCommands)
	assert.True(t, recovered.Signing.RequireChecksum)
}

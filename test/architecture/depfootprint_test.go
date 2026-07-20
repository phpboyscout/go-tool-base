package architecture

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestViperAbsentFromDependencyGraph inverts the extraction-era footprint
// stance (config migration spec, D12): the config module used to be "the
// toolkit's Viper layer", so the graph legitimately carried the Viper stack.
// After the v0.3.x migration Viper is gone entirely, and this guard keeps it
// from returning unnoticed through a future dependency.
func TestViperAbsentFromDependencyGraph(t *testing.T) {
	t.Parallel()

	root := repoRoot(t)

	for _, file := range []string{"go.mod", "go.sum"} {
		data, err := os.ReadFile(filepath.Join(root, file))
		require.NoError(t, err)

		assert.NotContains(t, string(data), "github.com/spf13/viper",
			"%s must not carry github.com/spf13/viper — the store migration removed it from the graph", file)
	}
}

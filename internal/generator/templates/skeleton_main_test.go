package templates

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSkeletonMain_DelegatesToSignalAwareExecute verifies the scaffolded
// main.go routes execution through gtbRoot.Execute (which installs the
// signal-aware context) and documents the signal contract for tool authors.
func TestSkeletonMain_DelegatesToSignalAwareExecute(t *testing.T) {
	t.Parallel()

	f := SkeletonMain("example.com/acme/mytool")

	var buf bytes.Buffer
	require.NoError(t, f.Render(&buf))
	out := buf.String()

	assert.Contains(t, out, "gtbRoot.Execute(rootCmd, p)",
		"main must delegate to the signal-aware Execute wrapper")
	assert.Contains(t, out, "signal-aware context",
		"generated main should document the signal handling contract")
	assert.Contains(t, out, "128+signum",
		"generated main should document the signal exit-code convention")
}

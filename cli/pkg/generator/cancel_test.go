package generator

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestGeneratorOps_HonourACancelledContext pins that the three exported
// operations which take a context refuse to start once it is cancelled,
// rather than accepting the parameter and ignoring it.
func TestGeneratorOps_HonourACancelledContext(t *testing.T) {
	t.Parallel()

	g, _, _ := newPerimeterTestProject(t, issue13Manifest)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	require.ErrorIs(t, g.Remove(ctx), context.Canceled)
	require.ErrorIs(t, g.RegenerateManifest(ctx), context.Canceled)
	require.ErrorIs(t, g.SetProtection(ctx, "alpha", true), context.Canceled)
}

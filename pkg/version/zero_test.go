package version

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestInfo_IsZero pins the value-type contract: an unset Info is detectable
// without a nil check, and is a development build.
func TestInfo_IsZero(t *testing.T) {
	t.Parallel()

	var unset Info
	require.True(t, unset.IsZero())
	require.True(t, unset.IsDevelopment())

	require.False(t, NewInfo("1.2.3", "", "").IsZero())
}

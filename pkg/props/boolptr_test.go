package props_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

// TestBoolPtr: the tri-state baseline a generated root sets in a struct
// literal (spec 0197 D4) distinguishes explicitly false from unset.
func TestBoolPtr(t *testing.T) {
	t.Parallel()

	tool := props.Tool{Signing: props.SigningConfig{RequireChecksum: props.BoolPtr(false)}}
	require.NotNil(t, tool.Signing.RequireChecksum, "explicitly false is not unset")
	assert.False(t, *tool.Signing.RequireChecksum)
	assert.True(t, *props.BoolPtr(true))
}

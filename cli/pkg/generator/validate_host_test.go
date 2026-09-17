package generator

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gitlab.com/phpboyscout/go/errors"
)

// TestValidateHost_PortBounds is the 2.4.4 guard: a numeric-but-out-of-range
// port must be rejected, not merely digit-checked.
func TestValidateHost_PortBounds(t *testing.T) {
	t.Parallel()

	require.NoError(t, ValidateHost("example.com:8080"))
	require.NoError(t, ValidateHost("example.com:65535"))
	require.NoError(t, ValidateHost("example.com:1"))

	for _, bad := range []string{"example.com:99999", "example.com:0", "example.com:65536"} {
		err := ValidateHost(bad)
		require.Error(t, err, bad)
		require.ErrorIs(t, err, ErrInvalidInput, bad)
		assert.Contains(t, errors.FlattenHints(err), "1-65535", bad)
	}
}

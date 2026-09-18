package generate

import (
	"context"
	"testing"

	"charm.land/huh/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go/errorhandling"
	"gitlab.com/phpboyscout/go/errors"

	"gitlab.com/phpboyscout/go-tool-base/cli/pkg/generator"
)

// TestUsageError_CarriesTheUsageExitCode (F13 of the v0.43.0 manual round):
// the regenerate reference promises exit 2 for a usage error, but a refused
// invocation carried no code and the root exited 1. What ValidateOrPrompt
// refuses is a usage error by definition (a missing or invalid input), so
// the three commands wrap its refusal with errorhandling.ExitCodeUsage; a
// wizard the user cancelled is not a usage error and keeps its own path.
func TestUsageError_CarriesTheUsageExitCode(t *testing.T) {
	t.Parallel()

	o := &SkeletonOptions{Name: "Bad Name", Repo: "org/tool", ForgeBackend: "github"}
	refused := o.ValidateOrPrompt(context.Background(), nobodyTyping())
	require.ErrorIs(t, refused, generator.ErrInvalidInput)

	err := usageError(refused)
	assert.Equal(t, errorhandling.ExitCodeUsage, errorhandling.ExitCode(err))
	require.ErrorIs(t, err, generator.ErrInvalidInput, "the refusal is still reachable")

	missing := usageError((&AddFlagOptions{}).ValidateOrPrompt(context.Background(), nobodyTyping()))
	assert.Equal(t, errorhandling.ExitCodeUsage, errorhandling.ExitCode(missing), "no flags and no terminal is a usage error too")

	require.NoError(t, usageError(nil))
	assert.NotEqual(t, errorhandling.ExitCodeUsage, errorhandling.ExitCode(usageError(huh.ErrUserAborted)), "a cancelled wizard is not a usage error")
	assert.NotEqual(t, errorhandling.ExitCodeUsage, errorhandling.ExitCode(usageError(errors.New("disk full"))))
}

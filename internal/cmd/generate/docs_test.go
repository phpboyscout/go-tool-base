package generate

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

// TestNewCmdDocs_SourceFlagDeprecated proves the legacy --source flag is
// marked deprecated (so cobra surfaces a warning steering users to
// --command) rather than silently dead.
func TestNewCmdDocs_SourceFlagDeprecated(t *testing.T) {
	t.Parallel()

	cmd := NewCmdDocs(&props.Props{Logger: logger.NewNoop()}, &SharedFlags{})

	f := cmd.Flags().Lookup("source")
	require.NotNil(t, f, "--source flag must still exist (it is a deprecated alias for --command)")
	assert.NotEmpty(t, f.Deprecated, "--source must be MarkDeprecated, pointing at --command")
}

// TestNewCmdDocs_SourceOnlySatisfiesRequiredGroup proves a source-only
// invocation is NOT wrongly rejected by the one-required flag group:
// because Run() falls back to --source when --command is empty, --source
// must satisfy the {command|package|source} requirement.
func TestNewCmdDocs_SourceOnlySatisfiesRequiredGroup(t *testing.T) {
	t.Parallel()

	cmd := NewCmdDocs(&props.Props{Logger: logger.NewNoop()}, &SharedFlags{})

	require.NoError(t, cmd.Flags().Set("source", "./internal/cmd/mycmd"))

	// ValidateFlagGroups enforces MarkFlagsOneRequired / MutuallyExclusive.
	assert.NoError(t, cmd.ValidateFlagGroups(),
		"a source-only invocation must satisfy the one-required group")
}

// TestNewCmdDocs_NoTargetFlagsFailsRequiredGroup proves the one-required
// group still fires when none of command/package/source is supplied.
func TestNewCmdDocs_NoTargetFlagsFailsRequiredGroup(t *testing.T) {
	t.Parallel()

	cmd := NewCmdDocs(&props.Props{Logger: logger.NewNoop()}, &SharedFlags{})

	assert.Error(t, cmd.ValidateFlagGroups(),
		"with no command/package/source the one-required group must reject")
}

// TestNewCmdDocs_SourceAndPackageMutuallyExclusive guards the added
// mutual-exclusion between the deprecated --source alias and --package.
func TestNewCmdDocs_SourceAndPackageMutuallyExclusive(t *testing.T) {
	t.Parallel()

	cmd := NewCmdDocs(&props.Props{Logger: logger.NewNoop()}, &SharedFlags{})

	require.NoError(t, cmd.Flags().Set("source", "./a"))
	require.NoError(t, cmd.Flags().Set("package", "./b"))

	assert.Error(t, cmd.ValidateFlagGroups(),
		"--source and --package must be mutually exclusive (both map to a doc target)")
}

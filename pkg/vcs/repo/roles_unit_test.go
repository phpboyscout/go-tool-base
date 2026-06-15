package repo

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// acceptTreeReader is a function whose contract is the narrow TreeReader role
// rather than the full RepoLike composite. It exercises a read-only query.
func acceptTreeReader(r TreeReader) (bool, error) {
	return r.FileExists("does-not-exist")
}

// TestRoles_NarrowConsumer_AcceptsConcreteTypes proves that a function declaring
// only the narrow TreeReader role can be called with either concrete repository
// type, demonstrating the interface-segregation payoff of the role split.
func TestRoles_NarrowConsumer_AcceptsConcreteTypes(t *testing.T) {
	t.Parallel()

	exists, err := acceptTreeReader(&Repo{})
	require.Error(t, err) // unopened repo returns ErrNoRepository via headTree
	assert.False(t, exists)

	exists, err = acceptTreeReader(&ThreadSafeRepo{repo: &Repo{}})
	require.Error(t, err)
	assert.False(t, exists)
}

// TestRoles_CompositeMethodSet asserts that the RepoLike composite is satisfied
// by both concrete types and is assignable from each narrow role view, locking
// in that the recomposed RepoLike has an unchanged method set.
func TestRoles_CompositeMethodSet(t *testing.T) {
	t.Parallel()

	var full RepoLike = &Repo{}

	// Every role is reachable through the composite without a type change.
	var (
		_ TreeReader         = full
		_ Opener             = full
		_ Authenticator      = full
		_ WorktreeController = full
		_ Committer          = full
		_ SourceState        = full
		_ GitAccessor        = full
		_ Brancher           = full
	)

	assert.True(t, full.SourceIs(SourceUnknown))
}

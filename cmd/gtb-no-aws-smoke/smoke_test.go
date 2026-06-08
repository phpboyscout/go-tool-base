package main

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"gitlab.com/phpboyscout/go-tool-base/pkg/signing"

	// Mirror the production blank-imports of this binary so the test
	// observes the same registry state the running binary would.
	_ "gitlab.com/phpboyscout/go-tool-base/pkg/signing/local"
)

// TestNoAWSSmoke verifies the regulated-build slice: when the kms
// backend is not blank-imported, the registry exposes only the
// remaining backends (`local` here). This is the runtime arm of the
// Resolution 4 compile-time smoke — the compile-time arm is the
// fact that this package builds at all without the kms import.
//
// The link-time arm (asserting no github.com/aws/* symbols in the
// resulting binary) is enforced separately in CI via `go list -deps`
// over this package; running it here would couple the unit-test path
// to `go build`, which we keep out of `go test`.
func TestNoAWSSmoke(t *testing.T) {
	t.Parallel()

	names := signing.Names()
	assert.Equal(t, []string{"local"}, names,
		"with only `local` blank-imported, signing.Names() must report exactly that backend — kms must not leak in via any transitive import")
}

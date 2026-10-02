package awssecrets_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go/features"

	awssecrets "gitlab.com/phpboyscout/go-tool-base/pkg/config/sources/aws-secrets"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
)

// The blank import declares the kind and registers its factory and
// initialiser (spec 0204 D3).
func TestImportLinksTheKind(t *testing.T) {
	t.Parallel()

	set, err := features.Resolve(features.Default().Snapshot(), nil)
	require.NoError(t, err)

	kind, ok := setup.ConfigSourceKindsIn(set)[awssecrets.Kind]
	require.True(t, ok)
	assert.NotNil(t, kind.Factory)
	assert.NotNil(t, kind.Initialiser)
	assert.False(t, kind.WritableByDefault)
}

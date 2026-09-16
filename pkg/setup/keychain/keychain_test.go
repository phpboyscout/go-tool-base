package keychain_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/pkg/features"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup/keychain"
)

// TestImportDeclaresTheLinkFeature: importing this package is the whole
// activation; the feature is declared as a link kind, off by default (its
// presence, not a default, is what enables it), and the descriptor carries the
// identifiers the generator emits.
func TestImportDeclaresTheLinkFeature(t *testing.T) {
	t.Parallel()

	d, ok := features.Default().Snapshot().Lookup(keychain.KeychainFeature)
	require.True(t, ok, "the blank import must declare keychain")

	assert.Equal(t, props.KindLink, d.FeatureKind())
	assert.False(t, d.DefaultOn(), "a link's presence is its enablement; it declares no default")

	fd, isGTB := d.(props.FeatureDescriptor)
	require.True(t, isGTB)
	assert.Equal(t, "KeychainFeature", fd.ConstName)
	assert.Equal(t, keychain.PackagePath, fd.ConstPackage)
}

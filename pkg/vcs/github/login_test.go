package github

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/pkg/browser"
)

// TestNewLoginFlow_RoutesBrowseThroughPkgBrowser proves the device-flow browser
// opener goes through pkg/browser (scheme allowlist enforced) rather than
// falling back to cli/browser. pkg/browser rejects a non-allowlisted scheme
// before invoking any OS handler, so a disallowed URL surfaces its error.
func TestNewLoginFlow_RoutesBrowseThroughPkgBrowser(t *testing.T) {
	t.Parallel()

	flow, err := newLoginFlow("github.com")
	require.NoError(t, err)
	require.NotNil(t, flow.BrowseURL, "BrowseURL must be set so cli/browser is not used by default")

	err = flow.BrowseURL("ftp://attacker.example/payload")
	require.Error(t, err, "a disallowed scheme must be rejected by pkg/browser")
	assert.ErrorIs(t, err, browser.ErrDisallowedScheme)
}

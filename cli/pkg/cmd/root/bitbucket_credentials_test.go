package root

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gitlab.com/phpboyscout/go/forge"
	bitbucket "gitlab.com/phpboyscout/go/forge-bitbucket"

	"gitlab.com/phpboyscout/go-tool-base/internal/testutil"
	"gitlab.com/phpboyscout/go-tool-base/pkg/vcs"
)

// The gtb binary links forge-bitbucket, so this module is where the real
// factory can be built through forge.Lookup and the two halves seen on the
// wire; the framework module links no adapter.

// bitbucketAPI records the basic-auth pair of the first authenticated request
// and answers the downloads listing with nothing.
func bitbucketAPI(t *testing.T) (*httptest.Server, *[2]string) {
	t.Helper()

	var seen [2]string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if user, pass, ok := r.BasicAuth(); ok {
			seen = [2]string{user, pass}
		}

		_ = json.NewEncoder(w).Encode(map[string]any{"values": []any{}})
	}))
	t.Cleanup(srv.Close)

	return srv, &seen
}

func buildBitbucket(t *testing.T, yaml string) *bitbucket.BitbucketReleaseProvider {
	t.Helper()

	factory, err := forge.Lookup(forge.SourceTypeBitbucket)
	require.NoError(t, err, "gtb links forge-bitbucket")

	endpoint := forge.Endpoint{Type: forge.SourceTypeBitbucket, Host: "bitbucket.org"}
	cfg := vcs.ConfigFromReader(testutil.ViewFromYAML(t, yaml))

	provider, err := factory(context.Background(), endpoint, cfg, vcs.CredentialOptions(endpoint, cfg, "")...)
	require.NoError(t, err, "construction must not report GTB's own pointer keys as stale (#89)")

	bb, ok := provider.(*bitbucket.BitbucketReleaseProvider)
	require.True(t, ok)

	return bb
}

// TestBitbucket_ShippedDefaultsWithNothingExportedConstruct (#89): the
// bundle ships username.env and app_password.env pointing at the well-known
// variables. With nothing exported the adapter used to report both pointers
// as stale and fail construction, so a Bitbucket-sourced tool could not
// build its release client in a bare CI image.
func TestBitbucket_ShippedDefaultsWithNothingExportedConstruct(t *testing.T) {
	t.Setenv("BITBUCKET_USERNAME", "")
	t.Setenv("BITBUCKET_APP_PASSWORD", "")

	buildBitbucket(t, "bitbucket:\n  username:\n    env: BITBUCKET_USERNAME\n  app_password:\n    env: BITBUCKET_APP_PASSWORD\n")
}

// TestBitbucket_EnvReferenceModeReachesTheWire (#89): the wizard's
// recommended env-reference mode names the operator's own variables, which
// the adapter never read. Both halves now arrive as the basic-auth pair.
func TestBitbucket_EnvReferenceModeReachesTheWire(t *testing.T) {
	t.Setenv("MY_BB_USER", "alice")
	t.Setenv("MY_BB_PASS", "app-pw")
	t.Setenv("BITBUCKET_USERNAME", "")
	t.Setenv("BITBUCKET_APP_PASSWORD", "")

	srv, seen := bitbucketAPI(t)

	bb := buildBitbucket(t, "bitbucket:\n  username:\n    env: MY_BB_USER\n  app_password:\n    env: MY_BB_PASS\n")
	bb.SetAPIBase(strings.TrimSuffix(srv.URL, "/"))

	_, _ = bb.GetLatestRelease(context.Background(), "ws", "repo")

	assert.Equal(t, [2]string{"alice", "app-pw"}, *seen, "the pair GTB's pointers name is what the adapter sends")
}

package setup

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/internal/testutil"
	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

// directSource serves one asset and records the Authorization header of the
// last download, so a test can see what credential the provider put on the
// wire. `direct` is the one registered factory the framework module links
// itself (providers.go), which makes it the real factory these tests can
// build through forge.Lookup.
func directSource(t *testing.T) (*httptest.Server, *string) {
	t.Helper()

	var seen string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = r.Header.Get("Authorization")
		_, _ = io.WriteString(w, "asset-bytes")
	}))
	t.Cleanup(srv.Close)

	return srv, &seen
}

func directProps(t *testing.T, yaml string) *props.Props {
	t.Helper()

	return &props.Props{
		Logger: logger.NewNoop(),
		Config: testutil.StoreFromYAML(t, yaml),
		Tool: props.Tool{
			Name:          "tool",
			ReleaseSource: props.ReleaseSource{Type: "direct", Owner: "o", Repo: "r"},
		},
	}
}

// downloadFirstAsset drives the built release client through a release lookup
// and a download, which is the request `direct` sends its token on.
func downloadFirstAsset(t *testing.T, u *SelfUpdater) {
	t.Helper()

	rel, err := u.releaseClient.GetLatestRelease(context.Background(), "o", "r")
	require.NoError(t, err)
	require.NotEmpty(t, rel.GetAssets())

	body, _, err := u.releaseClient.DownloadReleaseAsset(context.Background(), "o", "r", rel.GetAssets()[0])
	require.NoError(t, err)
	require.NoError(t, body.Close())
}

// TestNewUpdater_HandsTheFactoryGTBsCredentialChain is #76 at the seam the
// factories use, now that the seam exists (go/forge spec 0025): the
// configuration carries auth.env, GTB's shipped default, and no auth.value.
// The factory is handed GTB's chain and consults nothing else, so construction
// succeeds and the credential the pointer names is what reaches the wire.
func TestNewUpdater_HandsTheFactoryGTBsCredentialChain(t *testing.T) {
	srv, seen := directSource(t)
	t.Setenv("MY_CI_TOKEN", "tok-from-ci")

	u, err := NewUpdater(context.Background(), directProps(t,
		"direct:\n  auth:\n    env: MY_CI_TOKEN\n  pinned_version: v1.2.3\n  url_template: "+srv.URL+"/{version}/{tool}.{ext}\n"),
		"", false)
	require.NoError(t, err)

	downloadFirstAsset(t, u)
	assert.Equal(t, "Bearer tok-from-ci", *seen, "the credential auth.env points at is the one on the wire")
}

// TestNewUpdater_TheShippedDefaultWithNoTokenIsAnAbsence: the bare CI image.
// auth.env names a variable nobody exported; that is no credential, not a
// stale configuration, and a public release source needs none.
func TestNewUpdater_TheShippedDefaultWithNoTokenIsAnAbsence(t *testing.T) {
	srv, seen := directSource(t)
	t.Setenv("DIRECT_TOKEN", "")

	u, err := NewUpdater(context.Background(), directProps(t,
		"direct:\n  auth:\n    env: DIRECT_TOKEN\n  pinned_version: v1.2.3\n  url_template: "+srv.URL+"/{version}/{tool}.{ext}\n"),
		"", false)
	require.NoError(t, err, "an unexported variable is an absence, and construction must not fail on it")

	downloadFirstAsset(t, u)
	assert.Empty(t, *seen, "nothing resolved, so nothing is sent")
}

// TestNewUpdater_ABrokenCredentialFailsConstructionWithTheReason: the view this
// replaces resolved the chain itself and turned its error into an empty string,
// so a keychain that refused built an unauthenticated client that failed later
// with a 401 nobody could explain. The factory now receives the error from the
// chain and construction fails naming it.
func TestNewUpdater_ABrokenCredentialFailsConstructionWithTheReason(t *testing.T) {
	srv, _ := directSource(t)
	t.Setenv("DIRECT_TOKEN", "")

	_, err := NewUpdater(context.Background(), directProps(t,
		"direct:\n  auth:\n    keychain: no-slash-here\n  pinned_version: v1.2.3\n  url_template: "+srv.URL+"/{version}/{tool}.{ext}\n"),
		"", false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "malformed keychain reference")
}

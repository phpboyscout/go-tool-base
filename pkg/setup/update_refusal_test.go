package setup

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go/errors"
	"gitlab.com/phpboyscout/go/forge"

	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

// Every release failure used to reach the user as "failed to get latest
// release". A rate-limited host, a lapsed token, a private repository the token
// cannot see, and a tool whose first release has not been cut have four
// different next steps, and that sentence named none of them (spec 0193 D5).
//
// What these assert is that the four stay APART. Asserting exact wording would
// pin prose that should be free to improve; asserting that "forbidden" does not
// tell the user to re-issue their token is the property that matters.

func updaterForRefusal() *SelfUpdater {
	return &SelfUpdater{
		Tool: props.Tool{
			Name: "mytool",
			ReleaseSource: props.ReleaseSource{
				Type: "github", Host: "ghe.example.com", Owner: "acme", Repo: "widget",
			},
		},
		endpoint: forge.Endpoint{Type: "github", Host: "ghe.example.com"},
	}
}

func hintOf(t *testing.T, err error) string {
	t.Helper()

	hints := errors.Hints(err)
	require.NotEmpty(t, hints, "a classified refusal must carry a hint")

	return strings.Join(hints, " ")
}

func TestExplainRefusal_SeparatesTheFourOutcomes(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name    string
		err     error
		wants   []string
		rejects []string
	}{
		{
			name:  "no release cut yet is not a misconfiguration",
			err:   forge.ErrReleaseNotFound,
			wants: []string{"acme", "widget", "no release"},
			// The whole point: a brand-new tool must not be sent to check its
			// credentials because nothing has been released.
			rejects: []string{"credential is not valid", "not found on"},
		},
		{
			name:    "repository absent points at the source, not the token",
			err:     forge.ErrNotFound,
			wants:   []string{"acme", "widget", "ghe.example.com"},
			rejects: []string{"no release"},
		},
		{
			name:  "an invalid credential points at the chain",
			err:   forge.ErrUnauthorized,
			wants: []string{"not valid", "mytool doctor"},
			// Re-issuing is the fix here, so it must not say the opposite.
			rejects: []string{"Re-issuing it will not help"},
		},
		{
			name:  "forbidden says re-issuing will not help",
			err:   forge.ErrForbidden,
			wants: []string{"valid but lacks permission", "Re-issuing it will not help"},
			// The credential works. Telling the user it is invalid sends them
			// to mint a replacement that fails identically.
			rejects: []string{"absent, expired, or revoked"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := hintOf(t, updaterForRefusal().explainRefusal(t.Context(), tc.err))

			for _, want := range tc.wants {
				assert.Contains(t, got, want)
			}

			for _, reject := range tc.rejects {
				assert.NotContains(t, got, reject)
			}
		})
	}
}

func TestExplainRefusal_RateLimitCarriesTheRetryAfter(t *testing.T) {
	t.Parallel()

	err := updaterForRefusal().explainRefusal(
		t.Context(), forge.RateLimited(errors.New("429"), 90*time.Second))

	assert.Contains(t, hintOf(t, err), "1m30s",
		"a forge that said how long to wait must have that repeated to the user")
}

func TestExplainRefusal_RateLimitWithoutARetryAfterStillExplains(t *testing.T) {
	t.Parallel()

	got := hintOf(t, updaterForRefusal().explainRefusal(t.Context(), forge.ErrRateLimited))

	assert.Contains(t, got, "did not say for how long")
	assert.Contains(t, got, "ghe.example.com")
}

// An unclassified error is returned untouched. Inventing a hint for a failure
// we have not identified is worse than the flat wrapper this replaced: it reads
// as diagnosis where there was none.
func TestExplainRefusal_LeavesAnUnclassifiedErrorAlone(t *testing.T) {
	t.Parallel()

	in := errors.New("connection reset by peer")
	out := updaterForRefusal().explainRefusal(t.Context(), in)

	require.ErrorIs(t, out, in)
	assert.Empty(t, errors.Hints(out), "no hint should be invented")
}

func TestExplainRefusal_NilIsNil(t *testing.T) {
	t.Parallel()

	assert.NoError(t, updaterForRefusal().explainRefusal(t.Context(), nil))
}

// A directly-injected provider (props.Tool.ReleaseProvider) leaves the endpoint
// zero. The message must degrade rather than name a host nobody configured.
func TestExplainRefusal_WithoutAnEndpointNamesNoHost(t *testing.T) {
	t.Parallel()

	s := &SelfUpdater{Tool: props.Tool{Name: "mytool"}}

	got := hintOf(t, s.explainRefusal(t.Context(), forge.ErrRateLimited))

	assert.Contains(t, got, "the release host")
}

package setup

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/release/static"
	"gitlab.com/phpboyscout/go-tool-base/pkg/version"
)

// TestReleaseHTTPClient_NamesTheToolAndVersion pins go-tool-base#157: a release
// store counts update checks per release, so each request says which tool and
// version made it, and nothing else.
func TestReleaseHTTPClient_NamesTheToolAndVersion(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name, tool, version, want string
	}{
		{name: "tool and version", tool: "widget", version: "v1.2.3", want: "widget/v1.2.3"},
		{name: "a build with no version", tool: "widget", want: "widget"},
		{name: "characters a header token cannot carry", tool: "widget", version: "v1.2.3 dirty\r\n", want: "widget/v1.2.3-dirty--"},
		{name: "no tool name keeps Go's default", version: "v1.2.3", want: "Go-http-client/1.1"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			url, userAgent := userAgentRecorder(t)
			p := &props.Props{Tool: props.Tool{Name: tt.tool}, Version: version.Info{Version: tt.version}}

			ch, err := static.New(url, ReleaseHTTPClient(p))
			require.NoError(t, err)

			_, err = ch.Pointer(context.Background())
			require.Error(t, err)
			assert.Equal(t, tt.want, userAgent())
		})
	}
}

// The updater's static channel is built with ReleaseHTTPClient, so a revert
// of that call site to a bare client fails here (go-tool-base#201).
func TestResolveChannel_StaticChannelSendsTheReleaseUserAgent(t *testing.T) {
	t.Parallel()

	url, userAgent := userAgentRecorder(t)
	p := &props.Props{
		Tool:    props.Tool{Name: "widget", ReleaseSource: props.ReleaseSource{Type: props.ReleaseSourceStatic, BaseURL: url}},
		Version: version.Info{Version: "v1.2.3"},
	}

	s := &SelfUpdater{}
	require.NoError(t, s.resolveChannel(context.Background(), p))

	_, err := s.channel.Latest(context.Background())
	require.Error(t, err)
	assert.Equal(t, "widget/v1.2.3", userAgent())
}

// userAgentRecorder serves 404s and returns the User-Agent of the first
// request, failing rather than hanging when none arrives.
func userAgentRecorder(t *testing.T) (string, func() string) {
	t.Helper()

	got := make(chan string, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case got <- r.UserAgent():
		default:
		}

		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)

	return srv.URL, func() string {
		t.Helper()

		select {
		case ua := <-got:
			return ua
		case <-time.After(5 * time.Second):
			t.Fatal("no request reached the release server")

			return ""
		}
	}
}

package setup

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

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

			got := make(chan string, 1)
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				got <- r.UserAgent()

				http.NotFound(w, r)
			}))
			t.Cleanup(srv.Close)

			p := &props.Props{Tool: props.Tool{Name: tt.tool}, Version: version.Info{Version: tt.version}}

			ch, err := static.New(srv.URL, ReleaseHTTPClient(p))
			require.NoError(t, err)

			_, err = ch.Pointer(context.Background())
			require.Error(t, err)
			assert.Equal(t, tt.want, <-got)
		})
	}
}

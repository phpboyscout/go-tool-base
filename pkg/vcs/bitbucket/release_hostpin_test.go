package bitbucket

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSetBasicAuthIfHostMatches exercises the host-pin helper directly,
// including the defensive branches (empty credentials, unparseable apiBase)
// that the higher-level provider tests do not reach.
func TestSetBasicAuthIfHostMatches(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		apiBase  string
		username string
		reqURL   string
		wantAuth bool
	}{
		{
			name:     "matching host attaches auth",
			apiBase:  "https://api.bitbucket.org/2.0",
			username: "u",
			reqURL:   "https://api.bitbucket.org/2.0/repositories/x/y/downloads",
			wantAuth: true,
		},
		{
			name:     "foreign host gets no auth",
			apiBase:  "https://api.bitbucket.org/2.0",
			username: "u",
			reqURL:   "https://evil.example.com/steal",
			wantAuth: false,
		},
		{
			name:     "empty username never attaches",
			apiBase:  "https://api.bitbucket.org/2.0",
			username: "",
			reqURL:   "https://api.bitbucket.org/2.0/x",
			wantAuth: false,
		},
		{
			name:     "unparseable apiBase attaches nothing",
			apiBase:  "://not-a-url",
			username: "u",
			reqURL:   "https://api.bitbucket.org/2.0/x",
			wantAuth: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			p := &BitbucketReleaseProvider{
				apiBase:     tt.apiBase,
				username:    tt.username,
				appPassword: "secret",
			}

			req, err := http.NewRequest(http.MethodGet, tt.reqURL, nil)
			require.NoError(t, err)

			p.setBasicAuthIfHostMatches(req)

			_, _, ok := req.BasicAuth()
			assert.Equal(t, tt.wantAuth, ok)
		})
	}
}

package http

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRedirectPolicy(t *testing.T) {
	t.Parallel()

	policy := redirectPolicy(3)

	t.Run("WithinLimit", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodGet, "https://example.com/2", nil)
		via := []*http.Request{
			{URL: mustParseURL("https://example.com/1")},
		}
		err := policy(req, via)
		assert.NoError(t, err)
	})

	t.Run("ExceedsLimit", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodGet, "https://example.com/4", nil)
		via := []*http.Request{
			{URL: mustParseURL("https://example.com/1")},
			{URL: mustParseURL("https://example.com/2")},
			{URL: mustParseURL("https://example.com/3")},
		}
		err := policy(req, via)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "stopped after 3 redirects")
	})

	t.Run("HTTPStoHTTP_Downgrade", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodGet, "http://example.com/downgrade", nil)
		via := []*http.Request{
			{URL: mustParseURL("https://example.com/secure")},
		}
		err := policy(req, via)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "refused redirect: HTTPS to HTTP downgrade")
	})

	t.Run("HTTPStoHTTPS_Allowed", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodGet, "https://example.com/secure2", nil)
		via := []*http.Request{
			{URL: mustParseURL("https://example.com/secure1")},
		}
		err := policy(req, via)
		assert.NoError(t, err)
	})
}

func TestNewClient_WithMaxRedirects_Zero(t *testing.T) {
	t.Parallel()

	client := NewClient(WithMaxRedirects(0))

	// len(via) >= 0 is always true, so every redirect attempt must be rejected.
	req, _ := http.NewRequest(http.MethodGet, "https://example.com/dest", nil)
	via := []*http.Request{{URL: mustParseURL("https://example.com/src")}}

	err := client.CheckRedirect(req, via)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "stopped after 0 redirects")
}

func TestNewClient_RealHTTPSRequest(t *testing.T) {
	t.Parallel()
	// Start a local TLS server
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)

	// Use server's cert for the client
	client := NewClient(WithTLSConfig(server.Client().Transport.(*http.Transport).TLSClientConfig))
	resp, err := client.Get(server.URL)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func mustParseURL(s string) *url.URL {
	u, _ := url.Parse(s)
	return u
}

// In redirectPolicy check, via[0] is the original request.
// Let's re-verify the logic in redirectPolicy.
// if len(via) > 0 && via[0].URL.Scheme == "https" && req.URL.Scheme == "http"
// This looks correct.

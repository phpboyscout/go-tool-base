package http

import (
	"crypto/x509"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go/httpclient"
)

// These tests exercise the secure client factory from
// gitlab.com/phpboyscout/go/httpclient as GTB consumes it. The factory's
// internals (redirect policy, hardened defaults) are unit-tested in the
// httpclient module.

func TestNewClient_WithMaxRedirects_Zero(t *testing.T) {
	t.Parallel()

	client := httpclient.NewClient(httpclient.WithMaxRedirects(0))

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
	client := httpclient.NewClient(httpclient.WithTLSConfig(server.Client().Transport.(*http.Transport).TLSClientConfig))
	resp, err := client.Get(server.URL)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestNewClient_WithCertPool_RealHTTPSRequest(t *testing.T) {
	t.Parallel()

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)

	// Trust the test server's self-signed cert via a custom pool — the
	// private-CA scenario the option exists for.
	pool := x509.NewCertPool()
	pool.AddCert(server.Certificate())

	client := httpclient.NewClient(httpclient.WithCertPool(pool))
	resp, err := client.Get(server.URL)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func mustParseURL(s string) *url.URL {
	u, _ := url.Parse(s)
	return u
}

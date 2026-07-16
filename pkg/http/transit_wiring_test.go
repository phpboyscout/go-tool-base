package http

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
)

// These tests exercise GTB's own wiring of the transit middleware into the
// secure client constructor — the seam this package owns. The middleware
// behaviour itself is tested in gitlab.com/phpboyscout/go/transit.

// countingHandler returns an http.Handler that responds with the given status
// codes in sequence, then 200 for all subsequent requests.
func countingHandler(t *testing.T, statusCodes ...int) http.Handler {
	t.Helper()

	var count atomic.Int64

	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		idx := int(count.Add(1)) - 1
		if idx < len(statusCodes) {
			w.WriteHeader(statusCodes[idx])
		} else {
			w.WriteHeader(http.StatusOK)
		}
	})
}

// TestWithRetry_Integration verifies NewClient(WithRetry(...)) installs the
// transit retry transport and a transient sequence is retried to success.
func TestWithRetry_Integration(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(countingHandler(t,
		http.StatusBadGateway,
		http.StatusServiceUnavailable,
	))
	t.Cleanup(srv.Close)

	client := NewClient(
		WithTransport(srv.Client().Transport),
		WithRetry(RetryConfig{
			MaxRetries:           3,
			InitialBackoff:       1 * time.Millisecond,
			MaxBackoff:           10 * time.Millisecond,
			RetryableStatusCodes: []int{502, 503},
		}),
	)

	resp, err := client.Get(srv.URL)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

// TestWithClientMiddleware_Integration verifies NewClient(WithClientMiddleware(...))
// applies a transit ClientChain to the transport.
func TestWithClientMiddleware_Integration(t *testing.T) {
	t.Parallel()

	var receivedAuth string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	chain := NewClientChain(
		WithRequestLogging(logger.ToSlog(logger.NewNoop())),
		WithBearerToken("integration-test"),
	)

	client := NewClient(
		WithTimeout(5*time.Second),
		WithClientMiddleware(chain),
	)

	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, srv.URL, nil)
	resp, err := client.Do(req)
	require.NoError(t, err)

	defer func() { _ = resp.Body.Close() }()

	assert.Equal(t, "Bearer integration-test", receivedAuth)
}

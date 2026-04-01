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

	"github.com/phpboyscout/go-tool-base/pkg/logger"
)

func TestClientChain_Then(t *testing.T) {
	t.Parallel()

	var order []string

	mw1 := func(next http.RoundTripper) http.RoundTripper {
		return roundTripFunc(func(req *http.Request) (*http.Response, error) {
			order = append(order, "mw1-before")
			resp, err := next.RoundTrip(req)
			order = append(order, "mw1-after")

			return resp, err
		})
	}

	mw2 := func(next http.RoundTripper) http.RoundTripper {
		return roundTripFunc(func(req *http.Request) (*http.Response, error) {
			order = append(order, "mw2-before")
			resp, err := next.RoundTrip(req)
			order = append(order, "mw2-after")

			return resp, err
		})
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	chain := NewClientChain(mw1, mw2)
	transport := chain.Then(http.DefaultTransport)

	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, srv.URL, nil)
	_, err := transport.RoundTrip(req)
	require.NoError(t, err)

	assert.Equal(t, []string{"mw1-before", "mw2-before", "mw2-after", "mw1-after"}, order)
}

func TestClientChain_Append(t *testing.T) {
	t.Parallel()

	chain1 := NewClientChain()
	chain2 := chain1.Append(WithBearerToken("test"))

	// Original chain should be unmodified (immutable)
	assert.Empty(t, chain1.middlewares)
	assert.Len(t, chain2.middlewares, 1)
}

func TestWithRequestLogging(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	buf := logger.NewBuffer()

	chain := NewClientChain(WithRequestLogging(buf))
	client := &http.Client{Transport: chain.Then(http.DefaultTransport)}

	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, srv.URL+"/test", nil)
	resp, err := client.Do(req)
	require.NoError(t, err)

	defer resp.Body.Close()

	assert.True(t, buf.Contains("HTTP request completed"))
}

func TestWithBearerToken(t *testing.T) {
	t.Parallel()

	var receivedAuth string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	chain := NewClientChain(WithBearerToken("my-secret-token"))
	client := &http.Client{Transport: chain.Then(http.DefaultTransport)}

	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, srv.URL, nil)
	resp, err := client.Do(req)
	require.NoError(t, err)

	defer resp.Body.Close()

	assert.Equal(t, "Bearer my-secret-token", receivedAuth)
}

func TestWithBasicAuth(t *testing.T) {
	t.Parallel()

	var receivedAuth string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	chain := NewClientChain(WithBasicAuth("user", "pass"))
	client := &http.Client{Transport: chain.Then(http.DefaultTransport)}

	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, srv.URL, nil)
	resp, err := client.Do(req)
	require.NoError(t, err)

	defer resp.Body.Close()

	assert.Equal(t, "Basic dXNlcjpwYXNz", receivedAuth)
}

func TestWithRateLimit(t *testing.T) {
	t.Parallel()

	var requestCount atomic.Int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requestCount.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	// 5 requests per second = 200ms between requests
	chain := NewClientChain(WithRateLimit(5))
	client := &http.Client{Transport: chain.Then(http.DefaultTransport)}

	start := time.Now()

	for range 3 {
		req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, srv.URL, nil)
		resp, err := client.Do(req)
		require.NoError(t, err)

		resp.Body.Close()
	}

	elapsed := time.Since(start)

	assert.Equal(t, int32(3), requestCount.Load())
	// 3 requests at 5/s should take at least 400ms (2 intervals)
	assert.GreaterOrEqual(t, elapsed, 350*time.Millisecond, "rate limiting should enforce delays")
}

func TestWithRateLimit_ContextCancellation(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	chain := NewClientChain(WithRateLimit(1)) // 1 req/s = 1s between requests
	client := &http.Client{Transport: chain.Then(http.DefaultTransport)}

	// First request goes through immediately
	req1, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, srv.URL, nil)
	resp1, err := client.Do(req1)
	require.NoError(t, err)

	resp1.Body.Close()

	// Second request should be rate-limited; cancel before it completes
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	req2, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL, nil)
	_, err = client.Do(req2)

	assert.Error(t, err, "should fail due to context cancellation during rate limit wait")
}

func TestWithClientMiddleware_Integration(t *testing.T) {
	t.Parallel()

	var receivedAuth string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	chain := NewClientChain(
		WithRequestLogging(logger.NewNoop()),
		WithBearerToken("integration-test"),
	)

	client := NewClient(
		WithTimeout(5*time.Second),
		WithClientMiddleware(chain),
	)

	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, srv.URL, nil)
	resp, err := client.Do(req)
	require.NoError(t, err)

	defer resp.Body.Close()

	assert.Equal(t, "Bearer integration-test", receivedAuth)
}

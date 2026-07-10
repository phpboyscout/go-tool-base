package http

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
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
	resp, err := transport.RoundTrip(req)
	require.NoError(t, err)

	defer func() { _ = resp.Body.Close() }()

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

	chain := NewClientChain(WithRequestLogging(logger.ToSlog(buf)))
	client := &http.Client{Transport: chain.Then(http.DefaultTransport)}

	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, srv.URL+"/test", nil)
	resp, err := client.Do(req)
	require.NoError(t, err)

	defer func() { _ = resp.Body.Close() }()

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

	defer func() { _ = resp.Body.Close() }()

	assert.Equal(t, "Bearer my-secret-token", receivedAuth)
}

// TestWithBearerToken_NotLeakedOnCrossHostRedirect proves the Authorization
// header is not re-injected when a request is redirected to a different host.
// Because the middleware is a RoundTripper, net/http's cross-host stripping
// (which only governs initial-request headers) does not protect it.
func TestWithBearerToken_NotLeakedOnCrossHostRedirect(t *testing.T) {
	t.Parallel()

	var foreignAuth string

	foreign := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		foreignAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	defer foreign.Close()

	var originAuth string

	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/start" {
			originAuth = r.Header.Get("Authorization")
			http.Redirect(w, r, foreign.URL+"/landing", http.StatusFound)

			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer origin.Close()

	chain := NewClientChain(WithBearerToken("my-secret-token"))
	client := &http.Client{Transport: chain.Then(http.DefaultTransport)}

	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, origin.URL+"/start", nil)
	resp, err := client.Do(req)
	require.NoError(t, err)

	defer func() { _ = resp.Body.Close() }()

	assert.Equal(t, "Bearer my-secret-token", originAuth, "origin host should receive the token")
	assert.Empty(t, foreignAuth, "token must not be sent to the redirect target host")
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

	defer func() { _ = resp.Body.Close() }()

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

		_ = resp.Body.Close()
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

	func() { _ = resp1.Body.Close() }()

	// Second request should be rate-limited; cancel before it completes
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	req2, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL, nil)
	resp2, err := client.Do(req2)

	if resp2 != nil {
		defer func() { _ = resp2.Body.Close() }()
	}

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

// TestWithRateLimit_SerialisesUnderConcurrency proves the rate limiter spaces
// out concurrent requests rather than admitting a burst all at once. The prior
// implementation let N goroutines sleep in parallel and then proceed together,
// admitting N requests per interval instead of one.
func TestWithRateLimit_SerialisesUnderConcurrency(t *testing.T) {
	t.Parallel()

	var passed atomic.Int32

	inner := roundTripFunc(func(_ *http.Request) (*http.Response, error) {
		passed.Add(1)

		return &http.Response{StatusCode: http.StatusOK, Body: http.NoBody}, nil
	})

	const rps = 50 // 20ms between requests
	rt := WithRateLimit(rps)(inner)

	const n = 4

	var wg sync.WaitGroup

	start := time.Now()

	for range n {
		wg.Add(1)

		go func() {
			defer wg.Done()

			req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://example.test", nil)
			resp, err := rt.RoundTrip(req)
			if err == nil {
				_ = resp.Body.Close()
			}
		}()
	}

	wg.Wait()

	elapsed := time.Since(start)

	assert.Equal(t, int32(n), passed.Load())
	// n requests at 50 rps must take at least ~3 intervals (60ms). A buggy
	// limiter admitting the whole burst finishes in ~one interval (≤20ms).
	assert.GreaterOrEqual(t, elapsed, 40*time.Millisecond,
		"concurrent requests must be spaced by the rate limiter, not admitted as a burst")
}

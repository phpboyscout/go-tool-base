package http

import (
	"bytes"
	"context"
	"io"
	gonet "net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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

func TestDefaultRetryConfig(t *testing.T) {
	t.Parallel()

	cfg := DefaultRetryConfig()
	assert.Equal(t, 3, cfg.MaxRetries)
	assert.Equal(t, 500*time.Millisecond, cfg.InitialBackoff)
	assert.Equal(t, 30*time.Second, cfg.MaxBackoff)
	assert.Equal(t, []int{429, 502, 503, 504}, cfg.RetryableStatusCodes)
	assert.Nil(t, cfg.ShouldRetry)
}

func TestRetryTransport_NoRetryOnSuccess(t *testing.T) {
	t.Parallel()

	var calls atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	rt := &retryTransport{
		next: srv.Client().Transport,
		cfg:  DefaultRetryConfig(),
	}

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, srv.URL, nil)
	require.NoError(t, err)

	resp, err := rt.RoundTrip(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, int64(1), calls.Load())
}

func TestRetryTransport_RetriesOn503(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(countingHandler(t, http.StatusServiceUnavailable))
	t.Cleanup(srv.Close)

	rt := &retryTransport{
		next: srv.Client().Transport,
		cfg: RetryConfig{
			MaxRetries:           3,
			InitialBackoff:       1 * time.Millisecond,
			MaxBackoff:           10 * time.Millisecond,
			RetryableStatusCodes: []int{503},
		},
	}

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, srv.URL, nil)
	require.NoError(t, err)

	resp, err := rt.RoundTrip(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestRetryTransport_RetriesOn429WithRetryAfter(t *testing.T) {
	t.Parallel()

	var calls atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) == 1 {
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusTooManyRequests)

			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	rt := &retryTransport{
		next: srv.Client().Transport,
		cfg: RetryConfig{
			MaxRetries:           3,
			InitialBackoff:       1 * time.Millisecond,
			MaxBackoff:           5 * time.Second,
			RetryableStatusCodes: []int{429},
		},
	}

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, srv.URL, nil)
	require.NoError(t, err)

	start := time.Now()

	resp, err := rt.RoundTrip(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	elapsed := time.Since(start)

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.GreaterOrEqual(t, elapsed, 900*time.Millisecond, "should have waited ~1s for Retry-After")
}

func TestRetryTransport_ExhaustsMaxRetries(t *testing.T) {
	t.Parallel()

	var calls atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	t.Cleanup(srv.Close)

	rt := &retryTransport{
		next: srv.Client().Transport,
		cfg: RetryConfig{
			MaxRetries:           2,
			InitialBackoff:       1 * time.Millisecond,
			MaxBackoff:           10 * time.Millisecond,
			RetryableStatusCodes: []int{503},
		},
	}

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, srv.URL, nil)
	require.NoError(t, err)

	resp, err := rt.RoundTrip(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	assert.Equal(t, http.StatusServiceUnavailable, resp.StatusCode)
	assert.Equal(t, int64(3), calls.Load(), "should be 1 initial + 2 retries")
}

func TestRetryTransport_ContextCancelled(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	t.Cleanup(srv.Close)

	rt := &retryTransport{
		next: srv.Client().Transport,
		cfg: RetryConfig{
			MaxRetries:           5,
			InitialBackoff:       10 * time.Second,
			MaxBackoff:           30 * time.Second,
			RetryableStatusCodes: []int{503},
		},
	}

	ctx, cancel := context.WithCancel(context.Background())

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL, nil)
	require.NoError(t, err)

	// Cancel context shortly after first attempt
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	resp, err := rt.RoundTrip(req)
	if resp != nil {
		_ = resp.Body.Close()
	}

	require.Error(t, err)
	assert.True(t, errors.Is(err, context.Canceled))
}

func TestRetryTransport_NetworkError(t *testing.T) {
	t.Parallel()

	var calls atomic.Int64
	transport := roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		if calls.Add(1) == 1 {
			return nil, &gonet.OpError{Op: "dial", Err: errors.New("connection refused")}
		}

		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(bytes.NewReader(nil)),
		}, nil
	})

	rt := &retryTransport{
		next: transport,
		cfg: RetryConfig{
			MaxRetries:           3,
			InitialBackoff:       1 * time.Millisecond,
			MaxBackoff:           10 * time.Millisecond,
			RetryableStatusCodes: []int{503},
		},
	}

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://localhost:0/test", nil)
	require.NoError(t, err)

	resp, err := rt.RoundTrip(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, int64(2), calls.Load())
}

func TestRetryTransport_BodyRewind(t *testing.T) {
	t.Parallel()

	var bodies []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		bodies = append(bodies, string(body))

		if len(bodies) == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)

			return
		}

		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	rt := &retryTransport{
		next: srv.Client().Transport,
		cfg: RetryConfig{
			MaxRetries:           3,
			InitialBackoff:       1 * time.Millisecond,
			MaxBackoff:           10 * time.Millisecond,
			RetryableStatusCodes: []int{503},
		},
	}

	payload := "request body content"
	req, err := http.NewRequestWithContext(
		context.Background(), http.MethodPost, srv.URL,
		bytes.NewReader([]byte(payload)),
	)
	require.NoError(t, err)

	resp, err := rt.RoundTrip(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	require.Len(t, bodies, 2)
	assert.Equal(t, payload, bodies[0], "first attempt should have original body")
	assert.Equal(t, payload, bodies[1], "retry should have rewound body")
}

func TestRetryTransport_NoRetryOn4xx(t *testing.T) {
	t.Parallel()

	codes := []int{
		http.StatusBadRequest,
		http.StatusUnauthorized,
		http.StatusForbidden,
		http.StatusNotFound,
	}

	for _, code := range codes {
		t.Run(http.StatusText(code), func(t *testing.T) {
			t.Parallel()

			var calls atomic.Int64
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				calls.Add(1)
				w.WriteHeader(code)
			}))
			t.Cleanup(srv.Close)

			rt := &retryTransport{
				next: srv.Client().Transport,
				cfg: RetryConfig{
					MaxRetries:           3,
					InitialBackoff:       1 * time.Millisecond,
					MaxBackoff:           10 * time.Millisecond,
					RetryableStatusCodes: []int{429, 502, 503, 504},
				},
			}

			req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, srv.URL, nil)
			require.NoError(t, err)

			resp, err := rt.RoundTrip(req)
			require.NoError(t, err)
			defer func() { _ = resp.Body.Close() }()

			assert.Equal(t, code, resp.StatusCode)
			assert.Equal(t, int64(1), calls.Load(), "should not retry %d", code)
		})
	}
}

func TestRetryTransport_CustomShouldRetry(t *testing.T) {
	t.Parallel()

	var calls atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) == 1 {
			w.WriteHeader(http.StatusConflict) // 409 — not in default retryable set
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	rt := &retryTransport{
		next: srv.Client().Transport,
		cfg: RetryConfig{
			MaxRetries:     3,
			InitialBackoff: 1 * time.Millisecond,
			MaxBackoff:     10 * time.Millisecond,
			ShouldRetry: func(_ int, resp *http.Response, _ error) bool {
				return resp != nil && resp.StatusCode == http.StatusConflict
			},
		},
	}

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, srv.URL, nil)
	require.NoError(t, err)

	resp, err := rt.RoundTrip(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, int64(2), calls.Load())
}

func TestRetryTransport_BackoffJitter(t *testing.T) {
	t.Parallel()

	rt := &retryTransport{
		cfg: RetryConfig{
			InitialBackoff: 100 * time.Millisecond,
			MaxBackoff:     10 * time.Second,
		},
	}

	for attempt := 1; attempt <= 5; attempt++ {
		maxExpected := min(rt.cfg.InitialBackoff*(1<<uint(attempt-1)), rt.cfg.MaxBackoff)

		for range 20 {
			delay := rt.computeDelay(attempt, nil)
			assert.GreaterOrEqual(t, delay, time.Duration(0), "delay must be non-negative")
			assert.LessOrEqual(t, delay, maxExpected, "delay must not exceed computed backoff for attempt %d", attempt)
		}
	}
}

func TestRetryTransport_MaxBackoffCap(t *testing.T) {
	t.Parallel()

	rt := &retryTransport{
		cfg: RetryConfig{
			InitialBackoff: 1 * time.Second,
			MaxBackoff:     5 * time.Second,
		},
	}

	// High attempt count — should not exceed MaxBackoff
	for range 50 {
		delay := rt.computeDelay(20, nil)
		assert.LessOrEqual(t, delay, 5*time.Second, "delay must be capped at MaxBackoff")
	}
}

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

func TestParseRetryAfter(t *testing.T) {
	t.Parallel()

	t.Run("Seconds", func(t *testing.T) {
		t.Parallel()
		d := parseRetryAfter("5")
		assert.Equal(t, 5*time.Second, d)
	})

	t.Run("Empty", func(t *testing.T) {
		t.Parallel()
		d := parseRetryAfter("")
		assert.Equal(t, time.Duration(0), d)
	})

	t.Run("Invalid", func(t *testing.T) {
		t.Parallel()
		d := parseRetryAfter("not-a-number")
		assert.Equal(t, time.Duration(0), d)
	})
}

// roundTripperFunc adapts a function to the http.RoundTripper interface.
type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

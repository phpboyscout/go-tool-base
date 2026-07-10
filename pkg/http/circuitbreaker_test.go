package http

import (
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
)

// countingTransport returns a fixed response/error and counts invocations.
type countingTransport struct {
	calls  atomic.Int64
	status int
	err    error
}

func (c *countingTransport) RoundTrip(*http.Request) (*http.Response, error) {
	c.calls.Add(1)
	if c.err != nil {
		return nil, c.err
	}

	return &http.Response{StatusCode: c.status, Body: http.NoBody, Header: make(http.Header)}, nil
}

func newReq() *http.Request {
	req, _ := http.NewRequest(http.MethodGet, "http://example.test/", nil)

	return req
}

// roundtrip performs one request, closes the response body, and returns the
// status code (-1 when the breaker rejected the call with no response) and the
// error. Returning a status int rather than the *http.Response keeps the body
// fully owned here, so callers need not (and cannot) leak it.
func roundtrip(rt http.RoundTripper) (int, error) {
	resp, err := rt.RoundTrip(newReq())

	status := -1
	if resp != nil {
		status = resp.StatusCode
		_ = resp.Body.Close()
	}

	return status, err
}

func TestCircuitState_String(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "closed", StateClosed.String())
	assert.Equal(t, "open", StateOpen.String())
	assert.Equal(t, "half-open", StateHalfOpen.String())
}

func TestDefaultCircuitBreakerConfig(t *testing.T) {
	t.Parallel()

	cfg := DefaultCircuitBreakerConfig()

	assert.Equal(t, 5, cfg.FailureThreshold)
	assert.Equal(t, 30*time.Second, cfg.Cooldown)
	assert.Equal(t, 1, cfg.HalfOpenMaxRequests)
	assert.Nil(t, cfg.IsFailure)
}

func TestDefaultHTTPIsFailure(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		resp *http.Response
		err  error
		want bool
	}{
		{name: "transport error", err: errors.New("dial fail"), want: true},
		{name: "500 fails", resp: &http.Response{StatusCode: 500}, want: true},
		{name: "503 fails", resp: &http.Response{StatusCode: 503}, want: true},
		{name: "200 ok", resp: &http.Response{StatusCode: 200}, want: false},
		{name: "404 not a breaker failure", resp: &http.Response{StatusCode: 404}, want: false},
		{name: "429 not a breaker failure", resp: &http.Response{StatusCode: 429}, want: false},
		{name: "nil resp no err", resp: nil, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, defaultHTTPIsFailure(tt.resp, tt.err))
		})
	}
}

func TestBreaker_OpensAtThresholdAndRejectsFast(t *testing.T) {
	t.Parallel()

	base := &countingTransport{status: http.StatusInternalServerError}
	cfg := CircuitBreakerConfig{FailureThreshold: 3, Cooldown: time.Minute}
	rt := WithCircuitBreaker(logger.ToSlog(logger.NewNoop()), cfg)(base)

	// Three 5xx responses trip the breaker.
	for range 3 {
		status, err := roundtrip(rt)
		require.NoError(t, err)
		require.Equal(t, http.StatusInternalServerError, status)
	}

	callsBefore := base.calls.Load()

	// Next call is rejected fast with ErrCircuitOpen and never reaches base.
	_, err := roundtrip(rt)
	assert.True(t, errors.Is(err, ErrCircuitOpen), "open breaker returns ErrCircuitOpen")
	assert.Equal(t, callsBefore, base.calls.Load(), "rejected call must not invoke next")
}

func TestBreaker_4xxDoesNotTrip(t *testing.T) {
	t.Parallel()

	base := &countingTransport{status: http.StatusTooManyRequests} // 429
	cfg := CircuitBreakerConfig{FailureThreshold: 2, Cooldown: time.Minute}
	rt := WithCircuitBreaker(logger.ToSlog(logger.NewNoop()), cfg)(base)

	for range 5 {
		status, err := roundtrip(rt)
		require.NoError(t, err)
		require.Equal(t, http.StatusTooManyRequests, status)
	}

	// Still closed: all five reached base.
	assert.Equal(t, int64(5), base.calls.Load())
}

func TestBreaker_HalfOpenRecovery(t *testing.T) {
	t.Parallel()

	var clock atomicClock
	clock.set(time.Date(2026, 6, 21, 0, 0, 0, 0, time.UTC))

	base := &countingTransport{status: http.StatusInternalServerError}
	cfg := CircuitBreakerConfig{FailureThreshold: 1, Cooldown: 30 * time.Second}
	rt := newCircuitBreakerMiddleware(logger.ToSlog(logger.NewNoop()), cfg, clock.now)(base)

	// Trip it.
	_, _ = roundtrip(rt)

	// Before cooldown: rejected.
	_, err := roundtrip(rt)
	require.True(t, errors.Is(err, ErrCircuitOpen))

	// After cooldown, downstream is healthy now -> trial succeeds -> closed.
	clock.add(31 * time.Second)
	base.status = http.StatusOK

	status, err := roundtrip(rt)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, status)

	// Breaker closed: a subsequent call also reaches base.
	_, err = roundtrip(rt)
	require.NoError(t, err)
}

func TestBreaker_HalfOpenFailureReopens(t *testing.T) {
	t.Parallel()

	var clock atomicClock
	clock.set(time.Date(2026, 6, 21, 0, 0, 0, 0, time.UTC))

	base := &countingTransport{status: http.StatusInternalServerError}
	cfg := CircuitBreakerConfig{FailureThreshold: 1, Cooldown: 30 * time.Second}
	rt := newCircuitBreakerMiddleware(logger.ToSlog(logger.NewNoop()), cfg, clock.now)(base)

	_, _ = roundtrip(rt) // trip
	clock.add(31 * time.Second)

	// Trial still fails (downstream still 5xx) -> re-open.
	_, _ = roundtrip(rt)

	callsBefore := base.calls.Load()
	_, err := roundtrip(rt)
	require.True(t, errors.Is(err, ErrCircuitOpen), "re-opened after failed trial")
	assert.Equal(t, callsBefore, base.calls.Load())
}

func TestBreaker_ComposesWithRetry_OneCallOneFailure(t *testing.T) {
	t.Parallel()

	// breaker wraps retry wraps base. A single logical call exhausts the retry
	// budget against a dead base, but must count as exactly ONE breaker failure.
	base := &countingTransport{status: http.StatusServiceUnavailable} // 503 is retryable
	retry := &retryTransport{
		next: base,
		cfg: RetryConfig{
			MaxRetries:           2,
			InitialBackoff:       time.Millisecond,
			MaxBackoff:           2 * time.Millisecond,
			RetryableStatusCodes: []int{http.StatusServiceUnavailable},
		},
	}

	cfg := CircuitBreakerConfig{FailureThreshold: 2, Cooldown: time.Minute}
	rt := WithCircuitBreaker(logger.ToSlog(logger.NewNoop()), cfg)(retry)

	// First logical call: 1 + 2 retries = 3 base hits, counts as 1 breaker failure.
	_, _ = roundtrip(rt)
	assert.Equal(t, int64(3), base.calls.Load())

	// Second logical call: another 3 base hits, second breaker failure -> opens.
	_, _ = roundtrip(rt)
	assert.Equal(t, int64(6), base.calls.Load())

	// Now open: third call rejected before reaching retry/base.
	_, err := roundtrip(rt)
	require.True(t, errors.Is(err, ErrCircuitOpen))
	assert.Equal(t, int64(6), base.calls.Load(), "open breaker spends no retry budget")
}

func TestBreaker_OnStateChangeFires(t *testing.T) {
	t.Parallel()

	var (
		transitions []string
		mu          sync.Mutex
	)

	cfg := CircuitBreakerConfig{
		FailureThreshold: 1,
		Cooldown:         time.Minute,
		OnStateChange: func(from, to CircuitState) {
			mu.Lock()
			defer mu.Unlock()
			transitions = append(transitions, from.String()+"->"+to.String())
		},
	}
	base := &countingTransport{status: http.StatusInternalServerError}
	rt := WithCircuitBreaker(logger.ToSlog(logger.NewNoop()), cfg)(base)

	_, _ = roundtrip(rt) // closed -> open

	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, []string{"closed->open"}, transitions)
}

func TestBreaker_ConcurrentRace(t *testing.T) {
	t.Parallel()

	base := &countingTransport{status: http.StatusInternalServerError}
	cfg := CircuitBreakerConfig{FailureThreshold: 3, Cooldown: time.Millisecond}
	rt := WithCircuitBreaker(logger.ToSlog(logger.NewNoop()), cfg)(base)

	var wg sync.WaitGroup
	for range 200 {
		wg.Add(1)

		go func() {
			defer wg.Done()

			_, _ = roundtrip(rt)
		}()
	}

	wg.Wait()
}

// atomicClock is a goroutine-safe manually-advanced clock for tests.
type atomicClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *atomicClock) set(t time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = t
}

func (c *atomicClock) add(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

func (c *atomicClock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.t
}

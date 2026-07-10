package http

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
)

// okHandler is a trivial 200 handler used as the limiter's next.
func okHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
}

func TestDefaultRateLimitConfig(t *testing.T) {
	t.Parallel()

	cfg := DefaultRateLimitConfig()

	assert.InDelta(t, float64(defaultRateLimitRPS), cfg.RequestsPerSecond, 0)
	assert.Equal(t, defaultRateLimitBurst, cfg.Burst)
	assert.Equal(t, defaultMaxTrackedKeys, cfg.MaxTrackedKeys)
	assert.Nil(t, cfg.KeyFunc)
}

func TestRateLimitConfig_Normalized(t *testing.T) {
	t.Parallel()

	got := RateLimitConfig{RequestsPerSecond: 0, Burst: 0, MaxTrackedKeys: 0}.normalized()

	assert.InDelta(t, float64(defaultRateLimitRPS), got.RequestsPerSecond, 0)
	assert.Equal(t, defaultRateLimitBurst, got.Burst)
	assert.Equal(t, defaultMaxTrackedKeys, got.MaxTrackedKeys)

	// Negative values are clamped too.
	got = RateLimitConfig{RequestsPerSecond: -1, Burst: -5, MaxTrackedKeys: -1}.normalized()
	assert.InDelta(t, float64(defaultRateLimitRPS), got.RequestsPerSecond, 0)
	assert.Equal(t, defaultRateLimitBurst, got.Burst)
	assert.Equal(t, defaultMaxTrackedKeys, got.MaxTrackedKeys)
}

func TestRateLimit_AdmitsUnderRate(t *testing.T) {
	t.Parallel()

	// Burst of 5 admits 5 instantaneous requests at a low fill rate.
	mw := RateLimitMiddleware(logger.ToSlog(logger.NewNoop()), RateLimitConfig{RequestsPerSecond: 1, Burst: 5})
	handler := mw(okHandler())

	for i := range 5 {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
		require.Equalf(t, http.StatusOK, rec.Code, "request %d should be admitted within burst", i)
	}
}

func TestRateLimit_Rejects429(t *testing.T) {
	t.Parallel()

	// Burst of 1: the first request passes, the second is rejected.
	mw := RateLimitMiddleware(logger.ToSlog(logger.NewNoop()), RateLimitConfig{RequestsPerSecond: 1, Burst: 1})
	handler := mw(okHandler())

	first := httptest.NewRecorder()
	handler.ServeHTTP(first, httptest.NewRequest(http.MethodGet, "/", nil))
	require.Equal(t, http.StatusOK, first.Code)

	second := httptest.NewRecorder()
	handler.ServeHTTP(second, httptest.NewRequest(http.MethodGet, "/", nil))

	assert.Equal(t, http.StatusTooManyRequests, second.Code)
	assert.Equal(t, "1", second.Header().Get("Retry-After"))
}

func TestRateLimit_OnLimitedCallback(t *testing.T) {
	t.Parallel()

	var called atomic.Int32

	cfg := RateLimitConfig{
		RequestsPerSecond: 1,
		Burst:             1,
		OnLimited:         func(*http.Request) { called.Add(1) },
	}
	handler := RateLimitMiddleware(logger.ToSlog(logger.NewNoop()), cfg)(okHandler())

	for range 2 {
		handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
	}

	assert.Equal(t, int32(1), called.Load(), "OnLimited fires once for the single rejection")
}

func TestRateLimit_PerClientKey(t *testing.T) {
	t.Parallel()

	// Burst 1 per key: each distinct client IP gets its own bucket, so two
	// different IPs each get one admitted request.
	cfg := RateLimitConfig{RequestsPerSecond: 1, Burst: 1, KeyFunc: ClientIPKey}
	handler := RateLimitMiddleware(logger.ToSlog(logger.NewNoop()), cfg)(okHandler())

	serve := func(remoteAddr string) int {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.RemoteAddr = remoteAddr
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		return rec.Code
	}

	assert.Equal(t, http.StatusOK, serve("10.0.0.1:1111"), "first IP admitted")
	assert.Equal(t, http.StatusOK, serve("10.0.0.2:2222"), "distinct IP has its own bucket")
	assert.Equal(t, http.StatusTooManyRequests, serve("10.0.0.1:3333"), "first IP exhausted its bucket")
}

func TestRateLimit_GlobalNilKeyFunc(t *testing.T) {
	t.Parallel()

	// With KeyFunc nil, all clients share one bucket regardless of source IP.
	cfg := RateLimitConfig{RequestsPerSecond: 1, Burst: 1}
	handler := RateLimitMiddleware(logger.ToSlog(logger.NewNoop()), cfg)(okHandler())

	req1 := httptest.NewRequest(http.MethodGet, "/", nil)
	req1.RemoteAddr = "10.0.0.1:1111"
	rec1 := httptest.NewRecorder()
	handler.ServeHTTP(rec1, req1)
	require.Equal(t, http.StatusOK, rec1.Code)

	// A different IP is still throttled because the bucket is shared.
	req2 := httptest.NewRequest(http.MethodGet, "/", nil)
	req2.RemoteAddr = "10.0.0.2:2222"
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, req2)
	assert.Equal(t, http.StatusTooManyRequests, rec2.Code)
}

func TestRateLimit_NonBlocking(t *testing.T) {
	t.Parallel()

	// A rejected request must return promptly (Allow, not Wait). With a fill
	// rate of 1 rps and burst 1, the second request would block ~1s under Wait;
	// under Allow it returns immediately with 429. The test simply asserts the
	// call returns without hanging the test deadline.
	cfg := RateLimitConfig{RequestsPerSecond: 1, Burst: 1}
	handler := RateLimitMiddleware(logger.ToSlog(logger.NewNoop()), cfg)(okHandler())

	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))

	done := make(chan int, 1)
	go func() {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
		done <- rec.Code
	}()

	assert.Equal(t, http.StatusTooManyRequests, <-done)
}

func TestClientIPKey(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		remoteAddr string
		xff        string
		want       string
	}{
		{name: "host:port stripped", remoteAddr: "192.0.2.5:54321", want: "192.0.2.5"},
		{name: "ipv6", remoteAddr: "[2001:db8::1]:443", want: "2001:db8::1"},
		{name: "no port returns raw", remoteAddr: "192.0.2.5", want: "192.0.2.5"},
		{name: "xff ignored (spoofable)", remoteAddr: "192.0.2.5:1", xff: "1.2.3.4", want: "192.0.2.5"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.RemoteAddr = tt.remoteAddr
			if tt.xff != "" {
				req.Header.Set("X-Forwarded-For", tt.xff)
			}

			assert.Equal(t, tt.want, ClientIPKey(req))
		})
	}
}

func TestRateLimit_ConcurrentRace(t *testing.T) {
	t.Parallel()

	// Race-detector exercise: concurrent requests across many keys must be
	// race-clean with no package-level mutable state.
	cfg := RateLimitConfig{RequestsPerSecond: 1000, Burst: 1000, KeyFunc: ClientIPKey, MaxTrackedKeys: 64}
	handler := RateLimitMiddleware(logger.ToSlog(logger.NewNoop()), cfg)(okHandler())

	var wg sync.WaitGroup
	for i := range 200 {
		wg.Add(1)

		go func(n int) {
			defer wg.Done()

			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.RemoteAddr = "10.0." + strconv.Itoa(n%128) + ".1:1234"
			handler.ServeHTTP(httptest.NewRecorder(), req)
		}(i)
	}

	wg.Wait()
}

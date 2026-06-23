package http

import (
	"net/http"

	"golang.org/x/time/rate"

	"gitlab.com/phpboyscout/go-tool-base/internal/ratelimit"
	"gitlab.com/phpboyscout/go-tool-base/pkg/config"
	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
)

const (
	defaultRateLimitRPS   = 50
	defaultRateLimitBurst = 100
	// defaultMaxTrackedKeys bounds the per-client bucket store so an attacker
	// rotating source keys cannot allocate unbounded *rate.Limiter values and
	// exhaust memory. The least-recently-used key is evicted when the cap is hit.
	defaultMaxTrackedKeys = ratelimit.DefaultMaxTrackedKeys
)

// RateLimitConfig configures the server-side token-bucket rate limiter.
type RateLimitConfig struct {
	// RequestsPerSecond is the sustained fill rate of the token bucket.
	// Must be > 0. Default: 50.
	RequestsPerSecond float64

	// Burst is the bucket capacity — the maximum number of requests that may
	// be admitted in an instantaneous spike. Must be >= 1. Default: 100.
	Burst int

	// KeyFunc derives the limiter key for a request, enabling per-client
	// limiting. When nil, a single global bucket is used for all requests.
	// A common choice is to key on the client IP (see ClientIPKey).
	KeyFunc func(*http.Request) string

	// MaxTrackedKeys bounds the per-client bucket store. Ignored when KeyFunc
	// is nil. Must be >= 1. Default: 8192.
	MaxTrackedKeys int

	// OnLimited is invoked when a request is rejected, before the 429 is
	// written. Optional; useful for metrics/telemetry. A structured debug log
	// is emitted via the constructor's logger regardless.
	OnLimited func(*http.Request)
}

// DefaultRateLimitConfig returns a RateLimitConfig suitable for a modest
// management/API server: 50 rps sustained, burst 100, single global bucket.
func DefaultRateLimitConfig() RateLimitConfig {
	return RateLimitConfig{
		RequestsPerSecond: defaultRateLimitRPS,
		Burst:             defaultRateLimitBurst,
		MaxTrackedKeys:    defaultMaxTrackedKeys,
	}
}

// RateLimitConfigFromConfig builds a RateLimitConfig from the config layer under
// "<prefix>.ratelimit.*" (prefix defaults to "server.http"), so operators tune
// the limiter via config like they tune the port or TLS. Recognised keys:
//
//	<prefix>.ratelimit.requests_per_second  (float)
//	<prefix>.ratelimit.burst                (int)
//	<prefix>.ratelimit.max_tracked_keys     (int)
//
// Unset keys keep their DefaultRateLimitConfig values. The code-only fields
// (KeyFunc, OnLimited) are never read from config — wiring stays explicit; this
// only supplies the policy numbers.
func RateLimitConfigFromConfig(cfg config.Containable, prefix string) RateLimitConfig {
	if prefix == "" {
		prefix = DefaultConfigPrefix
	}

	base := prefix + ".ratelimit"
	c := DefaultRateLimitConfig()

	if cfg.IsSet(base + ".requests_per_second") {
		c.RequestsPerSecond = cfg.GetFloat(base + ".requests_per_second")
	}

	if cfg.IsSet(base + ".burst") {
		c.Burst = cfg.GetInt(base + ".burst")
	}

	if cfg.IsSet(base + ".max_tracked_keys") {
		c.MaxTrackedKeys = cfg.GetInt(base + ".max_tracked_keys")
	}

	return c
}

// normalized returns a copy with invalid values clamped to safe defaults, so a
// misconfigured limiter degrades into the default policy rather than panicking
// (rate.NewLimiter accepts any rate but a non-positive burst would reject every
// request, which is a silent footgun).
func (c RateLimitConfig) normalized() RateLimitConfig {
	if c.RequestsPerSecond <= 0 {
		c.RequestsPerSecond = defaultRateLimitRPS
	}

	if c.Burst < 1 {
		c.Burst = defaultRateLimitBurst
	}

	if c.MaxTrackedKeys < 1 {
		c.MaxTrackedKeys = defaultMaxTrackedKeys
	}

	return c
}

// RateLimitMiddleware returns a Middleware that admits requests under a
// token-bucket limiter and rejects excess traffic with 429 Too Many Requests
// plus a Retry-After header (which a GTB client's retry layer honours). An
// invalid config is clamped to defaults rather than rejected.
//
// Because it is an ordinary Middleware it composes into any Chain and can be
// scoped globally (one entry in the server chain) or per-route (wrap a single
// handler). Per-client limiting is enabled by setting RateLimitConfig.KeyFunc.
//
// Health endpoints (/healthz, /livez, /readyz) are mounted outside the
// WithMiddleware chain by Register, so a global limiter never throttles probes.
func RateLimitMiddleware(log logger.Logger, cfg RateLimitConfig) Middleware {
	cfg = cfg.normalized()

	limit := rate.Limit(cfg.RequestsPerSecond)

	// A nil KeyFunc means one shared bucket for all traffic — use a single
	// limiter directly and avoid the per-key store's map+lock on every request.
	var (
		global *rate.Limiter
		store  *ratelimit.Store
	)

	if cfg.KeyFunc == nil {
		global = rate.NewLimiter(limit, cfg.Burst)
	} else {
		store = ratelimit.NewStore(limit, cfg.Burst, cfg.MaxTrackedKeys)
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			limiter := global

			key := ""
			if cfg.KeyFunc != nil {
				key = cfg.KeyFunc(r)
				limiter = store.LimiterFor(key)
			}

			// Allow is non-blocking: ingress must reject excess, never block,
			// or a flood would queue and exhaust memory (unlike the egress
			// WithRateLimit, which uses Wait to throttle the caller).
			if !limiter.Allow() {
				if cfg.OnLimited != nil {
					cfg.OnLimited(r)
				}

				log.Debug("request rate-limited", "path", r.URL.Path, "key", key)
				w.Header().Set("Retry-After", "1")
				http.Error(w, "rate limit exceeded", http.StatusTooManyRequests)

				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// ClientIPKey is a ready-made RateLimitConfig.KeyFunc that keys on the client
// IP using the connection's RemoteAddr.
//
// It deliberately does NOT trust X-Forwarded-For / X-Real-IP: those headers are
// spoofable by any direct client, so keying a limiter on them would let an
// attacker both evade their own bucket and churn the bounded key store by
// rotating fake IPs. A server behind a trusted reverse proxy that terminates
// XFF should supply its own KeyFunc that reads the proxy-set header. This reuses
// the logging middleware's client-IP derivation with trustedProxy=false, the
// same safe default.
func ClientIPKey(r *http.Request) string {
	return clientIP(r, false)
}

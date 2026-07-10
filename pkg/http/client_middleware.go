package http

import (
	"encoding/base64"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/cockroachdb/errors"
	"golang.org/x/time/rate"
)

// ClientMiddleware wraps an http.RoundTripper with additional behaviour.
// The first middleware in a chain is the outermost wrapper — it executes
// first on the request and last on the response.
type ClientMiddleware func(next http.RoundTripper) http.RoundTripper

// ClientChain composes ClientMiddleware in order. Immutable — Append
// returns a new chain.
type ClientChain struct {
	middlewares []ClientMiddleware
}

// NewClientChain creates a ClientChain from the given middleware.
func NewClientChain(middlewares ...ClientMiddleware) ClientChain {
	return ClientChain{middlewares: append([]ClientMiddleware{}, middlewares...)}
}

// Append returns a new chain with additional middleware appended.
func (c ClientChain) Append(middlewares ...ClientMiddleware) ClientChain {
	combined := make([]ClientMiddleware, 0, len(c.middlewares)+len(middlewares))
	combined = append(combined, c.middlewares...)
	combined = append(combined, middlewares...)

	return ClientChain{middlewares: combined}
}

// Then applies the middleware chain to the given RoundTripper and returns
// the wrapped result.
func (c ClientChain) Then(rt http.RoundTripper) http.RoundTripper {
	for i := len(c.middlewares) - 1; i >= 0; i-- {
		rt = c.middlewares[i](rt)
	}

	return rt
}

// WithClientMiddleware applies a middleware chain to the client's transport.
// The chain wraps the transport after retry (if configured) so that retry
// operates on the raw transport, not on logged/authed requests.
func WithClientMiddleware(chain ClientChain) ClientOption {
	return func(cfg *clientConfig) {
		cfg.clientChain = &chain
	}
}

// --- Built-in Client Middleware ---

// WithRequestLogging returns middleware that logs each outbound request and
// response at debug level. Logs method, URL, status code, and duration.
// Headers and body are NOT logged for security.
func WithRequestLogging(log *slog.Logger) ClientMiddleware {
	return func(next http.RoundTripper) http.RoundTripper {
		return roundTripFunc(func(req *http.Request) (*http.Response, error) {
			start := time.Now()

			resp, err := next.RoundTrip(req)

			duration := time.Since(start)
			if err != nil {
				log.Debug("HTTP request failed",
					"method", req.Method,
					"url", req.URL.String(),
					"duration", duration,
					"error", err,
				)
			} else {
				log.Debug("HTTP request completed",
					"method", req.Method,
					"url", req.URL.String(),
					"status", resp.StatusCode,
					"duration", duration,
				)
			}

			return resp, err
		})
	}
}

// hostPinnedAuth wraps next so that authValue is injected as the Authorization
// header only when the request host matches the first host this middleware
// saw. Because credential middleware is a RoundTripper, it runs on every
// redirect hop and net/http's cross-host Authorization stripping (which only
// governs headers set on the initial request) does not apply — pinning the
// host prevents leaking the credential to a redirect target the caller did
// not address.
func hostPinnedAuth(next http.RoundTripper, authValue string) http.RoundTripper {
	var (
		once        sync.Once
		allowedHost string
	)

	return roundTripFunc(func(req *http.Request) (*http.Response, error) {
		once.Do(func() { allowedHost = req.URL.Host })

		if req.URL.Host == allowedHost {
			req = req.Clone(req.Context())
			req.Header.Set("Authorization", authValue)
		}

		return next.RoundTrip(req)
	})
}

// WithBearerToken returns middleware that injects an Authorization: Bearer
// header. The header is only sent to the first host the client addresses, so
// a cross-host redirect cannot capture the token.
func WithBearerToken(token string) ClientMiddleware {
	return func(next http.RoundTripper) http.RoundTripper {
		return hostPinnedAuth(next, "Bearer "+token)
	}
}

// WithBasicAuth returns middleware that injects an Authorization: Basic
// header. The header is only sent to the first host the client addresses, so
// a cross-host redirect cannot capture the credential.
func WithBasicAuth(username, password string) ClientMiddleware {
	encoded := base64.StdEncoding.EncodeToString([]byte(username + ":" + password))

	return func(next http.RoundTripper) http.RoundTripper {
		return hostPinnedAuth(next, "Basic "+encoded)
	}
}

// WithRateLimit returns middleware that limits outbound requests to the
// specified rate using a token-bucket limiter (burst 1). Blocks until a token
// is available or the request context is cancelled. The limiter is shared
// across all requests through the transport, so it holds under concurrency —
// the previous hand-rolled version let concurrent goroutines sleep in parallel
// and then proceed together, admitting a burst per interval.
func WithRateLimit(requestsPerSecond float64) ClientMiddleware {
	limiter := rate.NewLimiter(rate.Limit(requestsPerSecond), 1)

	return func(next http.RoundTripper) http.RoundTripper {
		return roundTripFunc(func(req *http.Request) (*http.Response, error) {
			if err := limiter.Wait(req.Context()); err != nil {
				return nil, errors.WithStack(err)
			}

			return next.RoundTrip(req)
		})
	}
}

// roundTripFunc is a function adapter for http.RoundTripper.
type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

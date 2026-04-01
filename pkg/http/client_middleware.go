package http

import (
	"encoding/base64"
	"net/http"
	"sync"
	"time"

	"github.com/phpboyscout/go-tool-base/pkg/logger"
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
func WithRequestLogging(log logger.Logger) ClientMiddleware {
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

// WithBearerToken returns middleware that injects an Authorization: Bearer
// header on every request.
func WithBearerToken(token string) ClientMiddleware {
	return func(next http.RoundTripper) http.RoundTripper {
		return roundTripFunc(func(req *http.Request) (*http.Response, error) {
			req = req.Clone(req.Context())
			req.Header.Set("Authorization", "Bearer "+token)

			return next.RoundTrip(req)
		})
	}
}

// WithBasicAuth returns middleware that injects an Authorization: Basic
// header on every request.
func WithBasicAuth(username, password string) ClientMiddleware {
	encoded := base64.StdEncoding.EncodeToString([]byte(username + ":" + password))

	return func(next http.RoundTripper) http.RoundTripper {
		return roundTripFunc(func(req *http.Request) (*http.Response, error) {
			req = req.Clone(req.Context())
			req.Header.Set("Authorization", "Basic "+encoded)

			return next.RoundTrip(req)
		})
	}
}

// WithRateLimit returns middleware that limits outbound requests to the
// specified rate using a token bucket algorithm. Blocks until a token is
// available or the request context is cancelled.
func WithRateLimit(requestsPerSecond float64) ClientMiddleware {
	interval := time.Duration(float64(time.Second) / requestsPerSecond)

	return func(next http.RoundTripper) http.RoundTripper {
		var mu sync.Mutex

		var lastRequest time.Time

		return roundTripFunc(func(req *http.Request) (*http.Response, error) {
			mu.Lock()

			now := time.Now()

			if elapsed := now.Sub(lastRequest); elapsed < interval {
				wait := interval - elapsed
				mu.Unlock()

				select {
				case <-time.After(wait):
				case <-req.Context().Done():
					return nil, req.Context().Err()
				}

				mu.Lock()
			}

			lastRequest = time.Now()
			mu.Unlock()

			return next.RoundTrip(req)
		})
	}
}

// roundTripFunc is a function adapter for http.RoundTripper.
type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

package http

// This file makes pkg/http a thin adapter over the shared transport-middleware
// module gitlab.com/phpboyscout/go/transit. It re-exports the module's HTTP
// middleware — the server chain, the structured/OTel/rate-limit server
// middleware, and the client round-trippers (retry, circuit breaker, auth) — so
// existing GTB call sites are unchanged. The GTB-specific pieces that cannot live
// in the framework-free module stay in this package: the secure client
// constructor and its WithRetry/WithClientMiddleware options (client.go), the
// config-key adapters (config_adapter.go), server wiring (server.go), request
// authentication (auth.go) and security headers (security_headers.go).

import (
	transithttp "gitlab.com/phpboyscout/go/transit/http"
)

// Re-exported middleware and config types.
type (
	Middleware                    = transithttp.Middleware
	Chain                         = transithttp.Chain
	CircuitState                  = transithttp.CircuitState
	CircuitBreakerConfig          = transithttp.CircuitBreakerConfig
	CircuitBreakerConfigOverrides = transithttp.CircuitBreakerConfigOverrides
	ClientMiddleware              = transithttp.ClientMiddleware
	ClientChain                   = transithttp.ClientChain
	LogFormat                     = transithttp.LogFormat
	LoggingOption                 = transithttp.LoggingOption
	RateLimitConfig               = transithttp.RateLimitConfig
	RateLimitConfigOverrides      = transithttp.RateLimitConfigOverrides
	RetryConfig                   = transithttp.RetryConfig
)

// Re-exported typed constants (circuit-breaker states and log formats).
const (
	StateClosed   = transithttp.StateClosed
	StateOpen     = transithttp.StateOpen
	StateHalfOpen = transithttp.StateHalfOpen

	FormatStructured = transithttp.FormatStructured
	FormatCommon     = transithttp.FormatCommon
	FormatCombined   = transithttp.FormatCombined
	FormatJSON       = transithttp.FormatJSON
)

// Re-exported sentinel error.
var ErrCircuitOpen = transithttp.ErrCircuitOpen

// Re-exported constructors and helpers (function values forwarding to the module).
var (
	NewChain                    = transithttp.NewChain
	LoggingMiddleware           = transithttp.LoggingMiddleware
	OTelMiddleware              = transithttp.OTelMiddleware
	RateLimitMiddleware         = transithttp.RateLimitMiddleware
	ClientIPKey                 = transithttp.ClientIPKey
	DefaultRateLimitConfig      = transithttp.DefaultRateLimitConfig
	MergeRateLimitConfig        = transithttp.MergeRateLimitConfig
	DefaultCircuitBreakerConfig = transithttp.DefaultCircuitBreakerConfig
	MergeCircuitBreakerConfig   = transithttp.MergeCircuitBreakerConfig
	WithCircuitBreaker          = transithttp.WithCircuitBreaker
	NewClientChain              = transithttp.NewClientChain
	WithRequestLogging          = transithttp.WithRequestLogging
	WithBearerToken             = transithttp.WithBearerToken
	WithBasicAuth               = transithttp.WithBasicAuth
	WithRateLimit               = transithttp.WithRateLimit
	DefaultRetryConfig          = transithttp.DefaultRetryConfig
	NewRetryTransport           = transithttp.NewRetryTransport

	// Logging options.
	WithFormat       = transithttp.WithFormat
	WithLogLevel     = transithttp.WithLogLevel
	WithoutLatency   = transithttp.WithoutLatency
	WithoutUserAgent = transithttp.WithoutUserAgent
	WithPathFilter   = transithttp.WithPathFilter
	WithTrustedProxy = transithttp.WithTrustedProxy
	WithHeaderFields = transithttp.WithHeaderFields
)

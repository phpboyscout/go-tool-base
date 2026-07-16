package grpc

// This file makes pkg/grpc a thin adapter over the shared transport-middleware
// module gitlab.com/phpboyscout/go/transit. It re-exports the module's gRPC
// interceptors and instrumentation — the server interceptor chain, logging and
// rate-limit interceptors, the OTel stats/client handlers, and the client
// circuit-breaker interceptors — so existing GTB call sites are unchanged. The
// GTB-specific pieces stay in this package: the server constructor and Register
// wiring (server.go), the config-key adapters (config_adapter.go) and request
// authentication (auth.go).

import (
	transitgrpc "gitlab.com/phpboyscout/go/transit/grpc"
)

// Re-exported interceptor and config types.
type (
	Interceptor                   = transitgrpc.Interceptor
	InterceptorChain              = transitgrpc.InterceptorChain
	CircuitState                  = transitgrpc.CircuitState
	CircuitBreakerConfig          = transitgrpc.CircuitBreakerConfig
	CircuitBreakerConfigOverrides = transitgrpc.CircuitBreakerConfigOverrides
	GRPCLoggingOption             = transitgrpc.GRPCLoggingOption
	RateLimitConfig               = transitgrpc.RateLimitConfig
	RateLimitConfigOverrides      = transitgrpc.RateLimitConfigOverrides
)

// Re-exported circuit-breaker state constants.
const (
	StateClosed   = transitgrpc.StateClosed
	StateOpen     = transitgrpc.StateOpen
	StateHalfOpen = transitgrpc.StateHalfOpen
)

// Re-exported constructors and helpers (function values forwarding to the module).
var (
	NewInterceptorChain             = transitgrpc.NewInterceptorChain
	LoggingInterceptor              = transitgrpc.LoggingInterceptor
	RateLimitInterceptor            = transitgrpc.RateLimitInterceptor
	PeerKey                         = transitgrpc.PeerKey
	OTelStatsHandler                = transitgrpc.OTelStatsHandler
	OTelClientHandler               = transitgrpc.OTelClientHandler
	DefaultRateLimitConfig          = transitgrpc.DefaultRateLimitConfig
	MergeRateLimitConfig            = transitgrpc.MergeRateLimitConfig
	DefaultCircuitBreakerConfig     = transitgrpc.DefaultCircuitBreakerConfig
	MergeCircuitBreakerConfig       = transitgrpc.MergeCircuitBreakerConfig
	CircuitBreakerInterceptor       = transitgrpc.CircuitBreakerInterceptor
	CircuitBreakerStreamInterceptor = transitgrpc.CircuitBreakerStreamInterceptor

	// Logging options.
	WithGRPCLogLevel   = transitgrpc.WithGRPCLogLevel
	WithoutGRPCLatency = transitgrpc.WithoutGRPCLatency
	WithGRPCPathFilter = transitgrpc.WithGRPCPathFilter
)

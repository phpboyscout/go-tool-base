package grpc

import (
	"context"

	"golang.org/x/time/rate"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"

	"gitlab.com/phpboyscout/go-tool-base/internal/ratelimit"
	"gitlab.com/phpboyscout/go-tool-base/pkg/config"
	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
)

const (
	defaultRateLimitRPS   = 50
	defaultRateLimitBurst = 100
)

// RateLimitConfig configures the server-side token-bucket rate limiter for gRPC
// ingress, mirroring the HTTP server limiter.
type RateLimitConfig struct {
	// RequestsPerSecond is the sustained fill rate. Must be > 0. Default: 50.
	RequestsPerSecond float64
	// Burst is the bucket capacity. Must be >= 1. Default: 100.
	Burst int
	// MaxTrackedKeys bounds the per-key bucket store (ignored when KeyFunc is
	// nil). Must be >= 1. Default: 8192.
	MaxTrackedKeys int
	// KeyFunc derives the limiter key from the RPC context and full method name,
	// enabling per-client or per-method limiting. When nil, a single global
	// bucket is used. See PeerKey.
	KeyFunc func(ctx context.Context, fullMethod string) string
	// OnLimited is invoked when an RPC is rejected. Optional.
	OnLimited func(ctx context.Context, fullMethod string)
}

// DefaultRateLimitConfig returns a limiter suitable for a modest management
// server: 50 rps sustained, burst 100, single global bucket.
func DefaultRateLimitConfig() RateLimitConfig {
	return RateLimitConfig{
		RequestsPerSecond: defaultRateLimitRPS,
		Burst:             defaultRateLimitBurst,
		MaxTrackedKeys:    ratelimit.DefaultMaxTrackedKeys,
	}
}

// RateLimitConfigFromConfig builds a RateLimitConfig from the config layer under
// "<prefix>.ratelimit.*" (prefix defaults to "server.grpc"), mirroring the HTTP
// helper. Recognised keys: requests_per_second (float), burst (int),
// max_tracked_keys (int). Unset keys keep their defaults; KeyFunc/OnLimited are
// never read from config.
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

func (c RateLimitConfig) normalized() RateLimitConfig {
	if c.RequestsPerSecond <= 0 {
		c.RequestsPerSecond = defaultRateLimitRPS
	}

	if c.Burst < 1 {
		c.Burst = defaultRateLimitBurst
	}

	if c.MaxTrackedKeys < 1 {
		c.MaxTrackedKeys = ratelimit.DefaultMaxTrackedKeys
	}

	return c
}

// RateLimitInterceptor returns an Interceptor (unary + stream) that admits RPCs
// under a token-bucket limiter and rejects excess with codes.ResourceExhausted.
// It composes into any InterceptorChain. Per-method or per-client scoping is
// achieved by setting KeyFunc (e.g. PeerKey, or a func keying on fullMethod).
//
// Like the HTTP limiter, admission is non-blocking (Allow, not Wait): ingress
// must reject excess, never queue it, or a flood would exhaust memory.
func RateLimitInterceptor(log logger.Logger, cfg RateLimitConfig) Interceptor {
	cfg = cfg.normalized()

	limit := rate.Limit(cfg.RequestsPerSecond)

	var (
		global *rate.Limiter
		store  *ratelimit.Store
	)

	if cfg.KeyFunc == nil {
		global = rate.NewLimiter(limit, cfg.Burst)
	} else {
		store = ratelimit.NewStore(limit, cfg.Burst, cfg.MaxTrackedKeys)
	}

	admit := func(ctx context.Context, fullMethod string) bool {
		limiter := global
		if cfg.KeyFunc != nil {
			limiter = store.LimiterFor(cfg.KeyFunc(ctx, fullMethod))
		}

		return limiter.Allow()
	}

	reject := func(ctx context.Context, fullMethod string) error {
		if cfg.OnLimited != nil {
			cfg.OnLimited(ctx, fullMethod)
		}

		log.Debug("rpc rate-limited", "method", fullMethod)

		return status.Error(codes.ResourceExhausted, "rate limit exceeded")
	}

	return Interceptor{
		Unary: func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
			if !admit(ctx, info.FullMethod) {
				return nil, reject(ctx, info.FullMethod)
			}

			return handler(ctx, req)
		},
		Stream: func(srv any, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
			if !admit(ss.Context(), info.FullMethod) {
				return reject(ss.Context(), info.FullMethod)
			}

			return handler(srv, ss)
		},
	}
}

// PeerKey is a ready-made KeyFunc keying on the RPC peer address. RPCs with no
// resolvable peer share a single bucket under the empty key.
func PeerKey(ctx context.Context, _ string) string {
	if p, ok := peer.FromContext(ctx); ok && p.Addr != nil {
		return p.Addr.String()
	}

	return ""
}

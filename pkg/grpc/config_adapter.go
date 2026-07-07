package grpc

import "gitlab.com/phpboyscout/go-tool-base/pkg/config"

// RateLimitConfigFromConfig builds a RateLimitConfig from the config layer
// under "<prefix>.ratelimit.*" (prefix defaults to "server.grpc").
//
// Unset keys keep their DefaultRateLimitConfig values. The code-only fields
// (KeyFunc, OnLimited) are never read from config; wiring stays explicit.
func RateLimitConfigFromConfig(cfg config.Containable, prefix string) RateLimitConfig {
	if prefix == "" {
		prefix = DefaultConfigPrefix
	}

	if cfg == nil {
		return DefaultRateLimitConfig()
	}

	base := prefix + ".ratelimit"

	section, err := config.UnmarshalSection[RateLimitConfig](cfg, base)
	if err != nil || !section.Exists {
		return DefaultRateLimitConfig()
	}

	return MergeRateLimitConfig(DefaultRateLimitConfig(), section.Value, RateLimitConfigOverrides{
		RequestsPerSecond: cfg.IsSet(base + ".requests_per_second"),
		Burst:             cfg.IsSet(base + ".burst"),
		MaxTrackedKeys:    cfg.IsSet(base + ".max_tracked_keys"),
	})
}

// CircuitBreakerConfigFromConfig builds a CircuitBreakerConfig from the config
// layer under "<prefix>.circuitbreaker.*" (prefix defaults to "server.grpc").
//
// Unset keys keep their DefaultCircuitBreakerConfig values. The code-only
// fields (IsFailure, OnStateChange) are never read from config.
func CircuitBreakerConfigFromConfig(cfg config.Containable, prefix string) CircuitBreakerConfig {
	if prefix == "" {
		prefix = DefaultConfigPrefix
	}

	if cfg == nil {
		return DefaultCircuitBreakerConfig()
	}

	base := prefix + ".circuitbreaker"

	section, err := config.UnmarshalSection[CircuitBreakerConfig](cfg, base)
	if err != nil || !section.Exists {
		return DefaultCircuitBreakerConfig()
	}

	return MergeCircuitBreakerConfig(DefaultCircuitBreakerConfig(), section.Value, CircuitBreakerConfigOverrides{
		FailureThreshold:    cfg.IsSet(base + ".failure_threshold"),
		Cooldown:            cfg.IsSet(base + ".cooldown"),
		HalfOpenMaxRequests: cfg.IsSet(base + ".half_open_max_requests"),
	})
}

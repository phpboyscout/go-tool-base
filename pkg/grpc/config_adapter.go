package grpc

import (
	"context"

	"google.golang.org/grpc"

	"gitlab.com/phpboyscout/go-tool-base/pkg/config"
	"gitlab.com/phpboyscout/go-tool-base/pkg/controls"
	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	gtbtls "gitlab.com/phpboyscout/go-tool-base/pkg/tls"
)

// ServerSettingsFromConfig resolves gRPC server settings from GTB config. It
// preserves the existing fallback from <prefix>.port to server.port.
func ServerSettingsFromConfig(cfg config.Containable, prefix string) ServerSettings {
	return serverSettingsFromConfig(cfg, prefix, true, true)
}

func serverSettingsFromConfig(cfg config.Containable, prefix string, includePort, includeReflection bool) ServerSettings {
	if prefix == "" {
		prefix = DefaultConfigPrefix
	}

	if cfg == nil {
		return ServerSettings{}
	}

	if _, ok := cfg.(*config.Container); !ok {
		return serverSettingsFromLegacyConfig(cfg, prefix, includePort, includeReflection)
	}

	section, err := config.UnmarshalSection[ServerSettings](cfg, prefix)
	if err != nil {
		return serverSettingsFromLegacyConfig(cfg, prefix, includePort, includeReflection)
	}

	settings := section.Value
	if !includePort {
		settings.Port = 0
	} else if settings.Port == 0 {
		settings.Port = cfg.GetInt(ConfigKeySharedPort)
	}

	if !includeReflection {
		settings.Reflection = false
	}

	return settings
}

func serverSettingsFromLegacyConfig(cfg config.Containable, prefix string, includePort, includeReflection bool) ServerSettings {
	var settings ServerSettings

	if includePort {
		settings.Port = cfg.GetInt(prefix + ".port")
	}

	if includeReflection {
		settings.Reflection = cfg.GetBool(prefix + ".reflection")
	}

	if includePort && settings.Port == 0 {
		settings.Port = cfg.GetInt(ConfigKeySharedPort)
	}

	return settings
}

// NewServer returns a new preconfigured grpc.Server.
//
// Default gRPC options applied (before caller-supplied opts):
//   - grpc.MaxRecvMsgSize(DefaultMaxGRPCMessageBytes)
//   - grpc.MaxSendMsgSize(DefaultMaxGRPCMessageBytes)
//
// Caller-supplied grpc.ServerOption values override the defaults (gRPC applies
// later options last, so a caller can raise or lower the limits explicitly).
//
// The opts variadic accepts both ServerOption values (e.g. WithConfigPrefix,
// which selects the config block the reflection flag is read from) and
// grpc.ServerOption values; other types are ignored.
func NewServer(cfg config.Containable, opts ...any) (*grpc.Server, error) {
	sc := defaultServerConfig()

	var serverOpts []grpc.ServerOption

	for _, o := range opts {
		switch v := o.(type) {
		case ServerOption:
			v(&sc)
		case grpc.ServerOption:
			serverOpts = append(serverOpts, v)
		}
	}

	if sc.prefix == "" {
		return newServer(ServerSettings{}, sc, serverOpts...)
	}

	return newServer(serverSettingsFromConfig(cfg, sc.prefix, false, true), sc, serverOpts...)
}

// Start returns a curried function suitable for use with the controls package.
// With no options it reads its port and TLS from the default "server.grpc"
// config block; pass WithConfigPrefix/WithPort to target a custom server.
// TLS configuration cascades: <prefix>.tls.* overrides server.tls.* shared defaults.
func Start(cfg config.Containable, logger logger.Logger, srv *grpc.Server, opts ...ServerOption) controls.StartFunc {
	sc := defaultServerConfig()
	for _, o := range opts {
		o(&sc)
	}

	if sc.prefix == "" {
		sc.prefix = DefaultConfigPrefix
	}

	settings := serverSettingsFromConfig(cfg, sc.prefix, sc.port == nil, false)
	tlsPair := gtbtls.Resolve(cfg, sc.tlsPrefix())

	return start(logger, srv, settings, tlsPair, sc, nil)
}

// DialLocal dials the gRPC server described by cfg over the loopback interface,
// using transport security that matches the server's own TLS config
// (server.grpc.tls -> server.tls). Intended for in-process callers such as the
// grpc-gateway, so they connect to the local server without re-deriving the
// endpoint or credentials by hand.
// The opts variadic accepts both ServerOption values (e.g. WithConfigPrefix to
// dial a non-default gRPC server) and grpc.DialOption values; other types are
// ignored.
func DialLocal(cfg config.Containable, opts ...any) (*grpc.ClientConn, error) {
	sc := defaultServerConfig()

	var dialOpts []grpc.DialOption

	for _, o := range opts {
		switch v := o.(type) {
		case ServerOption:
			v(&sc)
		case grpc.DialOption:
			dialOpts = append(dialOpts, v)
		}
	}

	settings := serverSettingsFromConfig(cfg, sc.prefix, sc.port == nil, false)
	tlsPair := gtbtls.Resolve(cfg, sc.tlsPrefix())

	return dialLocal(settings, tlsPair, sc, dialOpts...)
}

// Register creates a new gRPC server and registers it with the controller under
// the given id. The opts variadic accepts ServerOption values (port, prefix),
// RegisterOption values (interceptors) and grpc.ServerOption values.
func Register(_ context.Context, id string, controller controls.Controllable, cfg config.Containable, logger logger.Logger, opts ...any) (*grpc.Server, error) {
	sc := defaultServerConfig()

	var rc registerConfig

	var serverOpts []grpc.ServerOption

	for _, o := range opts {
		switch v := o.(type) {
		case ServerOption:
			v(&sc)
		case RegisterOption:
			v(&rc)
		case grpc.ServerOption:
			serverOpts = append(serverOpts, v)
		}
	}

	if sc.prefix == "" {
		return register(id, controller, logger, ServerSettings{}, gtbtls.Pair{}, sc, rc, serverOpts)
	}

	settings := serverSettingsFromConfig(cfg, sc.prefix, sc.port == nil, true)
	tlsPair := gtbtls.Resolve(cfg, sc.tlsPrefix())

	return register(id, controller, logger, settings, tlsPair, sc, rc, serverOpts)
}

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

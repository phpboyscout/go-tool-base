package grpc

import (
	"context"

	"google.golang.org/grpc"

	"gitlab.com/phpboyscout/go/controls"
	transitgrpc "gitlab.com/phpboyscout/go/transit/grpc"
	transportgrpc "gitlab.com/phpboyscout/go/transport/grpc"

	"gitlab.com/phpboyscout/go/config"

	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	gtbtls "gitlab.com/phpboyscout/go-tool-base/pkg/tls"
)

// DefaultConfigPrefix is the config block a gRPC server reads its port,
// reflection and TLS settings from unless overridden with WithConfigPrefix.
const DefaultConfigPrefix = "server.grpc"

// ConfigKeySharedPort is the shared fallback port used when the per-server port
// key (<prefix>.port) is unset.
const ConfigKeySharedPort = "server.port"

// ServerOption selects which GTB config block (and, optionally, an explicit
// port) the *FromContainable adapters read. It is distinct from the transport's
// own gitlab.com/phpboyscout/go/transport/grpc.ServerOption; the adapters accept
// GTB options (config selection) and forward grpc.ServerOption / grpc.DialOption
// / transport RegisterOption values to the underlying constructor.
type ServerOption func(*serverConfig)

// serverConfig carries the GTB config-selection knobs.
type serverConfig struct {
	prefix string
	port   *int
}

// WithConfigPrefix sets the config block the server reads from (default
// "server.grpc"). Use it to run a second gRPC server on its own block, e.g.
// "server.internal".
func WithConfigPrefix(prefix string) ServerOption {
	return func(c *serverConfig) {
		c.prefix = prefix
	}
}

// WithPort sets the listen (or dial) port explicitly, bypassing config lookup.
func WithPort(port int) ServerOption {
	return func(c *serverConfig) {
		c.port = &port
	}
}

func (c serverConfig) resolvedPrefix() string {
	if c.prefix == "" {
		return DefaultConfigPrefix
	}

	return c.prefix
}

// ServerSettingsFromConfig resolves gRPC server settings from GTB config. It
// preserves the existing fallback from <prefix>.port to server.port.
func ServerSettingsFromConfig(cfg config.Containable, prefix string) transportgrpc.ServerSettings {
	return serverSettingsFromConfig(cfg, prefix, true, true)
}

// ObserveServerSettingsFromConfig binds gRPC server settings to cfg and keeps a
// typed snapshot rehydrated after successful config reloads.
func ObserveServerSettingsFromConfig(
	cfg config.Containable,
	prefix string,
	opts ...config.SectionBindingOption[transportgrpc.ServerSettings],
) (*config.ObservedSection[transportgrpc.ServerSettings], error) {
	if prefix == "" {
		prefix = DefaultConfigPrefix
	}

	bindingOpts := make([]config.SectionBindingOption[transportgrpc.ServerSettings], 0, 1+len(opts))
	bindingOpts = append(bindingOpts, config.WithSectionDefaultFunc(func(next config.Containable) transportgrpc.ServerSettings {
		if next == nil {
			return transportgrpc.ServerSettings{}
		}

		return transportgrpc.ServerSettings{Port: next.GetInt(ConfigKeySharedPort)}
	}, mergeServerSettings))
	bindingOpts = append(bindingOpts, opts...)

	return config.ObserveSection[transportgrpc.ServerSettings](cfg, prefix, bindingOpts...)
}

func serverSettingsFromConfig(cfg config.Containable, prefix string, includePort, includeReflection bool) transportgrpc.ServerSettings {
	if prefix == "" {
		prefix = DefaultConfigPrefix
	}

	if cfg == nil {
		return transportgrpc.ServerSettings{}
	}

	if _, ok := cfg.(*config.Container); !ok {
		return serverSettingsFromLegacyConfig(cfg, prefix, includePort, includeReflection)
	}

	section, err := config.UnmarshalSection[transportgrpc.ServerSettings](cfg, prefix)
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

func mergeServerSettings(defaults, overlay transportgrpc.ServerSettings) transportgrpc.ServerSettings {
	if overlay.Port != 0 {
		defaults.Port = overlay.Port
	}

	defaults.Reflection = overlay.Reflection

	return defaults
}

func serverSettingsFromLegacyConfig(cfg config.Containable, prefix string, includePort, includeReflection bool) transportgrpc.ServerSettings {
	var settings transportgrpc.ServerSettings

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

// portOverride applies an explicit WithPort onto resolved settings.
func portOverride(settings transportgrpc.ServerSettings, sc serverConfig) transportgrpc.ServerSettings {
	if sc.port != nil {
		settings.Port = *sc.port
	}

	return settings
}

// NewServerFromContainable returns a new preconfigured grpc.Server from config.
// GTB config-selection options (WithConfigPrefix) select the block the
// reflection flag is read from; grpc.ServerOption values are forwarded.
func NewServerFromContainable(cfg config.Containable, opts ...any) (*grpc.Server, error) {
	var sc serverConfig

	var forwarded []any

	for _, o := range opts {
		switch v := o.(type) {
		case ServerOption:
			v(&sc)
		default:
			forwarded = append(forwarded, v)
		}
	}

	settings := serverSettingsFromConfig(cfg, sc.resolvedPrefix(), false, true)

	return transportgrpc.NewServer(settings, forwarded...)
}

// StartFromContainable returns a curried function suitable for use with the
// controls package. With no options it reads its port and TLS from the default
// "server.grpc" config block; pass WithConfigPrefix/WithPort to target a custom
// server. TLS cascades: <prefix>.tls.* overrides server.tls.* shared defaults.
func StartFromContainable(cfg config.Containable, log logger.Logger, srv *grpc.Server, opts ...any) controls.StartFunc {
	var sc serverConfig

	for _, o := range opts {
		if v, ok := o.(ServerOption); ok {
			v(&sc)
		}
	}

	settings := portOverride(serverSettingsFromConfig(cfg, sc.resolvedPrefix(), sc.port == nil, false), sc)
	tlsPair := gtbtls.Resolve(cfg, sc.resolvedPrefix()+".tls")

	return transportgrpc.Start(logger.ToSlog(log), srv, settings, tlsPair)
}

// DialLocalFromContainable dials the gRPC server described by cfg over the
// loopback interface, using transport security that matches the server's own
// TLS config (server.grpc.tls -> server.tls). Intended for in-process callers
// such as the grpc-gateway. The opts variadic accepts GTB ServerOption values
// (WithConfigPrefix to dial a non-default server) and grpc.DialOption values.
func DialLocalFromContainable(cfg config.Containable, opts ...any) (*grpc.ClientConn, error) {
	var sc serverConfig

	var dialOpts []any

	for _, o := range opts {
		switch v := o.(type) {
		case ServerOption:
			v(&sc)
		default:
			dialOpts = append(dialOpts, v)
		}
	}

	settings := portOverride(serverSettingsFromConfig(cfg, sc.resolvedPrefix(), sc.port == nil, false), sc)
	tlsPair := gtbtls.Resolve(cfg, sc.resolvedPrefix()+".tls")

	return transportgrpc.DialLocal(settings, tlsPair, dialOpts...)
}

// RegisterFromContainable creates a new gRPC server from config and registers it
// with the controller under the given id. The opts variadic accepts GTB
// ServerOption values (WithConfigPrefix, WithPort), transport RegisterOption
// values (interceptors) and grpc.ServerOption values.
func RegisterFromContainable(_ context.Context, id string, controller controls.Controllable, cfg config.Containable, log logger.Logger, opts ...any) (*grpc.Server, error) {
	var sc serverConfig

	var forwarded []any

	for _, o := range opts {
		switch v := o.(type) {
		case ServerOption:
			v(&sc)
		default:
			forwarded = append(forwarded, v)
		}
	}

	settings := portOverride(serverSettingsFromConfig(cfg, sc.resolvedPrefix(), sc.port == nil, true), sc)
	tlsPair := gtbtls.Resolve(cfg, sc.resolvedPrefix()+".tls")

	return transportgrpc.Register(id, controller, logger.ToSlog(log), settings, tlsPair, forwarded...)
}

// RateLimitConfigFromConfig builds a transitgrpc.RateLimitConfig from the config layer
// under "<prefix>.ratelimit.*" (prefix defaults to "server.grpc").
//
// Unset keys keep their transitgrpc.DefaultRateLimitConfig values. The code-only fields
// (KeyFunc, OnLimited) are never read from config; wiring stays explicit.
func RateLimitConfigFromConfig(cfg config.Containable, prefix string) transitgrpc.RateLimitConfig {
	if prefix == "" {
		prefix = DefaultConfigPrefix
	}

	if cfg == nil {
		return transitgrpc.DefaultRateLimitConfig()
	}

	base := prefix + ".ratelimit"

	section, err := config.UnmarshalSection[transitgrpc.RateLimitConfig](cfg, base)
	if err != nil || !section.Exists {
		return transitgrpc.DefaultRateLimitConfig()
	}

	return transitgrpc.MergeRateLimitConfig(transitgrpc.DefaultRateLimitConfig(), section.Value, transitgrpc.RateLimitConfigOverrides{
		RequestsPerSecond: cfg.IsSet(base + ".requests_per_second"),
		Burst:             cfg.IsSet(base + ".burst"),
		MaxTrackedKeys:    cfg.IsSet(base + ".max_tracked_keys"),
	})
}

// CircuitBreakerConfigFromConfig builds a transitgrpc.CircuitBreakerConfig from the config
// layer under "<prefix>.circuitbreaker.*" (prefix defaults to "server.grpc").
//
// Unset keys keep their transitgrpc.DefaultCircuitBreakerConfig values. The code-only
// fields (IsFailure, OnStateChange) are never read from config.
func CircuitBreakerConfigFromConfig(cfg config.Containable, prefix string) transitgrpc.CircuitBreakerConfig {
	if prefix == "" {
		prefix = DefaultConfigPrefix
	}

	if cfg == nil {
		return transitgrpc.DefaultCircuitBreakerConfig()
	}

	base := prefix + ".circuitbreaker"

	section, err := config.UnmarshalSection[transitgrpc.CircuitBreakerConfig](cfg, base)
	if err != nil || !section.Exists {
		return transitgrpc.DefaultCircuitBreakerConfig()
	}

	return transitgrpc.MergeCircuitBreakerConfig(transitgrpc.DefaultCircuitBreakerConfig(), section.Value, transitgrpc.CircuitBreakerConfigOverrides{
		FailureThreshold:    cfg.IsSet(base + ".failure_threshold"),
		Cooldown:            cfg.IsSet(base + ".cooldown"),
		HalfOpenMaxRequests: cfg.IsSet(base + ".half_open_max_requests"),
	})
}

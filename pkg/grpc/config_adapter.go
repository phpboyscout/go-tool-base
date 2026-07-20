package grpc

import (
	"context"

	"google.golang.org/grpc"

	"gitlab.com/phpboyscout/go/controls"
	transitgrpc "gitlab.com/phpboyscout/go/transit/grpc"
	transportgrpc "gitlab.com/phpboyscout/go/transport/grpc"

	"gitlab.com/phpboyscout/go/config"

	"gitlab.com/phpboyscout/go-tool-base/internal/transportcfg"
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
// port) the *FromReader adapters read. It is distinct from the transport's
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
func ServerSettingsFromConfig(cfg config.Reader, prefix string) transportgrpc.ServerSettings {
	return serverSettingsFromConfig(cfg, prefix, true, true)
}

// ObserveServerSettingsFromConfig binds gRPC server settings to cfg and keeps a
// typed snapshot rehydrated after successful config reloads.
func ObserveServerSettingsFromConfig(
	cfg config.Binder,
	prefix string,
	opts ...config.SectionBindingOption[transportgrpc.ServerSettings],
) (*config.ObservedSection[transportgrpc.ServerSettings], error) {
	if prefix == "" {
		prefix = DefaultConfigPrefix
	}

	bindingOpts := make([]config.SectionBindingOption[transportgrpc.ServerSettings], 0, 1+len(opts))
	bindingOpts = append(bindingOpts, config.WithSectionDefaultFunc(func(next config.Observed) transportgrpc.ServerSettings {
		if next == nil {
			return transportgrpc.ServerSettings{}
		}

		return transportgrpc.ServerSettings{Port: next.GetInt(ConfigKeySharedPort)}
	}, mergeServerSettings))
	bindingOpts = append(bindingOpts, opts...)

	return config.ObserveSection[transportgrpc.ServerSettings](cfg, prefix, bindingOpts...)
}

func serverSettingsFromConfig(cfg config.Reader, prefix string, includePort, includeReflection bool) transportgrpc.ServerSettings {
	if prefix == "" {
		prefix = DefaultConfigPrefix
	}

	if cfg == nil {
		return transportgrpc.ServerSettings{}
	}

	// A malformed section (e.g. a non-numeric port) fails the typed decode;
	// fall back to per-key reads, which yield zero values for the bad keys.
	section, err := config.UnmarshalSection[transportgrpc.ServerSettings](cfg, prefix)
	if err != nil {
		return serverSettingsFromFlatKeys(cfg, prefix, includePort, includeReflection)
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

func serverSettingsFromFlatKeys(cfg config.Reader, prefix string, includePort, includeReflection bool) transportgrpc.ServerSettings {
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

// NewServerFromReader returns a new preconfigured grpc.Server from config.
// GTB config-selection options (WithConfigPrefix) select the block the
// reflection flag is read from; grpc.ServerOption values are forwarded.
func NewServerFromReader(cfg config.Reader, opts ...any) (*grpc.Server, error) {
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

// StartFromReader returns a curried function suitable for use with the
// controls package. With no options it reads its port and TLS from the default
// "server.grpc" config block; pass WithConfigPrefix/WithPort to target a custom
// server. TLS cascades: <prefix>.tls.* overrides server.tls.* shared defaults.
func StartFromReader(cfg config.Reader, log logger.Logger, srv *grpc.Server, opts ...any) controls.StartFunc {
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

// DialLocalFromReader dials the gRPC server described by cfg over the
// loopback interface, using transport security that matches the server's own
// TLS config (server.grpc.tls -> server.tls). Intended for in-process callers
// such as the grpc-gateway. The opts variadic accepts GTB ServerOption values
// (WithConfigPrefix to dial a non-default server) and grpc.DialOption values.
func DialLocalFromReader(cfg config.Reader, opts ...any) (*grpc.ClientConn, error) {
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

// RegisterFromReader creates a new gRPC server from config and registers it
// with the controller under the given id. The opts variadic accepts GTB
// ServerOption values (WithConfigPrefix, WithPort), transport RegisterOption
// values (interceptors) and grpc.ServerOption values.
func RegisterFromReader(_ context.Context, id string, controller controls.Controllable, cfg config.Reader, log logger.Logger, opts ...any) (*grpc.Server, error) {
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
func RateLimitConfigFromConfig(cfg config.Reader, prefix string) transitgrpc.RateLimitConfig {
	if prefix == "" {
		prefix = DefaultConfigPrefix
	}

	base := prefix + ".ratelimit"

	return transportcfg.ResolveSection(cfg, base,
		transitgrpc.DefaultRateLimitConfig,
		transitgrpc.MergeRateLimitConfig,
		func(isSet func(string) bool) transitgrpc.RateLimitConfigOverrides {
			return transitgrpc.RateLimitConfigOverrides{
				RequestsPerSecond: isSet(base + ".requests_per_second"),
				Burst:             isSet(base + ".burst"),
				MaxTrackedKeys:    isSet(base + ".max_tracked_keys"),
			}
		})
}

// CircuitBreakerConfigFromConfig builds a transitgrpc.CircuitBreakerConfig from the config
// layer under "<prefix>.circuitbreaker.*" (prefix defaults to "server.grpc").
//
// Unset keys keep their transitgrpc.DefaultCircuitBreakerConfig values. The code-only
// fields (IsFailure, OnStateChange) are never read from config.
func CircuitBreakerConfigFromConfig(cfg config.Reader, prefix string) transitgrpc.CircuitBreakerConfig {
	if prefix == "" {
		prefix = DefaultConfigPrefix
	}

	base := prefix + ".circuitbreaker"

	return transportcfg.ResolveSection(cfg, base,
		transitgrpc.DefaultCircuitBreakerConfig,
		transitgrpc.MergeCircuitBreakerConfig,
		func(isSet func(string) bool) transitgrpc.CircuitBreakerConfigOverrides {
			return transitgrpc.CircuitBreakerConfigOverrides{
				FailureThreshold:    isSet(base + ".failure_threshold"),
				Cooldown:            isSet(base + ".cooldown"),
				HalfOpenMaxRequests: isSet(base + ".half_open_max_requests"),
			}
		})
}

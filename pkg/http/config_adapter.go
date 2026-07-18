package http

import (
	"context"
	"net/http"

	"gitlab.com/phpboyscout/go/controls"
	transithttp "gitlab.com/phpboyscout/go/transit/http"
	transporthttp "gitlab.com/phpboyscout/go/transport/http"

	"gitlab.com/phpboyscout/go/config"

	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	gtbtls "gitlab.com/phpboyscout/go-tool-base/pkg/tls"
)

// DefaultConfigPrefix is the config block an HTTP server reads its port, TLS and
// max_header_bytes from unless overridden with WithConfigPrefix.
const DefaultConfigPrefix = "server.http"

// maxPort is the highest valid TCP port number.
const maxPort = 65535

// ServerOption selects which GTB config block (and, optionally, an explicit
// port) the *FromContainable adapters read. It is distinct from the transport's
// own gitlab.com/phpboyscout/go/transport/http.ServerOption (timeouts, TLS,
// max-header-bytes); the adapters accept both — GTB options steer config
// resolution, transport options are forwarded to the constructor.
type ServerOption func(*serverConfig)

// serverConfig carries the GTB config-selection knobs: which config block to
// read, and an optional explicit port override.
type serverConfig struct {
	prefix string
	port   *int
}

// WithConfigPrefix sets the config block the server reads from (default
// "server.http"). Use it to run a second HTTP server on its own block, e.g.
// "server.admin".
func WithConfigPrefix(prefix string) ServerOption {
	return func(c *serverConfig) {
		c.prefix = prefix
	}
}

// WithPort sets the listen port explicitly, bypassing config lookup entirely.
// It has the highest precedence: it overrides both <prefix>.port and the
// server.port shared fallback.
func WithPort(port int) ServerOption {
	return func(c *serverConfig) {
		c.port = &port
	}
}

// ServerSettingsFromConfig resolves HTTP server settings from GTB config. It
// preserves the existing fallback from <prefix>.port to server.port and keeps
// max_header_bytes scoped to the selected prefix.
func ServerSettingsFromConfig(cfg config.Containable, prefix string) transporthttp.ServerSettings {
	return serverSettingsFromConfig(cfg, prefix, true)
}

// ObserveServerSettingsFromConfig binds HTTP server settings to cfg and keeps a
// typed snapshot rehydrated after successful config reloads.
func ObserveServerSettingsFromConfig(
	cfg config.Containable,
	prefix string,
	opts ...config.SectionBindingOption[transporthttp.ServerSettings],
) (*config.ObservedSection[transporthttp.ServerSettings], error) {
	if prefix == "" {
		prefix = DefaultConfigPrefix
	}

	bindingOpts := make([]config.SectionBindingOption[transporthttp.ServerSettings], 0, 1+len(opts))
	bindingOpts = append(bindingOpts, config.WithSectionDefaultFunc(func(next config.Containable) transporthttp.ServerSettings {
		if next == nil {
			return transporthttp.ServerSettings{}
		}

		return transporthttp.ServerSettings{Port: next.GetInt("server.port")}
	}, mergeServerSettings))
	bindingOpts = append(bindingOpts, opts...)

	return config.ObserveSection[transporthttp.ServerSettings](cfg, prefix, bindingOpts...)
}

func serverSettingsFromConfig(cfg config.Containable, prefix string, includePort bool) transporthttp.ServerSettings {
	if prefix == "" {
		prefix = DefaultConfigPrefix
	}

	if cfg == nil {
		return transporthttp.ServerSettings{}
	}

	if _, ok := cfg.(*config.Container); !ok {
		return serverSettingsFromLegacyConfig(cfg, prefix, includePort)
	}

	section, err := config.UnmarshalSection[transporthttp.ServerSettings](cfg, prefix)
	if err != nil {
		return serverSettingsFromLegacyConfig(cfg, prefix, includePort)
	}

	settings := section.Value
	if !includePort {
		settings.Port = 0
	} else if settings.Port == 0 {
		settings.Port = cfg.GetInt("server.port")
	}

	return settings
}

func mergeServerSettings(defaults, overlay transporthttp.ServerSettings) transporthttp.ServerSettings {
	if overlay.Port != 0 {
		defaults.Port = overlay.Port
	}

	if overlay.MaxHeaderBytes != 0 {
		defaults.MaxHeaderBytes = overlay.MaxHeaderBytes
	}

	return defaults
}

func serverSettingsFromLegacyConfig(cfg config.Containable, prefix string, includePort bool) transporthttp.ServerSettings {
	var settings transporthttp.ServerSettings

	if includePort {
		settings.Port = cfg.GetInt(prefix + ".port")
	}

	settings.MaxHeaderBytes = cfg.GetInt(prefix + ".max_header_bytes")

	if includePort && settings.Port == 0 {
		settings.Port = cfg.GetInt("server.port")
	}

	return settings
}

// splitServerOptions partitions the variadic into GTB config-selection options
// and transport ServerOptions, resolving the config block and port override.
func splitServerOptions(opts []any) (serverConfig, []transporthttp.ServerOption) {
	var sc serverConfig

	var transportOpts []transporthttp.ServerOption

	for _, o := range opts {
		switch v := o.(type) {
		case ServerOption:
			v(&sc)
		case transporthttp.ServerOption:
			transportOpts = append(transportOpts, v)
		}
	}

	return sc, transportOpts
}

// resolveSettings turns the config block + port override into typed settings.
// An explicit WithPort wins; otherwise the port is read from config.
func resolveSettings(cfg config.Containable, sc serverConfig) transporthttp.ServerSettings {
	prefix := sc.prefix
	if prefix == "" {
		prefix = DefaultConfigPrefix
	}

	settings := serverSettingsFromConfig(cfg, prefix, sc.port == nil)
	if sc.port != nil {
		settings.Port = *sc.port
	}

	return settings
}

// NewServerFromContainable returns a new preconfigured http.Server. With no
// options it reads from the default "server.http" config prefix; pass a GTB
// WithConfigPrefix/WithPort to select the config block, and transport
// ServerOption values (timeouts, TLS) to configure the server.
func NewServerFromContainable(ctx context.Context, cfg config.Containable, handler http.Handler, opts ...any) (*http.Server, error) {
	sc, transportOpts := splitServerOptions(opts)

	if sc.port != nil && (*sc.port < 0 || *sc.port > maxPort) {
		return transporthttp.NewServer(ctx, transporthttp.ServerSettings{}, handler, transportOpts...)
	}

	return transporthttp.NewServer(ctx, resolveSettings(cfg, sc), handler, transportOpts...)
}

// StartFromContainable returns a curried function suitable for use with the
// controls package. With no options it reads TLS from the default "server.http"
// config prefix; pass WithConfigPrefix to match a server on a custom prefix.
func StartFromContainable(cfg config.Containable, log logger.Logger, srv *http.Server, opts ...any) controls.StartFunc {
	sc, _ := splitServerOptions(opts)

	prefix := sc.prefix
	if prefix == "" {
		prefix = DefaultConfigPrefix
	}

	return transporthttp.StartWithTLSPair(logger.ToSlog(log), srv, gtbtls.Resolve(cfg, prefix+".tls"))
}

// RegisterFromContainable creates a new HTTP server and registers it with the
// controller under the given id. The opts variadic accepts GTB config-selection
// options (WithConfigPrefix, WithPort) plus transport ServerOption and
// RegisterOption values (timeouts, middleware, body limit).
func RegisterFromContainable(ctx context.Context, id string, controller controls.Controllable, cfg config.Containable, log logger.Logger, handler http.Handler, opts ...any) (*http.Server, error) {
	var sc serverConfig

	var registerOpts []any

	for _, o := range opts {
		switch v := o.(type) {
		case ServerOption:
			v(&sc)
		default:
			// transport ServerOption / RegisterOption values are forwarded to
			// the transport Register, which accepts the same `...any` families.
			registerOpts = append(registerOpts, v)
		}
	}

	if sc.port != nil && (*sc.port < 0 || *sc.port > maxPort) {
		return transporthttp.Register(ctx, id, controller, logger.ToSlog(log), handler, transporthttp.ServerSettings{}, gtbtls.Pair{}, registerOpts...)
	}

	prefix := sc.prefix
	if prefix == "" {
		prefix = DefaultConfigPrefix
	}

	settings := resolveSettings(cfg, sc)
	tlsPair := gtbtls.Resolve(cfg, prefix+".tls")

	return transporthttp.Register(ctx, id, controller, logger.ToSlog(log), handler, settings, tlsPair, registerOpts...)
}

// RateLimitConfigFromConfig builds a transithttp.RateLimitConfig from the config layer
// under "<prefix>.ratelimit.*" (prefix defaults to "server.http"), so
// operators tune the limiter via config like they tune the port or TLS.
//
// Unset keys keep their transithttp.DefaultRateLimitConfig values. The code-only fields
// (KeyFunc, OnLimited) are never read from config; wiring stays explicit.
func RateLimitConfigFromConfig(cfg config.Containable, prefix string) transithttp.RateLimitConfig {
	if prefix == "" {
		prefix = DefaultConfigPrefix
	}

	if cfg == nil {
		return transithttp.DefaultRateLimitConfig()
	}

	base := prefix + ".ratelimit"

	section, err := config.UnmarshalSection[transithttp.RateLimitConfig](cfg, base)
	if err != nil || !section.Exists {
		return transithttp.DefaultRateLimitConfig()
	}

	return transithttp.MergeRateLimitConfig(transithttp.DefaultRateLimitConfig(), section.Value, transithttp.RateLimitConfigOverrides{
		RequestsPerSecond: cfg.IsSet(base + ".requests_per_second"),
		Burst:             cfg.IsSet(base + ".burst"),
		MaxTrackedKeys:    cfg.IsSet(base + ".max_tracked_keys"),
	})
}

// CircuitBreakerConfigFromConfig builds a transithttp.CircuitBreakerConfig from the config
// layer under "<prefix>.circuitbreaker.*" (prefix defaults to "server.http").
//
// Unset keys keep their transithttp.DefaultCircuitBreakerConfig values. The code-only
// fields (IsFailure, OnStateChange) are never read from config.
func CircuitBreakerConfigFromConfig(cfg config.Containable, prefix string) transithttp.CircuitBreakerConfig {
	if prefix == "" {
		prefix = DefaultConfigPrefix
	}

	if cfg == nil {
		return transithttp.DefaultCircuitBreakerConfig()
	}

	base := prefix + ".circuitbreaker"

	section, err := config.UnmarshalSection[transithttp.CircuitBreakerConfig](cfg, base)
	if err != nil || !section.Exists {
		return transithttp.DefaultCircuitBreakerConfig()
	}

	return transithttp.MergeCircuitBreakerConfig(transithttp.DefaultCircuitBreakerConfig(), section.Value, transithttp.CircuitBreakerConfigOverrides{
		FailureThreshold:    cfg.IsSet(base + ".failure_threshold"),
		Cooldown:            cfg.IsSet(base + ".cooldown"),
		HalfOpenMaxRequests: cfg.IsSet(base + ".half_open_max_requests"),
	})
}

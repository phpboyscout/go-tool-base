package http

import (
	"context"
	"net/http"

	"gitlab.com/phpboyscout/go-tool-base/pkg/config"
	"gitlab.com/phpboyscout/go-tool-base/pkg/controls"
	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	gtbtls "gitlab.com/phpboyscout/go-tool-base/pkg/tls"
)

// ServerSettingsFromConfig resolves HTTP server settings from GTB config. It
// preserves the existing fallback from <prefix>.port to server.port and keeps
// max_header_bytes scoped to the selected prefix.
func ServerSettingsFromConfig(cfg config.Containable, prefix string) ServerSettings {
	return serverSettingsFromConfig(cfg, prefix, true, true)
}

// ObserveServerSettingsFromConfig binds HTTP server settings to cfg and keeps a
// typed snapshot rehydrated after successful config reloads.
func ObserveServerSettingsFromConfig(
	cfg config.Containable,
	prefix string,
	opts ...config.SectionBindingOption[ServerSettings],
) (*config.ObservedSection[ServerSettings], error) {
	if prefix == "" {
		prefix = DefaultConfigPrefix
	}

	bindingOpts := make([]config.SectionBindingOption[ServerSettings], 0, 1+len(opts))
	bindingOpts = append(bindingOpts, config.WithSectionDefaultFunc(func(next config.Containable) ServerSettings {
		if next == nil {
			return ServerSettings{}
		}

		return ServerSettings{Port: next.GetInt("server.port")}
	}, mergeServerSettings))
	bindingOpts = append(bindingOpts, opts...)

	return config.ObserveSection[ServerSettings](cfg, prefix, bindingOpts...)
}

func serverSettingsFromConfig(cfg config.Containable, prefix string, includePort, includeMaxHeaderBytes bool) ServerSettings {
	if prefix == "" {
		prefix = DefaultConfigPrefix
	}

	if cfg == nil {
		return ServerSettings{}
	}

	if _, ok := cfg.(*config.Container); !ok {
		return serverSettingsFromLegacyConfig(cfg, prefix, includePort, includeMaxHeaderBytes)
	}

	section, err := config.UnmarshalSection[ServerSettings](cfg, prefix)
	if err != nil {
		return serverSettingsFromLegacyConfig(cfg, prefix, includePort, includeMaxHeaderBytes)
	}

	settings := section.Value
	if !includePort {
		settings.Port = 0
	} else if settings.Port == 0 {
		settings.Port = cfg.GetInt("server.port")
	}

	if !includeMaxHeaderBytes {
		settings.MaxHeaderBytes = 0
	}

	return settings
}

func mergeServerSettings(defaults, overlay ServerSettings) ServerSettings {
	if overlay.Port != 0 {
		defaults.Port = overlay.Port
	}

	if overlay.MaxHeaderBytes != 0 {
		defaults.MaxHeaderBytes = overlay.MaxHeaderBytes
	}

	return defaults
}

func serverSettingsFromLegacyConfig(cfg config.Containable, prefix string, includePort, includeMaxHeaderBytes bool) ServerSettings {
	var settings ServerSettings

	if includePort {
		settings.Port = cfg.GetInt(prefix + ".port")
	}

	if includeMaxHeaderBytes {
		settings.MaxHeaderBytes = cfg.GetInt(prefix + ".max_header_bytes")
	}

	if includePort && settings.Port == 0 {
		settings.Port = cfg.GetInt("server.port")
	}

	return settings
}

// NewServer returns a new preconfigured http.Server. With no options it reads
// from the default "server.http" config prefix; pass ServerOption values such
// as WithConfigPrefix or WithPort to run multiple independent servers.
func NewServerFromContainable(ctx context.Context, cfg config.Containable, handler http.Handler, opts ...ServerOption) (*http.Server, error) {
	sc := defaultServerConfig()
	for _, o := range opts {
		o(&sc)
	}

	if sc.prefix == "" {
		return newServer(ctx, ServerSettings{}, handler, sc)
	}

	if sc.port != nil && (*sc.port < 0 || *sc.port > maxPort) {
		return newServer(ctx, ServerSettings{}, handler, sc)
	}

	return newServer(ctx, serverSettingsFromConfig(cfg, sc.prefix, sc.port == nil, sc.maxHeaderBytes == 0), handler, sc)
}

// Start returns a curried function suitable for use with the controls package.
// With no options it reads TLS from the default "server.http" config prefix;
// pass WithConfigPrefix to match a server constructed on a custom prefix.
func StartFromContainable(cfg config.Containable, logger logger.Logger, srv *http.Server, opts ...ServerOption) controls.StartFunc {
	sc := defaultServerConfig()
	for _, o := range opts {
		o(&sc)
	}

	if sc.prefix == "" {
		sc.prefix = DefaultConfigPrefix
	}

	return start(logger, srv, gtbtls.Resolve(cfg, sc.prefix+".tls"), nil)
}

// Register creates a new HTTP server and registers it with the controller under
// the given id. The opts variadic accepts both ServerOption values (port,
// prefix, timeouts) and RegisterOption values (middleware, body limit) — other
// types are ignored. This mirrors the pkg/grpc Register signature.
func RegisterFromContainable(ctx context.Context, id string, controller controls.Controllable, cfg config.Containable, logger logger.Logger, handler http.Handler, opts ...any) (*http.Server, error) {
	rc := registerConfig{
		serverConfig:        defaultServerConfig(),
		maxRequestBodyBytes: DefaultMaxRequestBodyBytes,
	}

	for _, o := range opts {
		switch v := o.(type) {
		case ServerOption:
			v(&rc.serverConfig)
		case RegisterOption:
			v(&rc)
		}
	}

	if rc.prefix == "" {
		return register(ctx, id, controller, logger, handler, ServerSettings{}, gtbtls.Pair{}, rc)
	}

	if rc.port != nil && (*rc.port < 0 || *rc.port > maxPort) {
		return register(ctx, id, controller, logger, handler, ServerSettings{}, gtbtls.Pair{}, rc)
	}

	settings := serverSettingsFromConfig(cfg, rc.prefix, rc.port == nil, rc.maxHeaderBytes == 0)
	tlsPair := gtbtls.Resolve(cfg, rc.prefix+".tls")

	return register(ctx, id, controller, logger, handler, settings, tlsPair, rc)
}

// RateLimitConfigFromConfig builds a RateLimitConfig from the config layer
// under "<prefix>.ratelimit.*" (prefix defaults to "server.http"), so
// operators tune the limiter via config like they tune the port or TLS.
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
// layer under "<prefix>.circuitbreaker.*" (prefix defaults to "server.http").
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

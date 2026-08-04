package http

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"gitlab.com/phpboyscout/go/controls"
	"gitlab.com/phpboyscout/go/errors"
	transithttp "gitlab.com/phpboyscout/go/transit/http"
	transporthttp "gitlab.com/phpboyscout/go/transport/http"

	"gitlab.com/phpboyscout/go/config"

	"gitlab.com/phpboyscout/go-tool-base/internal/transportcfg"
	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	gtbtls "gitlab.com/phpboyscout/go-tool-base/pkg/tls"
)

// DefaultConfigPrefix is the config block an HTTP server reads its port, TLS and
// max_header_bytes from unless overridden with WithConfigPrefix.
const DefaultConfigPrefix = "server.http"

// ServerOption selects which GTB config block (and, optionally, an explicit
// port) the *FromReader adapters read. It is distinct from the transport's
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
func ServerSettingsFromConfig(cfg config.Reader, prefix string) transporthttp.ServerSettings {
	return serverSettingsFromConfig(cfg, prefix, true)
}

// ObserveServerSettingsFromConfig binds HTTP server settings to cfg and keeps a
// typed snapshot rehydrated after successful config reloads.
func ObserveServerSettingsFromConfig(
	cfg config.Binder,
	prefix string,
	opts ...config.SectionBindingOption[transporthttp.ServerSettings],
) (*config.ObservedSection[transporthttp.ServerSettings], error) {
	if prefix == "" {
		prefix = DefaultConfigPrefix
	}

	bindingOpts := make([]config.SectionBindingOption[transporthttp.ServerSettings], 0, 1+len(opts))
	bindingOpts = append(bindingOpts, config.WithSectionDefaultFunc(func(next config.Observed) transporthttp.ServerSettings {
		if next == nil {
			return transporthttp.ServerSettings{}
		}

		return transporthttp.ServerSettings{Port: next.GetInt("server.port")}
	}, mergeServerSettings))
	bindingOpts = append(bindingOpts, opts...)

	return config.ObserveSection[transporthttp.ServerSettings](cfg, prefix, bindingOpts...)
}

func serverSettingsFromConfig(cfg config.Reader, prefix string, includePort bool) transporthttp.ServerSettings {
	if prefix == "" {
		prefix = DefaultConfigPrefix
	}

	if cfg == nil {
		return transporthttp.ServerSettings{}
	}

	// A malformed section (e.g. a non-numeric port) fails the typed decode;
	// fall back to per-key reads, which yield zero values for the bad keys.
	section, err := config.UnmarshalSection[transporthttp.ServerSettings](cfg, prefix)
	if err != nil {
		return serverSettingsFromFlatKeys(cfg, prefix, includePort)
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

	if overlay.Host != "" {
		defaults.Host = overlay.Host
	}

	if overlay.MaxHeaderBytes != 0 {
		defaults.MaxHeaderBytes = overlay.MaxHeaderBytes
	}

	return defaults
}

func serverSettingsFromFlatKeys(cfg config.Reader, prefix string, includePort bool) transporthttp.ServerSettings {
	var settings transporthttp.ServerSettings

	if includePort {
		settings.Port = cfg.GetInt(prefix + ".port")
	}

	settings.Host = cfg.GetString(prefix + ".host")
	settings.MaxHeaderBytes = cfg.GetInt(prefix + ".max_header_bytes")

	if includePort && settings.Port == 0 {
		settings.Port = cfg.GetInt("server.port")
	}

	return settings
}

// splitServerOptions partitions the variadic into GTB config-selection options
// and transport ServerOptions, resolving the config block and port override.
// Any value outside those two families is returned in unknown so the caller can
// reject it (constructors with an error return) or warn (surfaces without one)
// rather than silently discarding an option — a dropped auth/middleware chain is
// a security footgun.
func splitServerOptions(opts []any) (sc serverConfig, transportOpts []transporthttp.ServerOption, unknown []any) {
	for _, o := range opts {
		switch v := o.(type) {
		case ServerOption:
			v(&sc)
		case transporthttp.ServerOption:
			transportOpts = append(transportOpts, v)
		default:
			unknown = append(unknown, v)
		}
	}

	return sc, transportOpts, unknown
}

// unknownOptionTypes formats the concrete types of unrecognised option values
// for an error or warning message.
func unknownOptionTypes(unknown []any) string {
	types := make([]string, 0, len(unknown))
	for _, u := range unknown {
		types = append(types, fmt.Sprintf("%T", u))
	}

	return strings.Join(types, ", ")
}

// resolveSettings turns the config block into typed settings. An explicit GTB
// WithPort is not baked in here: it is forwarded to the transport constructor as
// transporthttp.WithPort so the transport's port-range validation fires (a
// typo'd port must be a hard error, not a silent ephemeral bind).
func resolveSettings(cfg config.Reader, sc serverConfig) transporthttp.ServerSettings {
	prefix := sc.prefix
	if prefix == "" {
		prefix = DefaultConfigPrefix
	}

	return serverSettingsFromConfig(cfg, prefix, sc.port == nil)
}

// portOverrideOption forwards an explicit GTB WithPort as a transport ServerOption
// so it flows through the transport's validated resolvePort. Returns nil when no
// explicit port was set.
func portOverrideOption(sc serverConfig) transporthttp.ServerOption {
	if sc.port == nil {
		return nil
	}

	return transporthttp.WithPort(*sc.port)
}

// NewServerFromReader returns a new preconfigured http.Server. With no
// options it reads from the default "server.http" config prefix; pass a GTB
// WithConfigPrefix/WithPort to select the config block, and transport
// ServerOption values (timeouts, TLS) to configure the server.
func NewServerFromReader(ctx context.Context, cfg config.Reader, handler http.Handler, opts ...any) (*http.Server, error) {
	sc, transportOpts, unknown := splitServerOptions(opts)
	if len(unknown) > 0 {
		return nil, errors.Newf("http: unsupported server option type(s): %s", unknownOptionTypes(unknown))
	}

	// An explicit port is forwarded as a transport ServerOption so the
	// transport's validation fires — a typo'd port must be a hard error, not a
	// silent ephemeral (:0) bind nobody can find.
	if po := portOverrideOption(sc); po != nil {
		transportOpts = append(transportOpts, po)
	}

	return transporthttp.NewServer(ctx, resolveSettings(cfg, sc), handler, transportOpts...)
}

// StartFromReader returns a curried function suitable for use with the
// controls package. With no options it reads TLS from the default "server.http"
// config prefix; pass WithConfigPrefix to match a server on a custom prefix.
func StartFromReader(cfg config.Reader, log logger.Logger, srv *http.Server, opts ...any) controls.StartFunc {
	sc, _, unknown := splitServerOptions(opts)
	if len(unknown) > 0 {
		log.Warn("http: ignoring unsupported server option type(s)", "types", unknownOptionTypes(unknown))
	}

	prefix := sc.prefix
	if prefix == "" {
		prefix = DefaultConfigPrefix
	}

	return transporthttp.StartWithTLSPair(logger.ToSlog(log), srv, gtbtls.Resolve(cfg, prefix+".tls"))
}

// RegisterFromReader creates a new HTTP server and registers it with the
// controller under the given id. The opts variadic accepts GTB config-selection
// options (WithConfigPrefix, WithPort) plus transport ServerOption and
// RegisterOption values (timeouts, middleware, body limit).
func RegisterFromReader(ctx context.Context, id string, controller controls.Controllable, cfg config.Reader, log logger.Logger, handler http.Handler, opts ...any) (*http.Server, error) {
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

	prefix := sc.prefix
	if prefix == "" {
		prefix = DefaultConfigPrefix
	}

	// An explicit port is forwarded to the transport Register as a validated
	// ServerOption — an out-of-range value is rejected there, not turned into a
	// silent ephemeral (:0) bind.
	if po := portOverrideOption(sc); po != nil {
		registerOpts = append(registerOpts, po)
	}

	settings := resolveSettings(cfg, sc)
	tlsPair := gtbtls.Resolve(cfg, prefix+".tls")

	return transporthttp.Register(ctx, id, controller, logger.ToSlog(log), handler, settings,
		append(registerOpts, transporthttp.WithTLSPair(tlsPair))...)
}

// RateLimitConfigFromConfig builds a transithttp.RateLimitConfig from the config layer
// under "<prefix>.ratelimit.*" (prefix defaults to "server.http"), so
// operators tune the limiter via config like they tune the port or TLS.
//
// Unset keys keep their transithttp.DefaultRateLimitConfig values. The code-only fields
// (KeyFunc, OnLimited) are never read from config; wiring stays explicit.
func RateLimitConfigFromConfig(cfg config.Reader, prefix string) transithttp.RateLimitConfig {
	if prefix == "" {
		prefix = DefaultConfigPrefix
	}

	base := prefix + ".ratelimit"

	return transportcfg.ResolveSection(cfg, base,
		transithttp.DefaultRateLimitConfig,
		transithttp.MergeRateLimitConfig,
		func(isSet func(string) bool) transithttp.RateLimitConfigOverrides {
			return transithttp.RateLimitConfigOverrides{
				RequestsPerSecond: isSet(base + ".requests_per_second"),
				Burst:             isSet(base + ".burst"),
				MaxTrackedKeys:    isSet(base + ".max_tracked_keys"),
			}
		})
}

// CircuitBreakerConfigFromConfig builds a transithttp.CircuitBreakerConfig from the config
// layer under "<prefix>.circuitbreaker.*" (prefix defaults to "server.http").
//
// Unset keys keep their transithttp.DefaultCircuitBreakerConfig values. The code-only
// fields (IsFailure, OnStateChange) are never read from config.
func CircuitBreakerConfigFromConfig(cfg config.Reader, prefix string) transithttp.CircuitBreakerConfig {
	if prefix == "" {
		prefix = DefaultConfigPrefix
	}

	base := prefix + ".circuitbreaker"

	return transportcfg.ResolveSection(cfg, base,
		transithttp.DefaultCircuitBreakerConfig,
		transithttp.MergeCircuitBreakerConfig,
		func(isSet func(string) bool) transithttp.CircuitBreakerConfigOverrides {
			return transithttp.CircuitBreakerConfigOverrides{
				FailureThreshold:    isSet(base + ".failure_threshold"),
				Cooldown:            isSet(base + ".cooldown"),
				HalfOpenMaxRequests: isSet(base + ".half_open_max_requests"),
			}
		})
}

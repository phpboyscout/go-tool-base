package gateway

import (
	"context"
	"net/http"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"google.golang.org/grpc"

	"gitlab.com/phpboyscout/go/controls"
	transitgrpc "gitlab.com/phpboyscout/go/transit/grpc"
	transithttp "gitlab.com/phpboyscout/go/transit/http"
	transportgateway "gitlab.com/phpboyscout/go/transport/gateway"
	transportgrpc "gitlab.com/phpboyscout/go/transport/grpc"

	"gitlab.com/phpboyscout/go-tool-base/pkg/config"
	gtbgrpc "gitlab.com/phpboyscout/go-tool-base/pkg/grpc"
	gtbhttp "gitlab.com/phpboyscout/go-tool-base/pkg/http"
	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	gtbtls "gitlab.com/phpboyscout/go-tool-base/pkg/tls"
)

// ConfigPrefix is the config block a gateway server reads (its HTTP listener
// port and TLS) when run as its own service via Register. TLS falls back to the
// shared "server.tls".
const ConfigPrefix = "server.gateway"

// RegisterFunc registers the generated gateway handlers onto the mux. It is
// re-exported from the transport gateway so call sites keep a single type.
type RegisterFunc = transportgateway.RegisterFunc

// Option configures a GTB gateway adapter: the in-process gRPC dial options, the
// grpc-gateway mux options, and an optional middleware chain over the REST
// surface. GTB owns this option type because it, not the transport, performs the
// config-driven dial.
type Option func(*options)

type options struct {
	dialOpts      []grpc.DialOption
	muxOpts       []runtime.ServeMuxOption
	middleware    transithttp.Chain
	hasMiddleware bool
}

// WithDialOptions passes extra grpc.DialOption values to the in-process
// connection the gateway dials to the local gRPC server.
func WithDialOptions(opts ...grpc.DialOption) Option {
	return func(o *options) {
		o.dialOpts = append(o.dialOpts, opts...)
	}
}

// WithMuxOptions passes grpc-gateway runtime.ServeMuxOption values to the mux.
func WithMuxOptions(opts ...runtime.ServeMuxOption) Option {
	return func(o *options) {
		o.muxOpts = append(o.muxOpts, opts...)
	}
}

// WithMiddleware wraps the gateway's REST surface with a middleware chain
// (health endpoints, when run via Register, stay outside it).
func WithMiddleware(chain transithttp.Chain) Option {
	return func(o *options) {
		o.middleware = chain
		o.hasMiddleware = true
	}
}

func newOptions(opts []Option) *options {
	o := &options{}
	for _, opt := range opts {
		opt(o)
	}

	return o
}

// dialOptions returns the dial options for the in-process connection: the OTel
// client stats handler followed by any caller-supplied dial options.
func (o *options) dialOptions() []any {
	dialOpts := make([]any, 0, len(o.dialOpts)+1)
	dialOpts = append(dialOpts, transitgrpc.OTelClientHandler())

	for _, d := range o.dialOpts {
		dialOpts = append(dialOpts, d)
	}

	return dialOpts
}

// transportOptions translates the adapter's options into transport gateway
// options (mux options and middleware; dialing is handled by the adapter).
func (o *options) transportOptions() []transportgateway.Option {
	gwOpts := []transportgateway.Option{transportgateway.WithMuxOptions(o.muxOpts...)}
	if o.hasMiddleware {
		gwOpts = append(gwOpts, transportgateway.WithMiddleware(o.middleware))
	}

	return gwOpts
}

// SettingsFromConfig resolves gateway transport settings from GTB config. The
// managed HTTP server reads server.gateway.* while the in-process gRPC dial uses
// server.grpc.* so it connects to the same local gRPC service as the rest of GTB.
func SettingsFromConfig(cfg config.Containable) transportgateway.Settings {
	return transportgateway.Settings{
		HTTP:    gtbhttp.ServerSettingsFromConfig(cfg, ConfigPrefix),
		HTTPTLS: gtbtls.Resolve(cfg, ConfigPrefix+".tls"),
		GRPC:    gtbgrpc.ServerSettingsFromConfig(cfg, gtbgrpc.DefaultConfigPrefix),
		GRPCTLS: gtbtls.Resolve(cfg, gtbgrpc.DefaultConfigPrefix+".tls"),
	}
}

// ObserveSettingsFromConfig binds gateway transport settings to cfg and keeps a
// typed snapshot rehydrated after successful config reloads.
func ObserveSettingsFromConfig(
	cfg config.Containable,
	opts ...config.SectionBindingOption[transportgateway.Settings],
) (*config.ObservedSection[transportgateway.Settings], error) {
	bindingOpts := make([]config.SectionBindingOption[transportgateway.Settings], 0, 1+len(opts))
	bindingOpts = append(bindingOpts, config.WithSectionDefaultFunc(SettingsFromConfig, mergeSettings))
	bindingOpts = append(bindingOpts, opts...)

	return config.ObserveSection[transportgateway.Settings](cfg, ConfigPrefix, bindingOpts...)
}

// mergeSettings intentionally ignores the unmarshalled overlay. Gateway settings
// are composed from multiple config prefixes (server.gateway.* and
// server.grpc.*), so a single-section UnmarshalSection of Settings cannot
// reconstruct them. The observer runs only to detect reloads; the real
// resolution is the recomposed defaults from SettingsFromConfig(next), which
// this returns verbatim.
func mergeSettings(defaults, _ transportgateway.Settings) transportgateway.Settings {
	return defaults
}

// NewFromContainable builds a grpc-gateway handler ready to mount on an existing
// HTTP server. It dials the local gRPC server (transport security matches the
// server's own config) and applies register to wire the handlers.
func NewFromContainable(ctx context.Context, cfg config.Containable, register RegisterFunc, opts ...Option) (http.Handler, error) {
	return NewFromConfig(ctx, cfg, register, opts...)
}

// NewFromConfig builds a grpc-gateway handler with GTB config resolved into
// typed transport settings before delegating to the config-free constructor.
func NewFromConfig(ctx context.Context, cfg config.Containable, register RegisterFunc, opts ...Option) (http.Handler, error) {
	o := newOptions(opts)
	settings := SettingsFromConfig(cfg)

	conn, err := transportgrpc.DialLocal(settings.GRPC, settings.GRPCTLS, o.dialOptions()...)
	if err != nil {
		return nil, err
	}

	handler, err := transportgateway.New(ctx, conn, register, o.transportOptions()...)
	if err != nil {
		_ = conn.Close()

		return nil, err
	}

	return handler, nil
}

// RegisterFromContainable runs the gateway as its own controller-managed HTTP
// server on the "server.gateway" config block (TLS falling back to the shared
// "server.tls"), dialing the local gRPC server. Pass WithMiddleware to wrap the
// REST surface with a middleware chain (health endpoints stay outside it).
func RegisterFromContainable(ctx context.Context, id string, controller controls.Controllable, cfg config.Containable, log logger.Logger, register RegisterFunc, opts ...Option) (*http.Server, error) {
	return RegisterFromConfig(ctx, id, controller, cfg, log, register, opts...)
}

// RegisterFromConfig runs the gateway with GTB config resolved into typed
// transport settings before delegating to the config-free constructor.
func RegisterFromConfig(ctx context.Context, id string, controller controls.Controllable, cfg config.Containable, log logger.Logger, register RegisterFunc, opts ...Option) (*http.Server, error) {
	o := newOptions(opts)
	settings := SettingsFromConfig(cfg)

	conn, err := transportgrpc.DialLocal(settings.GRPC, settings.GRPCTLS, o.dialOptions()...)
	if err != nil {
		return nil, err
	}

	return transportgateway.Register(
		ctx,
		id,
		controller,
		logger.ToSlog(log),
		conn,
		settings.HTTP,
		settings.HTTPTLS,
		register,
		o.transportOptions()...,
	)
}

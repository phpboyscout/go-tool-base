package gateway

import (
	"context"
	"net/http"

	"gitlab.com/phpboyscout/go-tool-base/pkg/config"
	"gitlab.com/phpboyscout/go-tool-base/pkg/controls"
	gtbgrpc "gitlab.com/phpboyscout/go-tool-base/pkg/grpc"
	gtbhttp "gitlab.com/phpboyscout/go-tool-base/pkg/http"
	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	gtbtls "gitlab.com/phpboyscout/go-tool-base/pkg/tls"
)

// SettingsFromConfig resolves gateway transport settings from GTB config. The
// managed HTTP server reads server.gateway.* while the in-process gRPC dial uses
// server.grpc.* so it connects to the same local gRPC service as the rest of GTB.
func SettingsFromConfig(cfg config.Containable) Settings {
	return Settings{
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
	opts ...config.SectionBindingOption[Settings],
) (*config.ObservedSection[Settings], error) {
	bindingOpts := make([]config.SectionBindingOption[Settings], 0, 1+len(opts))
	bindingOpts = append(bindingOpts, config.WithSectionDefaultFunc(SettingsFromConfig, mergeSettings))
	bindingOpts = append(bindingOpts, opts...)

	return config.ObserveSection[Settings](cfg, ConfigPrefix, bindingOpts...)
}

func mergeSettings(defaults, _ Settings) Settings {
	return defaults
}

func (o *options) dialOptions() []any {
	dialOpts := make([]any, 0, len(o.dialOpts)+1)
	dialOpts = append(dialOpts, gtbgrpc.OTelClientHandler())

	for _, d := range o.dialOpts {
		dialOpts = append(dialOpts, d)
	}

	return dialOpts
}

// New builds a grpc-gateway handler ready to mount on an existing HTTP server.
// It dials the local gRPC server via grpc.DialLocal (so transport security
// matches the server's own config) and applies register to wire the handlers.
//
// The returned handler is a *runtime.ServeMux (or, with WithMiddleware, that mux
// wrapped by the chain). The underlying gRPC connection lives for the process;
// it is the standard pattern for an in-process gateway.
func NewFromContainable(ctx context.Context, cfg config.Containable, register RegisterFunc, opts ...Option) (http.Handler, error) {
	return NewFromConfig(ctx, cfg, register, opts...)
}

// NewFromConfig builds a grpc-gateway handler with GTB config resolved into
// typed transport settings before delegating to the config-free constructor.
func NewFromConfig(ctx context.Context, cfg config.Containable, register RegisterFunc, opts ...Option) (http.Handler, error) {
	o := newOptions(opts)
	settings := SettingsFromConfig(cfg)

	conn, err := gtbgrpc.DialLocal(settings.GRPC, settings.GRPCTLS, o.dialOptions()...)
	if err != nil {
		return nil, err
	}

	handler, err := newWithOptions(ctx, conn, register, o)
	if err != nil {
		_ = conn.Close()

		return nil, err
	}

	return handler, nil
}

// Register runs the gateway as its own controller-managed HTTP server on the
// "server.gateway" config block (TLS falling back to the shared "server.tls"),
// dialing the local gRPC server. It is the first-class form of New, a peer of
// grpc.Register and http.Register. Pass WithMiddleware to wrap the REST surface
// with a middleware chain (health endpoints stay outside it).
func RegisterFromContainable(ctx context.Context, id string, controller controls.Controllable, cfg config.Containable, logger logger.Logger, register RegisterFunc, opts ...Option) (*http.Server, error) {
	return RegisterFromConfig(ctx, id, controller, cfg, logger, register, opts...)
}

// RegisterFromConfig runs the gateway with GTB config resolved into typed
// transport settings before delegating to the config-free constructor.
func RegisterFromConfig(ctx context.Context, id string, controller controls.Controllable, cfg config.Containable, logger logger.Logger, register RegisterFunc, opts ...Option) (*http.Server, error) {
	o := newOptions(opts)
	settings := SettingsFromConfig(cfg)

	conn, err := gtbgrpc.DialLocal(settings.GRPC, settings.GRPCTLS, o.dialOptions()...)
	if err != nil {
		return nil, err
	}

	handler, err := newWithOptions(ctx, conn, register, o)
	if err != nil {
		_ = conn.Close()

		return nil, err
	}

	httpOpts := []any{gtbhttp.WithConfigPrefix(ConfigPrefix)}
	if o.hasMiddleware {
		httpOpts = append(httpOpts, gtbhttp.WithMiddleware(o.middleware))
	}

	return gtbhttp.Register(
		ctx,
		id,
		controller,
		logger,
		handler,
		settings.HTTP,
		settings.HTTPTLS,
		httpOpts...,
	)
}

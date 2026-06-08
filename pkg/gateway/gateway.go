// Package gateway makes a grpc-gateway a first-class transport: it dials the
// local gRPC server (matching its transport security) and serves the generated
// REST handlers, either mounted on an existing HTTP server (New) or as its own
// controller-managed HTTP server on the "server.gateway" config block (Register).
package gateway

import (
	"context"
	"net/http"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"google.golang.org/grpc"

	"gitlab.com/phpboyscout/go-tool-base/pkg/config"
	"gitlab.com/phpboyscout/go-tool-base/pkg/controls"
	gtbgrpc "gitlab.com/phpboyscout/go-tool-base/pkg/grpc"
	gtbhttp "gitlab.com/phpboyscout/go-tool-base/pkg/http"
	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
)

// ConfigPrefix is the config block a gateway server reads (port, TLS) when run
// as its own service via Register. TLS falls back to the shared "server.tls".
const ConfigPrefix = "server.gateway"

// RegisterFunc registers the generated gateway handlers onto the mux, using a
// client connection to the gRPC server. It is the only gateway-specific code a
// caller writes, e.g.:
//
//	func(ctx context.Context, mux *runtime.ServeMux, conn *grpc.ClientConn) error {
//	    return widgetv1.RegisterWidgetServiceHandler(ctx, mux, conn)
//	}
type RegisterFunc func(ctx context.Context, mux *runtime.ServeMux, conn *grpc.ClientConn) error

type options struct {
	muxOpts  []runtime.ServeMuxOption
	dialOpts []grpc.DialOption
}

// Option configures the gateway.
type Option func(*options)

// WithMuxOptions passes runtime.ServeMuxOption values to the gateway mux (e.g. a
// custom error handler or header matcher).
func WithMuxOptions(opts ...runtime.ServeMuxOption) Option {
	return func(o *options) {
		o.muxOpts = append(o.muxOpts, opts...)
	}
}

// WithDialOptions passes extra grpc.DialOption values to the connection the
// gateway opens to the gRPC server (transport security is set automatically).
func WithDialOptions(opts ...grpc.DialOption) Option {
	return func(o *options) {
		o.dialOpts = append(o.dialOpts, opts...)
	}
}

// New builds a grpc-gateway handler ready to mount on an existing HTTP server.
// It dials the local gRPC server via grpc.DialLocal (so transport security
// matches the server's own config) and applies register to wire the handlers.
//
// The returned handler is a *runtime.ServeMux. The underlying gRPC connection
// lives for the process; it is the standard pattern for an in-process gateway.
func New(ctx context.Context, cfg config.Containable, register RegisterFunc, opts ...Option) (http.Handler, error) {
	o := &options{}
	for _, opt := range opts {
		opt(o)
	}

	mux := runtime.NewServeMux(o.muxOpts...)

	// Instrument the connection so the trace context propagates from the inbound
	// REST request into the gRPC call: the request and its proxied RPC share one
	// trace. A noop until telemetry.Setup installs the providers. Caller dial
	// options follow, so they can override or add to it. DialLocal takes the
	// option-typed variadic (...any), so the dial options are passed as such.
	dialOpts := make([]any, 0, len(o.dialOpts)+1)
	dialOpts = append(dialOpts, gtbgrpc.OTelClientHandler())

	for _, d := range o.dialOpts {
		dialOpts = append(dialOpts, d)
	}

	conn, err := gtbgrpc.DialLocal(cfg, dialOpts...)
	if err != nil {
		return nil, err
	}

	if err := register(ctx, mux, conn); err != nil {
		_ = conn.Close()

		return nil, err
	}

	return mux, nil
}

// Register runs the gateway as its own controller-managed HTTP server on the
// "server.gateway" config block (TLS falling back to the shared "server.tls"),
// dialing the local gRPC server. It is the first-class form of New, a peer of
// grpc.Register and http.Register.
func Register(ctx context.Context, id string, controller controls.Controllable, cfg config.Containable, logger logger.Logger, register RegisterFunc, opts ...Option) (*http.Server, error) {
	handler, err := New(ctx, cfg, register, opts...)
	if err != nil {
		return nil, err
	}

	return gtbhttp.Register(ctx, id, controller, cfg, logger, handler,
		gtbhttp.WithConfigPrefix(ConfigPrefix))
}

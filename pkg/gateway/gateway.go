package gateway

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"google.golang.org/grpc"

	"gitlab.com/phpboyscout/go-tool-base/pkg/controls"
	gtbhttp "gitlab.com/phpboyscout/go-tool-base/pkg/http"
	gtbtls "gitlab.com/phpboyscout/go-tool-base/pkg/tls"
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
	muxOpts       []runtime.ServeMuxOption
	dialOpts      []grpc.DialOption
	middleware    gtbhttp.Chain
	hasMiddleware bool
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

// WithMiddleware wraps the gateway's REST surface with an HTTP middleware chain
// (logging, security headers, rate limiting, auth, …). The gateway handler is an
// ordinary http.Handler, so the standard pkg/http Chain applies to it.
//
// On the New path the chain wraps the returned handler directly. On the Register
// path it is threaded to the managed server so health endpoints (/healthz,
// /livez, /readyz) remain outside the chain, exactly as with http.Register.
func WithMiddleware(chain gtbhttp.Chain) Option {
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

// serveMux builds the raw grpc-gateway handler (no middleware applied).
func (o *options) serveMux(ctx context.Context, conn *grpc.ClientConn, register RegisterFunc) (http.Handler, error) {
	mux := runtime.NewServeMux(o.muxOpts...)

	if err := register(ctx, mux, conn); err != nil {
		return nil, err
	}

	return mux, nil
}

func newWithOptions(ctx context.Context, conn *grpc.ClientConn, register RegisterFunc, o *options) (http.Handler, error) {
	handler, err := o.serveMux(ctx, conn, register)
	if err != nil {
		return nil, err
	}

	if o.hasMiddleware {
		return o.middleware.Then(handler), nil
	}

	return handler, nil
}

// New builds a grpc-gateway handler from an already prepared gRPC
// client connection.
func New(ctx context.Context, conn *grpc.ClientConn, register RegisterFunc, opts ...Option) (http.Handler, error) {
	o := newOptions(opts)

	return newWithOptions(ctx, conn, register, o)
}

// Register runs the gateway as its own controller-managed HTTP
// server from explicit typed HTTP settings and a prepared gRPC client
// connection.
func Register(
	ctx context.Context,
	id string,
	controller controls.Controllable,
	logger *slog.Logger,
	conn *grpc.ClientConn,
	httpSettings gtbhttp.ServerSettings,
	httpTLS gtbtls.Pair,
	register RegisterFunc,
	opts ...Option,
) (*http.Server, error) {
	o := newOptions(opts)

	handler, err := newWithOptions(ctx, conn, register, o)
	if err != nil {
		return nil, err
	}

	httpOpts := []any{}
	if o.hasMiddleware {
		httpOpts = append(httpOpts, gtbhttp.WithMiddleware(o.middleware))
	}

	return gtbhttp.Register(ctx, id, controller, logger, handler, httpSettings, httpTLS, httpOpts...)
}

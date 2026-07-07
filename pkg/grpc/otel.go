package grpc

import (
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"google.golang.org/grpc"
)

// OTelStatsHandler returns a grpc.ServerOption that installs OpenTelemetry
// instrumentation for every RPC — server spans and the standard server metrics
// (rpc.server.*) — reading whichever TracerProvider and MeterProvider are
// installed as the OTel globals (see telemetry.Setup). It is a stats handler
// rather than an interceptor because that is the instrumentation the OTel gRPC
// contrib library ships and the shape the semantic conventions are defined
// against.
//
// Pass it to Register alongside any other server options:
//
//	grpc.RegisterFromContainable(ctx, "grpc", controller, cfg, log, grpc.OTelStatsHandler())
func OTelStatsHandler(opts ...otelgrpc.Option) grpc.ServerOption {
	return grpc.StatsHandler(otelgrpc.NewServerHandler(opts...))
}

// OTelClientHandler returns a grpc.DialOption that instruments a client
// connection with OpenTelemetry: a client span per RPC, and — using the global
// propagator — the trace context injected into the outgoing metadata, so a
// downstream gRPC server continues the same trace rather than starting a new one.
// It reads the globally-installed providers (see telemetry.Setup) and is a noop
// until those are installed. The gateway applies it by default, so a REST request
// and the gRPC call it proxies land in one trace.
func OTelClientHandler(opts ...otelgrpc.Option) grpc.DialOption {
	return grpc.WithStatsHandler(otelgrpc.NewClientHandler(opts...))
}

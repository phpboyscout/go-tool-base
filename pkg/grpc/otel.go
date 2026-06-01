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
//	grpc.Register(ctx, "grpc", controller, cfg, log, grpc.OTelStatsHandler())
func OTelStatsHandler(opts ...otelgrpc.Option) grpc.ServerOption {
	return grpc.StatsHandler(otelgrpc.NewServerHandler(opts...))
}

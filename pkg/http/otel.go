package http

import (
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

// OTelMiddleware returns a Middleware that records an OpenTelemetry server span
// and the standard server metrics (http.server.*) for each request, reading
// whichever TracerProvider and MeterProvider are installed as the OTel globals
// (see telemetry.Setup). server names the span operation, identifying this
// service in the trace.
//
// It composes in a Chain like any other middleware. Put it ahead of the logging
// middleware so the request log can pick up the active span:
//
//	chain := http.NewChain(
//	    http.OTelMiddleware("macguffin"),
//	    http.LoggingMiddleware(log),
//	)
func OTelMiddleware(server string, opts ...otelhttp.Option) Middleware {
	return Middleware(otelhttp.NewMiddleware(server, opts...))
}

// Package grpc provides a gRPC server toolkit for GTB services: a server
// bootstrap that integrates with the pkg/controls lifecycle and the standard
// gRPC health protocol, plus a suite of composable server interceptors —
// authentication ([AuthInterceptor]), logging, OpenTelemetry stats handlers,
// rate limiting, and circuit breaking — and TLS credential helpers.
package grpc

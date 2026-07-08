// Package grpc provides a gRPC server toolkit for GTB services: a server
// bootstrap that integrates with the pkg/controls lifecycle and the standard
// gRPC health protocol, plus a suite of composable server interceptors —
// authentication ([AuthInterceptor]), logging, OpenTelemetry stats handlers,
// rate limiting, and circuit breaking — and TLS credential helpers.
// Servers are constructed from package-owned [ServerSettings]. GTB config
// integration lives in adapter helpers such as [ServerSettingsFromConfig],
// [ObserveServerSettingsFromConfig], [NewServerFromContainable], and
// [RegisterFromContainable], so the core constructors remain independent of the
// framework config container.
package grpc

// Package http provides an HTTP server and client toolkit for GTB services.
//
// The server side bootstraps an HTTP server that integrates with the pkg/controls
// lifecycle and exposes health, liveness, and readiness endpoints, plus composable
// server middleware: authentication ([AuthMiddleware]), rate limiting, security
// headers, OpenTelemetry, and logging.
// Servers are constructed from package-owned [ServerSettings]. GTB config
// integration lives in adapter helpers such as [ServerSettingsFromConfig],
// [ObserveServerSettingsFromConfig], [NewServerFromContainable], and
// [RegisterFromContainable], so the core constructors remain independent of the
// framework config container.
//
// The client side ([NewClient]) provides a configurable HTTP client with retry,
// rate-limiting, redirect control, and circuit-breaker middleware.
package http

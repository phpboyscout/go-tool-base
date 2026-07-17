// Package http is GTB's framework-integration layer over the extracted HTTP
// transport modules.
//
// The hardened HTTP server now lives in gitlab.com/phpboyscout/go/transport/http
// and the HTTP client in gitlab.com/phpboyscout/go/httpclient; this package binds
// them to the GTB config container. It provides the config-driven server adapters
// — [ServerSettingsFromConfig], [ObserveServerSettingsFromConfig],
// [NewServerFromContainable], [StartFromContainable], [RegisterFromContainable],
// plus [RateLimitConfigFromConfig] / [CircuitBreakerConfigFromConfig] — which read
// a service's config and call the transport constructors. The GTB config-selection
// options [WithConfigPrefix] and [WithPort] steer that resolution. It also
// re-exports the secure client factory ([NewClient]) from go/httpclient and the
// go/transit HTTP middleware.
//
// Code that wants the pure server or client API without the GTB config container
// imports go/transport/http or go/httpclient directly.
package http

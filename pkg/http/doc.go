// Package http is GTB's framework-integration layer over the extracted HTTP
// transport modules.
//
// The hardened HTTP server now lives in gitlab.com/phpboyscout/go/transport/http
// and the HTTP client in gitlab.com/phpboyscout/go/httpclient; this package binds
// them to the GTB config store. It provides the config-driven server adapters
// — [ServerSettingsFromConfig], [ObserveServerSettingsFromConfig],
// [NewServerFromReader], [StartFromReader], [RegisterFromReader],
// plus [RateLimitConfigFromConfig] / [CircuitBreakerConfigFromConfig] — which read
// a service's config and call the transport constructors. The GTB config-selection
// options [WithConfigPrefix] and [WithPort] steer that resolution. The config
// adapters name the go/transit HTTP middleware types (RateLimitConfig,
// CircuitBreakerConfig) directly.
//
// The secure client factory and HTTP middleware are no longer re-exported here:
// code that wants the pure server or client API imports
// gitlab.com/phpboyscout/go/transport/http, gitlab.com/phpboyscout/go/httpclient,
// or gitlab.com/phpboyscout/go/transit/http directly.
package http

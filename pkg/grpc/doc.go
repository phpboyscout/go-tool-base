// Package grpc is GTB's framework-integration layer over the extracted gRPC
// transport module.
//
// The hardened gRPC server now lives in gitlab.com/phpboyscout/go/transport/grpc;
// this package binds it to the GTB config container. It provides the config-driven
// adapters — [ServerSettingsFromConfig], [ObserveServerSettingsFromConfig],
// [NewServerFromContainable], [StartFromContainable], [DialLocalFromContainable],
// [RegisterFromContainable], plus [RateLimitConfigFromConfig] /
// [CircuitBreakerConfigFromConfig] — which read a service's config and call the
// transport constructors. The GTB config-selection options [WithConfigPrefix] and
// [WithPort] steer that resolution. It also re-exports the go/transit gRPC
// interceptors.
//
// Code that wants the pure server API without the GTB config container imports
// go/transport/grpc directly.
package grpc

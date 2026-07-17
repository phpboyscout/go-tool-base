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
// [WithPort] steer that resolution. The config adapters name the go/transit gRPC
// middleware types (RateLimitConfig, CircuitBreakerConfig) directly.
//
// The gRPC interceptors are no longer re-exported here: code that wants the pure
// server API imports gitlab.com/phpboyscout/go/transport/grpc or the interceptors
// from gitlab.com/phpboyscout/go/transit/grpc directly.
package grpc

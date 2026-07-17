// Package gateway is GTB's framework-integration layer over the extracted
// grpc-gateway module.
//
// The pure gateway construction now lives in
// gitlab.com/phpboyscout/go/transport/gateway; this package binds it to the GTB
// config container. It provides the config-driven adapters —
// [SettingsFromConfig], [ObserveSettingsFromConfig], [NewFromContainable],
// [NewFromConfig], [RegisterFromContainable], [RegisterFromConfig] — which resolve
// the gateway's HTTP settings and the upstream gRPC endpoint from config, dial the
// local gRPC server, and delegate to the transport constructors. The adapter owns
// its own [Option] set ([WithDialOptions], [WithMuxOptions], [WithMiddleware])
// because it, not the transport, performs the config-driven dial.
//
// Code that already has a *grpc.ClientConn imports go/transport/gateway directly.
package gateway

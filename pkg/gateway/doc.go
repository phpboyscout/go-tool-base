// Package gateway makes a grpc-gateway a first-class transport for generated
// REST handlers. Core construction accepts an already prepared gRPC client
// connection and typed HTTP server settings; GTB config integration lives in
// adapter helpers such as [SettingsFromConfig], [ObserveSettingsFromConfig],
// [NewFromContainable], and [RegisterFromContainable].
package gateway

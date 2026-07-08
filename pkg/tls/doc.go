// Package tls holds the shared TLS plumbing used across every transport in the
// framework (HTTP, gRPC and the gateway): the hardened default config, the
// typed Pair config shape with shared/per-transport resolution, and the
// client-side cert-pool helpers.
//
// Keeping it in one place decouples the http and grpc packages from each other
// and gives the gateway a single dependency. GTB config integration lives in
// [Resolve]; core TLS construction works from typed [Pair] values.
package tls

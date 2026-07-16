// Package tls is GTB's thin adapter over the standalone hardened-TLS module
// gitlab.com/phpboyscout/go/tls. It re-exports the module's core (the typed
// [Pair], the hardened [DefaultConfig], the [ClientConfig]/[CertPool] builders,
// and the pure [ResolvePair] merge) so existing GTB call sites are unchanged, and
// it retains the GTB-specific config-key adapter [Resolve] (plus [SharedPrefix])
// that materialises a [Pair] from GTB configuration — the piece that cannot live
// in the framework-free module.
package tls

import (
	cryptotls "crypto/tls"
	"crypto/x509"

	gtls "gitlab.com/phpboyscout/go/tls"
)

// SharedPrefix is the config prefix for TLS settings shared across every
// transport. A transport-specific prefix (e.g. "server.grpc.tls") overrides
// individual fields, so one certificate can serve all transports with
// per-transport overrides where needed. It is a GTB config-key convention, so it
// lives here with the [Resolve] adapter rather than in the config-agnostic module.
const SharedPrefix = "server.tls"

// Pair is the typed enabled/cert/key triple used to configure TLS for any
// transport. Re-exported from gitlab.com/phpboyscout/go/tls.
type Pair = gtls.Pair

// PairOverrides records which fields a transport-specific TLS section set, so
// [ResolvePair] can merge per-transport config without a config lookup interface.
// Re-exported from gitlab.com/phpboyscout/go/tls.
type PairOverrides = gtls.PairOverrides

// DefaultConfig returns the hardened TLS configuration shared across HTTP and
// gRPC servers and the HTTP client. Re-exported from
// gitlab.com/phpboyscout/go/tls.
func DefaultConfig() *cryptotls.Config { return gtls.DefaultConfig() }

// ResolvePair resolves TLS settings from already-materialised typed values,
// overriding individual fields of the shared pair when the transport section
// supplied them. Re-exported from gitlab.com/phpboyscout/go/tls.
func ResolvePair(shared, transport Pair, overrides PairOverrides) Pair {
	return gtls.ResolvePair(shared, transport, overrides)
}

// CertPool builds an x509 certificate pool seeded with the given PEM CA/cert
// files. Re-exported from gitlab.com/phpboyscout/go/tls.
func CertPool(caFiles ...string) (*x509.CertPool, error) { return gtls.CertPool(caFiles...) }

// ClientConfig returns a hardened client TLS config that trusts the given
// CA/cert files (or the system roots when none are given). Re-exported from
// gitlab.com/phpboyscout/go/tls.
func ClientConfig(caFiles ...string) (*cryptotls.Config, error) {
	return gtls.ClientConfig(caFiles...)
}

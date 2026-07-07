// Package tls holds the shared TLS plumbing used across every transport in the
// framework (HTTP, gRPC and the gateway): the hardened default config, the
// typed TLSPair config shape with shared/per-transport resolution, and the
// client-side cert-pool helpers. Keeping it in one place decouples the http and
// grpc packages from each other and gives the gateway a single dependency.
package tls

import (
	cryptotls "crypto/tls"
	"crypto/x509"
	"os"

	"github.com/cockroachdb/errors"
)

// SharedPrefix is the config prefix for TLS settings shared across every
// transport. A transport-specific prefix (e.g. "server.grpc.tls") overrides
// individual fields, so one certificate can serve all transports with
// per-transport overrides where needed.
const SharedPrefix = "server.tls"

// DefaultConfig returns the hardened TLS configuration shared across HTTP and
// gRPC servers and the HTTP client. It enforces TLS 1.2 minimum with curated
// AEAD cipher suites and modern curve preferences.
func DefaultConfig() *cryptotls.Config {
	return &cryptotls.Config{
		MinVersion: cryptotls.VersionTLS12,
		CipherSuites: []uint16{
			cryptotls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,
			cryptotls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
			cryptotls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,
			cryptotls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
			cryptotls.TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305,
			cryptotls.TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305,
		},
		CurvePreferences: []cryptotls.CurveID{
			cryptotls.X25519,
			cryptotls.CurveP256,
		},
	}
}

// Pair is the typed enabled/cert/key triple used to configure TLS for any
// transport. It carries struct tags so the same shape marshals to and from
// config consistently wherever it is used.
type Pair struct {
	Enabled bool   `mapstructure:"enabled" yaml:"enabled" json:"enabled"`
	Cert    string `mapstructure:"cert"    yaml:"cert"    json:"cert"`
	Key     string `mapstructure:"key"     yaml:"key"     json:"key"`
}

// PairOverrides records which fields a transport-specific TLS section set.
// ResolvePair uses it to merge per-transport config without requiring a config
// lookup interface.
type PairOverrides struct {
	Enabled bool
	Cert    bool
	Key     bool
}

// Valid reports whether TLS is enabled and both certificate paths are present.
func (p Pair) Valid() bool {
	return p.Enabled && p.Cert != "" && p.Key != ""
}

// Certificate loads the X509 key pair described by the pair.
func (p Pair) Certificate() (cryptotls.Certificate, error) {
	cert, err := cryptotls.LoadX509KeyPair(p.Cert, p.Key)
	if err != nil {
		return cryptotls.Certificate{}, errors.Wrap(err, "loading TLS certificate")
	}

	return cert, nil
}

// ServerConfig returns the hardened DefaultConfig with this pair's certificate
// loaded. Pass nextProtos to advertise ALPN protocols (e.g. "h2" for a raw gRPC
// TLS listener); when empty the config's defaults are left as-is.
func (p Pair) ServerConfig(nextProtos ...string) (*cryptotls.Config, error) {
	cert, err := p.Certificate()
	if err != nil {
		return nil, err
	}

	cfg := DefaultConfig()
	cfg.Certificates = []cryptotls.Certificate{cert}

	if len(nextProtos) > 0 {
		cfg.NextProtos = nextProtos
	}

	return cfg, nil
}

// ResolvePair resolves TLS settings from already-materialised typed values. It
// starts from the shared pair and overrides individual fields when the
// transport section explicitly supplied them.
func ResolvePair(shared Pair, transport Pair, overrides PairOverrides) Pair {
	pair := shared

	if overrides.Enabled {
		pair.Enabled = transport.Enabled
	}

	if overrides.Cert {
		pair.Cert = transport.Cert
	}

	if overrides.Key {
		pair.Key = transport.Key
	}

	return pair
}

// CertPool builds an x509 certificate pool seeded with the given PEM CA/cert
// files, so clients can trust certificates that are not in the system roots
// (self-signed or private CA). Pass the same cert files the servers present to
// share one trust anchor across gRPC, HTTP and the gateway.
func CertPool(caFiles ...string) (*x509.CertPool, error) {
	pool := x509.NewCertPool()

	for _, f := range caFiles {
		pem, err := os.ReadFile(f)
		if err != nil {
			return nil, errors.Wrapf(err, "reading CA file %q", f)
		}

		if !pool.AppendCertsFromPEM(pem) {
			return nil, errors.Newf("no certificates found in %q", f)
		}
	}

	return pool, nil
}

// ClientConfig returns a hardened client TLS config (DefaultConfig) that trusts
// the given CA/cert files via a custom pool. With no files it returns the
// default config, which trusts the system roots.
func ClientConfig(caFiles ...string) (*cryptotls.Config, error) {
	cfg := DefaultConfig()

	if len(caFiles) > 0 {
		pool, err := CertPool(caFiles...)
		if err != nil {
			return nil, err
		}

		cfg.RootCAs = pool
	}

	return cfg, nil
}

package http

import (
	"crypto/tls"

	"github.com/phpboyscout/go-tool-base/pkg/config"
)

// DefaultTLSConfig returns the hardened TLS configuration shared across
// HTTP and gRPC servers and the HTTP client. It enforces TLS 1.2 minimum
// with curated AEAD cipher suites and modern curve preferences.
func DefaultTLSConfig() *tls.Config {
	return &tls.Config{
		MinVersion: tls.VersionTLS12,
		CipherSuites: []uint16{
			tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,
			tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
			tls.TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305,
			tls.TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305,
		},
		CurvePreferences: []tls.CurveID{
			tls.X25519,
			tls.CurveP256,
		},
	}
}

// ResolveTLSConfig reads TLS configuration with cascading precedence:
// transport-specific prefix (e.g. "server.http.tls" or "server.grpc.tls")
// falls back to the shared "server.tls" prefix. This allows a single cert
// to be used by both HTTP and gRPC, with per-transport overrides when needed.
//
// Returns (enabled, certPath, keyPath).
func ResolveTLSConfig(cfg config.Containable, transportPrefix string) (bool, string, string) {
	const sharedPrefix = "server.tls"

	enabled := cfg.GetBool(sharedPrefix + ".enabled")
	cert := cfg.GetString(sharedPrefix + ".cert")
	key := cfg.GetString(sharedPrefix + ".key")

	// Transport-specific overrides
	if cfg.IsSet(transportPrefix + ".enabled") {
		enabled = cfg.GetBool(transportPrefix + ".enabled")
	}

	if cfg.IsSet(transportPrefix + ".cert") {
		cert = cfg.GetString(transportPrefix + ".cert")
	}

	if cfg.IsSet(transportPrefix + ".key") {
		key = cfg.GetString(transportPrefix + ".key")
	}

	return enabled, cert, key
}

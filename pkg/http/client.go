package http

// The secure HTTP client factory has been extracted to the standalone module
// gitlab.com/phpboyscout/go/httpclient. This file re-exports that module's
// public surface so existing GTB call sites (gtbhttp.NewClient, WithTimeout,
// WithRetry, …) are unchanged. The retry/middleware types the client options
// consume (RetryConfig, ClientChain) are re-exported from
// gitlab.com/phpboyscout/go/transit in reexport.go and are type-identical to
// the types httpclient's options expect, so the aliases below compose cleanly.

import (
	"gitlab.com/phpboyscout/go/httpclient"
)

// ClientOption configures the secure HTTP client.
type ClientOption = httpclient.ClientOption

// Re-exported client factory and options (function values forwarding to the
// module). Value aliases carry no statements, so they add no coverage surface.
var (
	// NewClient returns an *http.Client with security-focused defaults:
	// TLS 1.2 minimum, curated cipher suites, timeouts, connection limits,
	// and a redirect policy that rejects HTTPS-to-HTTP downgrades.
	NewClient = httpclient.NewClient
	// NewTransport returns a preconfigured *http.Transport with
	// security-focused defaults: curated TLS configuration, connection limits,
	// and timeouts.
	NewTransport = httpclient.NewTransport

	// WithTimeout sets the overall request timeout. Default: 30s.
	WithTimeout = httpclient.WithTimeout
	// WithMaxRedirects sets the maximum number of redirects to follow.
	// Default: 10. Set to 0 to disable redirect following entirely.
	WithMaxRedirects = httpclient.WithMaxRedirects
	// WithTLSConfig overrides the default TLS configuration.
	WithTLSConfig = httpclient.WithTLSConfig
	// WithTransport overrides the entire HTTP transport.
	WithTransport = httpclient.WithTransport
	// WithCertPool sets the root CA pool used to verify server certificates,
	// preserving the hardened default TLS configuration.
	WithCertPool = httpclient.WithCertPool
	// WithRetry enables automatic retry with exponential backoff for transient
	// failures, wiring in the transit retry transport.
	WithRetry = httpclient.WithRetry
	// WithClientMiddleware applies a client middleware chain to the client's
	// transport, wrapping the transport after retry (if configured).
	WithClientMiddleware = httpclient.WithClientMiddleware
)

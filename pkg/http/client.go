package http

import (
	"crypto/tls"
	"net"
	"net/http"
	"time"

	"github.com/cockroachdb/errors"
)

type clientConfig struct {
	timeout      time.Duration
	maxRedirects int
	tlsConfig    *tls.Config
	transport    http.RoundTripper
	retry        *RetryConfig
}

const (
	defaultTimeout               = 30 * time.Second
	defaultMaxRedirects          = 10
	defaultMaxIdleConns          = 100
	defaultMaxIdleConnsPerHost   = 10
	defaultIdleConnTimeout       = 90 * time.Second
	defaultTLSHandshakeTimeout   = 10 * time.Second
	defaultResponseHeaderTimeout = 30 * time.Second
	defaultDialTimeout           = 30 * time.Second
	defaultKeepAlive             = 30 * time.Second
)

func defaultClientConfig() *clientConfig {
	return &clientConfig{
		timeout:      defaultTimeout,
		maxRedirects: defaultMaxRedirects,
		tlsConfig:    defaultTLSConfig(),
	}
}

// ClientOption configures the secure HTTP client.
type ClientOption func(*clientConfig)

// WithTimeout sets the overall request timeout. Default: 30s.
func WithTimeout(d time.Duration) ClientOption {
	return func(cfg *clientConfig) {
		cfg.timeout = d
	}
}

// WithMaxRedirects sets the maximum number of redirects to follow. Default: 10.
// Set to 0 to disable redirect following entirely.
func WithMaxRedirects(n int) ClientOption {
	return func(cfg *clientConfig) {
		cfg.maxRedirects = n
	}
}

// WithTLSConfig overrides the default TLS configuration.
// The caller is responsible for ensuring the provided config meets
// security requirements.
func WithTLSConfig(cfg *tls.Config) ClientOption {
	return func(c *clientConfig) {
		c.tlsConfig = cfg
	}
}

// WithTransport overrides the entire HTTP transport.
// When set, transport-level options (TLS config, connection limits) are ignored.
func WithTransport(rt http.RoundTripper) ClientOption {
	return func(cfg *clientConfig) {
		cfg.transport = rt
	}
}

// NewTransport returns a preconfigured *http.Transport with security-focused
// defaults: curated TLS configuration, connection limits, and timeouts.
// If tlsCfg is nil, defaultTLSConfig() is used.
func NewTransport(tlsCfg *tls.Config) *http.Transport {
	if tlsCfg == nil {
		tlsCfg = defaultTLSConfig()
	}

	return &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		TLSClientConfig:       tlsCfg,
		MaxIdleConns:          defaultMaxIdleConns,
		MaxIdleConnsPerHost:   defaultMaxIdleConnsPerHost,
		IdleConnTimeout:       defaultIdleConnTimeout,
		TLSHandshakeTimeout:   defaultTLSHandshakeTimeout,
		ExpectContinueTimeout: 1 * time.Second,
		ResponseHeaderTimeout: defaultResponseHeaderTimeout,
		DialContext: (&net.Dialer{
			Timeout:   defaultDialTimeout,
			KeepAlive: defaultKeepAlive,
		}).DialContext,
	}
}

// NewClient returns an *http.Client with security-focused defaults:
// TLS 1.2 minimum, curated cipher suites, timeouts, connection limits,
// and redirect policy that rejects HTTPS-to-HTTP downgrades.
func NewClient(opts ...ClientOption) *http.Client {
	cfg := defaultClientConfig()
	for _, opt := range opts {
		opt(cfg)
	}

	var transport http.RoundTripper
	if cfg.transport != nil {
		transport = cfg.transport
	} else {
		transport = NewTransport(cfg.tlsConfig)
	}

	if cfg.retry != nil {
		transport = &retryTransport{next: transport, cfg: *cfg.retry}
	}

	return &http.Client{
		Timeout:       cfg.timeout,
		Transport:     transport,
		CheckRedirect: redirectPolicy(cfg.maxRedirects),
	}
}

// redirectPolicy returns a CheckRedirect function that limits the number
// of redirects and rejects HTTPS-to-HTTP downgrades.
func redirectPolicy(maxRedirects int) func(*http.Request, []*http.Request) error {
	return func(req *http.Request, via []*http.Request) error {
		if len(via) >= maxRedirects {
			return errors.Newf("stopped after %d redirects", maxRedirects)
		}

		if len(via) > 0 && via[0].URL.Scheme == "https" && req.URL.Scheme == "http" {
			return errors.New("refused redirect: HTTPS to HTTP downgrade")
		}

		return nil
	}
}

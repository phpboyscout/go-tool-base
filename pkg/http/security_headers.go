package http

import (
	"net/http"
	"strconv"
	"time"
)

// Security-header default values. Conservative defaults that are safe for the
// built-in docs/openapi surfaces and most JSON management APIs.
const (
	// defaultContentTypeOptions disables MIME-type sniffing.
	defaultContentTypeOptions = "nosniff"

	// defaultFrameOptions denies framing entirely (clickjacking defence).
	defaultFrameOptions = "DENY"

	// defaultFrameAncestorsCSP is the CSP counterpart to X-Frame-Options:
	// DENY. Set alongside X-Frame-Options for browsers that honour CSP
	// frame-ancestors in preference to the legacy header.
	defaultFrameAncestorsCSP = "frame-ancestors 'none'"

	// defaultReferrerPolicy avoids leaking the request URL to other origins.
	defaultReferrerPolicy = "no-referrer"
)

// securityHeadersConfig holds the resolved security-header settings.
type securityHeadersConfig struct {
	contentTypeOptions string
	frameOptions       string
	referrerPolicy     string

	// csp is the full Content-Security-Policy value. When empty, only the
	// frame-ancestors directive is emitted (defaultFrameAncestorsCSP) so the
	// clickjacking control is always present; a non-empty value replaces it
	// wholesale and the caller owns the complete policy.
	csp string

	// hstsMaxAge, when > 0, enables Strict-Transport-Security. HSTS is off by
	// default because it is only meaningful (and only safe) over TLS; a value
	// served over plain HTTP is ignored by browsers but advertising it on a
	// mixed deployment can wedge clients onto HTTPS for hosts that do not
	// serve it.
	hstsMaxAge        time.Duration
	hstsIncludeSubdom bool
	hstsPreload       bool
}

func defaultSecurityHeadersConfig() securityHeadersConfig {
	return securityHeadersConfig{
		contentTypeOptions: defaultContentTypeOptions,
		frameOptions:       defaultFrameOptions,
		referrerPolicy:     defaultReferrerPolicy,
	}
}

// SecurityHeadersOption configures SecurityHeadersMiddleware.
type SecurityHeadersOption func(*securityHeadersConfig)

// WithReferrerPolicy overrides the Referrer-Policy header value
// (default "no-referrer"). An empty value omits the header.
func WithReferrerPolicy(policy string) SecurityHeadersOption {
	return func(c *securityHeadersConfig) {
		c.referrerPolicy = policy
	}
}

// WithFrameOptions overrides the X-Frame-Options header value
// (default "DENY"). An empty value omits the header; the
// Content-Security-Policy frame-ancestors directive is unaffected.
func WithFrameOptions(value string) SecurityHeadersOption {
	return func(c *securityHeadersConfig) {
		c.frameOptions = value
	}
}

// WithContentTypeOptions overrides the X-Content-Type-Options header value
// (default "nosniff"). An empty value omits the header (not recommended).
func WithContentTypeOptions(value string) SecurityHeadersOption {
	return func(c *securityHeadersConfig) {
		c.contentTypeOptions = value
	}
}

// WithContentSecurityPolicy sets a full Content-Security-Policy header value,
// replacing the conservative default ("frame-ancestors 'none'"). The caller
// owns the complete policy when this option is supplied. An empty value falls
// back to the frame-ancestors-only default so the clickjacking control is
// never silently dropped.
func WithContentSecurityPolicy(policy string) SecurityHeadersOption {
	return func(c *securityHeadersConfig) {
		c.csp = policy
	}
}

// WithHSTS enables Strict-Transport-Security with the given max-age. HSTS is
// OFF by default because it is only meaningful over TLS; enable it only on a
// server that is exclusively reachable over HTTPS. A non-positive maxAge leaves
// HSTS disabled.
func WithHSTS(maxAge time.Duration, includeSubdomains, preload bool) SecurityHeadersOption {
	return func(c *securityHeadersConfig) {
		c.hstsMaxAge = maxAge
		c.hstsIncludeSubdom = includeSubdomains
		c.hstsPreload = preload
	}
}

// SecurityHeadersMiddleware returns a Middleware that sets a conservative set
// of response security headers on every request:
//
//   - X-Content-Type-Options: nosniff
//   - X-Frame-Options: DENY
//   - Content-Security-Policy: frame-ancestors 'none'
//   - Referrer-Policy: no-referrer
//
// HSTS and a full CSP are opt-in via WithHSTS and WithContentSecurityPolicy.
// HSTS is off by default because it is only meaningful over TLS.
//
// Headers are set before the wrapped handler runs so a handler that writes its
// own response (and may call WriteHeader) still emits them. A handler is free
// to override any of these by setting its own value.
//
// The built-in docs/openapi handlers apply this middleware by default; it is
// not forced onto user-supplied handlers.
func SecurityHeadersMiddleware(opts ...SecurityHeadersOption) Middleware {
	cfg := defaultSecurityHeadersConfig()
	for _, o := range opts {
		o(&cfg)
	}

	csp := cfg.csp
	if csp == "" {
		csp = defaultFrameAncestorsCSP
	}

	hstsValue := buildHSTSValue(cfg)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			h := w.Header()

			if cfg.contentTypeOptions != "" {
				h.Set("X-Content-Type-Options", cfg.contentTypeOptions)
			}

			if cfg.frameOptions != "" {
				h.Set("X-Frame-Options", cfg.frameOptions)
			}

			if cfg.referrerPolicy != "" {
				h.Set("Referrer-Policy", cfg.referrerPolicy)
			}

			h.Set("Content-Security-Policy", csp)

			if hstsValue != "" {
				h.Set("Strict-Transport-Security", hstsValue)
			}

			next.ServeHTTP(w, r)
		})
	}
}

// buildHSTSValue assembles the Strict-Transport-Security header value, or ""
// when HSTS is disabled (non-positive max-age).
func buildHSTSValue(cfg securityHeadersConfig) string {
	if cfg.hstsMaxAge <= 0 {
		return ""
	}

	seconds := int64(cfg.hstsMaxAge / time.Second)
	value := "max-age=" + strconv.FormatInt(seconds, 10)

	if cfg.hstsIncludeSubdom {
		value += "; includeSubDomains"
	}

	if cfg.hstsPreload {
		value += "; preload"
	}

	return value
}

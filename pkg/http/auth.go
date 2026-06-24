package http

import (
	"context"
	"net/http"
	"strings"

	"github.com/cockroachdb/errors"

	"gitlab.com/phpboyscout/go-tool-base/pkg/authn"
	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	"gitlab.com/phpboyscout/go-tool-base/pkg/redact"
)

// AuthOption configures AuthMiddleware.
type AuthOption func(*authConfig)

type authConfig struct {
	bearer    authn.Verifier
	apiKeyHdr string
	apiKey    authn.Verifier
	mtls      authn.CertVerifier
	authorize authn.AuthorizeFunc
	log       logger.Logger
	skip      func(*http.Request) bool
}

// WithBearerVerifier extracts a token from "Authorization: Bearer <token>" and
// verifies it with v.
func WithBearerVerifier(v authn.Verifier) AuthOption {
	return func(c *authConfig) { c.bearer = v }
}

// WithAPIKeyHeader extracts the credential from the named header (e.g.
// "X-API-Key") and verifies it with v.
func WithAPIKeyHeader(header string, v authn.Verifier) AuthOption {
	return func(c *authConfig) { c.apiKeyHdr = header; c.apiKey = v }
}

// WithMTLSVerifier authenticates the request from its verified client
// certificate when no header credential is presented. The server must be
// configured for client-cert verification (RequireAndVerifyClientCert).
func WithMTLSVerifier(v authn.CertVerifier) AuthOption {
	return func(c *authConfig) { c.mtls = v }
}

// WithAuthorize installs an authorization predicate run after verification.
func WithAuthorize(fn authn.AuthorizeFunc) AuthOption {
	return func(c *authConfig) { c.authorize = fn }
}

// WithAuthLogger sets the logger for redacted server-side auth failure logging.
func WithAuthLogger(l logger.Logger) AuthOption {
	return func(c *authConfig) { c.log = l }
}

// WithAuthSkipper skips auth for requests matching pred (e.g. an OPTIONS
// preflight or a public sub-path). Health endpoints are already outside the
// chain and need no skipper.
func WithAuthSkipper(pred func(*http.Request) bool) AuthOption {
	return func(c *authConfig) { c.skip = pred }
}

// AuthMiddleware returns a Middleware that authenticates (and optionally
// authorizes) each request, storing the verified Identity in the request context
// on success. With no verifier configured it is a construction error
// (fail-closed; never a silent pass-through).
//
// Credential precedence: a bearer token and an API-key header presented together
// are rejected as ambiguous (fail-closed). Otherwise the presented header scheme
// is used; mTLS authenticates only when no header credential is presented.
//
// On failure the middleware writes a generic 401 (with WWW-Authenticate) or 403
// and never discloses why — the specific cause is logged once at WARN with the
// credential redacted. The handler is not invoked on failure.
func AuthMiddleware(opts ...AuthOption) (Middleware, error) {
	cfg := &authConfig{}
	for _, o := range opts {
		o(cfg)
	}

	if cfg.bearer == nil && cfg.apiKey == nil && cfg.mtls == nil {
		return nil, errors.New("http: AuthMiddleware requires at least one verifier (fail-closed)")
	}

	if cfg.log == nil {
		cfg.log = logger.NewNoop()
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			cfg.serve(w, r, next)
		})
	}, nil
}

// serve runs authentication and authorization for one request.
func (cfg *authConfig) serve(w http.ResponseWriter, r *http.Request, next http.Handler) {
	if cfg.skip != nil && cfg.skip(r) {
		next.ServeHTTP(w, r)

		return
	}

	id, err := cfg.authenticate(r)
	if err != nil {
		cfg.log.Warn("authentication failed",
			"method", r.Method, "path", r.URL.Path, "error", redact.Error(err))
		cfg.writeError(w, http.StatusUnauthorized)

		return
	}

	ctx := authn.ContextWithRequestMetadata(r.Context(),
		authn.RequestMetadata{Method: r.Method, Path: r.URL.Path})
	ctx = authn.ContextWithIdentity(ctx, id)

	if cfg.authorize != nil && !cfg.authorize(ctx, id) {
		cfg.log.Warn("authorization denied",
			"method", r.Method, "path", r.URL.Path, "subject", id.Subject)
		cfg.writeError(w, http.StatusForbidden)

		return
	}

	next.ServeHTTP(w, r.WithContext(ctx))
}

// extractBearer returns the bearer token and whether the Bearer scheme was
// presented (only when a bearer verifier is configured).
func (cfg *authConfig) extractBearer(r *http.Request) (string, bool) {
	if cfg.bearer == nil {
		return "", false
	}

	return bearerToken(r.Header.Get("Authorization"))
}

// extractAPIKey returns the API-key header value and whether it was presented
// (only when an API-key verifier and header name are configured).
func (cfg *authConfig) extractAPIKey(r *http.Request) (string, bool) {
	if cfg.apiKey == nil || cfg.apiKeyHdr == "" {
		return "", false
	}

	v := r.Header.Get(cfg.apiKeyHdr)

	return v, v != ""
}

// authenticate selects the presented credential scheme and verifies it, failing
// closed on ambiguity (both bearer and API-key) and on no credential.
func (cfg *authConfig) authenticate(r *http.Request) (*authn.Identity, error) {
	bearerTok, bearerPresent := cfg.extractBearer(r)
	apiKeyVal, apiKeyPresent := cfg.extractAPIKey(r)

	switch {
	case bearerPresent && apiKeyPresent:
		return nil, errors.Wrap(authn.ErrUnauthenticated, "ambiguous credentials: bearer and api-key both presented")
	case bearerPresent:
		return cfg.bearer.Verify(r.Context(), bearerTok)
	case apiKeyPresent:
		return cfg.apiKey.Verify(r.Context(), apiKeyVal)
	case cfg.mtls != nil && r.TLS != nil && len(r.TLS.VerifiedChains) > 0:
		return cfg.mtls.VerifyCert(r.Context(), r.TLS.VerifiedChains)
	default:
		return nil, errors.Wrap(authn.ErrUnauthenticated, "no credential presented")
	}
}

func (cfg *authConfig) writeError(w http.ResponseWriter, status int) {
	if status == http.StatusUnauthorized && cfg.bearer != nil {
		w.Header().Set("WWW-Authenticate", "Bearer")
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)

	body := `{"error":"unauthorized"}`
	if status == http.StatusForbidden {
		body = `{"error":"forbidden"}`
	}

	_, _ = w.Write([]byte(body))
}

// bearerToken extracts the token from an "Authorization: Bearer <token>" header,
// reporting whether the Bearer scheme was present.
func bearerToken(header string) (string, bool) {
	const prefix = "Bearer "
	if len(header) >= len(prefix) && strings.EqualFold(header[:len(prefix)], prefix) {
		return strings.TrimSpace(header[len(prefix):]), true
	}

	return "", false
}

// IdentityFromContext returns the verified Identity set by AuthMiddleware. The
// same key is shared with the gRPC interceptor, so a handler reads identity the
// same way regardless of transport.
func IdentityFromContext(ctx context.Context) (*authn.Identity, bool) {
	return authn.IdentityFromContext(ctx)
}

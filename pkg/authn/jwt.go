package authn

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/golang-jwt/jwt/v5"
)

const (
	defaultJWTLeeway = 60 * time.Second
	maxOIDCDocBytes  = 1 << 20
)

// defaultJWTAlgorithms are the asymmetric algorithms accepted by default. "none"
// is never accepted, and HMAC ("HS*") is rejected when a JWKS is configured
// (alg-confusion defence: the public key must not be usable as an HMAC secret).
var defaultJWTAlgorithms = []string{"RS256", "RS384", "RS512", "ES256", "ES384", "ES512"}

// JWTConfig configures the JWT/OIDC bearer-token verifier.
type JWTConfig struct {
	// Issuer is the required "iss". Verification fails if it does not match.
	Issuer string
	// Audiences are the acceptable "aud" values (any-of). Empty disables the aud
	// check (logged-worthy; not recommended).
	Audiences []string
	// JWKSURL is the JSON Web Key Set endpoint (HTTPS). Signing keys are fetched
	// and cached from here. Resolved automatically by WithOIDCDiscovery.
	JWKSURL string
	// Leeway tolerates small clock skew on exp/nbf/iat. Default 60s.
	Leeway time.Duration
	// RefreshInterval bounds how often the JWKS is refetched. Default 15m.
	RefreshInterval time.Duration
	// AllowedAlgorithms restricts accepted "alg" values. Default: RS/ES 256/384/512.
	AllowedAlgorithms []string
	// HTTPClient fetches the JWKS (and the OIDC document). Defaults to a client
	// with a sane timeout. Must reach an HTTPS endpoint.
	HTTPClient *http.Client
}

// JWTOption configures NewJWTVerifier beyond the base JWTConfig.
type JWTOption func(*jwtSettings)

type jwtSettings struct {
	oidcIssuer string
}

// WithOIDCDiscovery resolves JWKSURL (and validates Issuer) from the provider's
// /.well-known/openid-configuration document at construction time. This is the
// ONLY OIDC affordance: discovery of the JWKS endpoint — no login flow, no token
// endpoint use. The discovery URL must be HTTPS.
func WithOIDCDiscovery(issuerURL string) JWTOption {
	return func(s *jwtSettings) { s.oidcIssuer = issuerURL }
}

type jwtVerifier struct {
	issuer      string
	audiences   []string
	leeway      time.Duration
	allowedAlgs []string
	cache       *jwksCache
}

// NewJWTVerifier returns a Verifier for JWT bearer tokens, fetching signing keys
// from the configured (or OIDC-discovered) JWKS endpoint. It performs an initial
// JWKS fetch so a misconfiguration fails fast.
func NewJWTVerifier(ctx context.Context, cfg JWTConfig, opts ...JWTOption) (Verifier, error) {
	var s jwtSettings
	for _, o := range opts {
		o(&s)
	}

	cfg = cfg.withDefaults()

	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: defaultJWKSFetchTimeout}
	}

	if s.oidcIssuer != "" {
		if err := resolveOIDC(ctx, client, s.oidcIssuer, &cfg); err != nil {
			return nil, err
		}
	}

	if cfg.JWKSURL == "" {
		return nil, errors.New("authn: JWT verifier requires a JWKS URL (set JWKSURL or WithOIDCDiscovery)")
	}

	if cfg.Issuer == "" {
		return nil, errors.New("authn: JWT verifier requires an Issuer")
	}

	if err := validateAlgorithms(cfg.AllowedAlgorithms); err != nil {
		return nil, err
	}

	cache, err := newJWKSCache(cfg.JWKSURL, client, cfg.RefreshInterval, defaultJWKSFetchTimeout, time.Now)
	if err != nil {
		return nil, err
	}

	// Prime the cache so a bad endpoint surfaces at construction, not first verify.
	if err := cache.fetch(ctx); err != nil {
		return nil, errors.Wrap(err, "authn: initial JWKS fetch")
	}

	return &jwtVerifier{
		issuer:      cfg.Issuer,
		audiences:   cfg.Audiences,
		leeway:      cfg.Leeway,
		allowedAlgs: cfg.AllowedAlgorithms,
		cache:       cache,
	}, nil
}

func (cfg JWTConfig) withDefaults() JWTConfig {
	if cfg.Leeway <= 0 {
		cfg.Leeway = defaultJWTLeeway
	}

	if cfg.RefreshInterval <= 0 {
		cfg.RefreshInterval = defaultJWKSRefresh
	}

	if len(cfg.AllowedAlgorithms) == 0 {
		cfg.AllowedAlgorithms = defaultJWTAlgorithms
	}

	return cfg
}

// validateAlgorithms rejects "none" and any HMAC algorithm — a JWKS verifier
// uses asymmetric keys, and accepting HS* would enable an alg-confusion attack
// where the public key is used as the HMAC secret.
func validateAlgorithms(algs []string) error {
	for _, a := range algs {
		upper := strings.ToUpper(a)
		if upper == "NONE" {
			return errors.New("authn: algorithm \"none\" is never allowed")
		}

		if strings.HasPrefix(upper, "HS") {
			return errors.Newf("authn: HMAC algorithm %q is not allowed with a JWKS (alg-confusion defence)", a)
		}
	}

	return nil
}

func (v *jwtVerifier) Verify(ctx context.Context, credential string) (*Identity, error) {
	keyFunc := func(token *jwt.Token) (any, error) {
		kid, _ := token.Header["kid"].(string)

		return v.cache.keyForKid(ctx, kid)
	}

	token, err := jwt.Parse(credential, keyFunc,
		jwt.WithValidMethods(v.allowedAlgs),
		jwt.WithIssuer(v.issuer),
		jwt.WithExpirationRequired(),
		jwt.WithLeeway(v.leeway),
	)
	if err != nil {
		return nil, errors.Wrap(ErrUnauthenticated, err.Error())
	}

	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return nil, errors.Wrap(ErrUnauthenticated, "unexpected claims type")
	}

	if !v.audienceOK(claims) {
		return nil, errors.Wrap(ErrUnauthenticated, "audience not accepted")
	}

	sub, _ := claims["sub"].(string)

	return &Identity{
		Subject: sub,
		Method:  "jwt",
		Claims:  claims,
		Scopes:  parseScopes(claims),
	}, nil
}

// audienceOK reports whether the token carries at least one accepted audience.
// With no configured audiences the check is disabled.
func (v *jwtVerifier) audienceOK(claims jwt.MapClaims) bool {
	if len(v.audiences) == 0 {
		return true
	}

	accepted := make(map[string]struct{}, len(v.audiences))
	for _, a := range v.audiences {
		accepted[a] = struct{}{}
	}

	for _, got := range audienceValues(claims["aud"]) {
		if _, ok := accepted[got]; ok {
			return true
		}
	}

	return false
}

// audienceValues normalises the "aud" claim, which may be a string or a list.
func audienceValues(raw any) []string {
	switch v := raw.(type) {
	case string:
		return []string{v}
	case []string:
		return v
	case []any:
		out := make([]string, 0, len(v))
		for _, e := range v {
			if s, ok := e.(string); ok {
				out = append(out, s)
			}
		}

		return out
	default:
		return nil
	}
}

// parseScopes extracts space-delimited "scope" (or "scp") into a slice.
func parseScopes(claims jwt.MapClaims) []string {
	if s, ok := claims["scope"].(string); ok && s != "" {
		return strings.Fields(s)
	}

	if s, ok := claims["scp"].(string); ok && s != "" {
		return strings.Fields(s)
	}

	return nil
}

// resolveOIDC fetches the issuer's discovery document and sets cfg.JWKSURL +
// cfg.Issuer. The document's issuer must match the requested issuer, and both the
// discovery URL and the advertised jwks_uri must be HTTPS.
func resolveOIDC(ctx context.Context, client *http.Client, issuerURL string, cfg *JWTConfig) error {
	u, err := url.Parse(issuerURL)
	if err != nil {
		return errors.Wrap(err, "authn: parse OIDC issuer URL")
	}

	if u.Scheme != "https" {
		return errors.Newf("authn: OIDC issuer URL must be HTTPS, got %q", u.Scheme)
	}

	docURL := strings.TrimRight(issuerURL, "/") + "/.well-known/openid-configuration"

	fetchCtx, cancel := context.WithTimeout(ctx, defaultJWKSFetchTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(fetchCtx, http.MethodGet, docURL, nil)
	if err != nil {
		return errors.Wrap(err, "authn: build OIDC discovery request")
	}

	resp, err := client.Do(req)
	if err != nil {
		return errors.Wrap(err, "authn: fetch OIDC discovery document")
	}

	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return errors.Newf("authn: OIDC discovery returned %d", resp.StatusCode)
	}

	var doc struct {
		Issuer  string `json:"issuer"`
		JWKSURI string `json:"jwks_uri"`
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxOIDCDocBytes))
	if err != nil {
		return errors.Wrap(err, "authn: read OIDC discovery document")
	}

	if err := json.Unmarshal(body, &doc); err != nil {
		return errors.Wrap(err, "authn: parse OIDC discovery document")
	}

	if doc.Issuer != issuerURL {
		return errors.Newf("authn: OIDC document issuer %q does not match %q", doc.Issuer, issuerURL)
	}

	cfg.JWKSURL = doc.JWKSURI
	cfg.Issuer = doc.Issuer

	return nil
}

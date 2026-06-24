package authn_test

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/pkg/authn"
)

const testIssuer = "https://issuer.test"

func b64(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }

func rsaJWKSJSON(kid string, pub *rsa.PublicKey) string {
	return fmt.Sprintf(`{"keys":[{"kty":"RSA","kid":%q,"n":%q,"e":%q}]}`,
		kid, b64(pub.N.Bytes()), b64(big.NewInt(int64(pub.E)).Bytes()))
}

func ecJWKSJSON(t *testing.T, kid string, pub *ecdsa.PublicKey) string {
	t.Helper()

	// Extract the raw x/y coordinates via the non-deprecated ECDH encoding
	// (0x04 || X || Y) rather than the deprecated ecdsa.PublicKey.X/Y fields.
	ek, err := pub.ECDH()
	require.NoError(t, err)

	raw := ek.Bytes()
	size := (len(raw) - 1) / 2

	return fmt.Sprintf(`{"keys":[{"kty":"EC","kid":%q,"crv":"P-256","x":%q,"y":%q}]}`,
		kid, b64(raw[1:1+size]), b64(raw[1+size:]))
}

// jwksServer serves the given JWKS at /jwks and an OIDC discovery doc pointing at
// it. Returns the TLS server and a client that trusts its certificate.
func jwksServer(t *testing.T, jwksJSON string) (*httptest.Server, *http.Client) {
	t.Helper()

	mux := http.NewServeMux()
	mux.HandleFunc("/jwks", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(jwksJSON))
	})

	srv := httptest.NewTLSServer(mux)
	t.Cleanup(srv.Close)

	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprintf(w, `{"issuer":%q,"jwks_uri":%q}`, srv.URL, srv.URL+"/jwks")
	})

	return srv, srv.Client()
}

func signRS256(t *testing.T, key *rsa.PrivateKey, kid string, claims jwt.MapClaims) string {
	t.Helper()

	tok := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	tok.Header["kid"] = kid

	s, err := tok.SignedString(key)
	require.NoError(t, err)

	return s
}

func claims(aud any, exp time.Time) jwt.MapClaims {
	return jwt.MapClaims{
		"iss":   testIssuer,
		"aud":   aud,
		"sub":   "user-123",
		"exp":   exp.Unix(),
		"scope": "api:read api:write",
	}
}

func newRSAVerifier(t *testing.T, key *rsa.PrivateKey, kid string, auds []string) authn.Verifier {
	t.Helper()

	srv, client := jwksServer(t, rsaJWKSJSON(kid, &key.PublicKey))

	v, err := authn.NewJWTVerifier(context.Background(), authn.JWTConfig{
		Issuer:     testIssuer,
		Audiences:  auds,
		JWKSURL:    srv.URL + "/jwks",
		HTTPClient: client,
	})
	require.NoError(t, err)

	return v
}

func TestJWTVerifier_ValidRS256(t *testing.T) {
	t.Parallel()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	v := newRSAVerifier(t, key, "rsa-1", []string{"api"})

	tok := signRS256(t, key, "rsa-1", claims("api", time.Now().Add(time.Hour)))
	id, err := v.Verify(context.Background(), tok)
	require.NoError(t, err)
	assert.Equal(t, "user-123", id.Subject)
	assert.Equal(t, "jwt", id.Method)
	assert.ElementsMatch(t, []string{"api:read", "api:write"}, id.Scopes)
	assert.Equal(t, testIssuer, id.Claims["iss"])
}

func TestJWTVerifier_ValidES256(t *testing.T) {
	t.Parallel()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	srv, client := jwksServer(t, ecJWKSJSON(t, "ec-1", &key.PublicKey))

	v, err := authn.NewJWTVerifier(context.Background(), authn.JWTConfig{
		Issuer: testIssuer, JWKSURL: srv.URL + "/jwks", HTTPClient: client,
	})
	require.NoError(t, err)

	tok := jwt.NewWithClaims(jwt.SigningMethodES256, claims("api", time.Now().Add(time.Hour)))
	tok.Header["kid"] = "ec-1"
	signed, err := tok.SignedString(key)
	require.NoError(t, err)

	id, err := v.Verify(context.Background(), signed)
	require.NoError(t, err)
	assert.Equal(t, "user-123", id.Subject)
}

func TestJWTVerifier_RejectsBadTokens(t *testing.T) {
	t.Parallel()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	v := newRSAVerifier(t, key, "rsa-1", []string{"api"})

	other, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	tests := map[string]string{
		"expired":        signRS256(t, key, "rsa-1", claims("api", time.Now().Add(-time.Hour))),
		"wrong issuer":   signRS256(t, key, "rsa-1", jwt.MapClaims{"iss": "https://evil.test", "aud": "api", "exp": time.Now().Add(time.Hour).Unix()}),
		"wrong audience": signRS256(t, key, "rsa-1", claims("other-api", time.Now().Add(time.Hour))),
		"unknown kid":    signRS256(t, key, "nope", claims("api", time.Now().Add(time.Hour))),
		"wrong key":      signRS256(t, other, "rsa-1", claims("api", time.Now().Add(time.Hour))),
		"garbage":        "not.a.jwt",
	}

	for name, tok := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			id, err := v.Verify(context.Background(), tok)
			require.Nil(t, id)
			require.ErrorIs(t, err, authn.ErrUnauthenticated, "%s must be rejected", name)
		})
	}
}

func TestJWTVerifier_RejectsAlgNoneAndHMAC(t *testing.T) {
	t.Parallel()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	v := newRSAVerifier(t, key, "rsa-1", nil)

	// alg "none".
	noneTok := jwt.NewWithClaims(jwt.SigningMethodNone, claims("api", time.Now().Add(time.Hour)))
	noneTok.Header["kid"] = "rsa-1"
	noneSigned, err := noneTok.SignedString(jwt.UnsafeAllowNoneSignatureType)
	require.NoError(t, err)

	_, err = v.Verify(context.Background(), noneSigned)
	require.ErrorIs(t, err, authn.ErrUnauthenticated, "alg=none must be rejected")

	// HMAC token (alg-confusion attempt) — HS256 is not an accepted method.
	hsTok := jwt.NewWithClaims(jwt.SigningMethodHS256, claims("api", time.Now().Add(time.Hour)))
	hsTok.Header["kid"] = "rsa-1"
	hsSigned, err := hsTok.SignedString([]byte("public-key-as-secret"))
	require.NoError(t, err)

	_, err = v.Verify(context.Background(), hsSigned)
	require.ErrorIs(t, err, authn.ErrUnauthenticated, "HMAC must be rejected")
}

func TestJWTVerifier_AudienceAnyOf(t *testing.T) {
	t.Parallel()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	v := newRSAVerifier(t, key, "rsa-1", []string{"api-a", "api-b"})

	// Token carrying a list aud including an accepted value.
	tok := signRS256(t, key, "rsa-1", claims([]string{"other", "api-b"}, time.Now().Add(time.Hour)))
	id, err := v.Verify(context.Background(), tok)
	require.NoError(t, err)
	assert.Equal(t, "user-123", id.Subject)
}

func TestNewJWTVerifier_ConstructionErrors(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	_, err := authn.NewJWTVerifier(ctx, authn.JWTConfig{Issuer: testIssuer})
	require.Error(t, err, "missing JWKS URL")

	_, err = authn.NewJWTVerifier(ctx, authn.JWTConfig{Issuer: testIssuer, JWKSURL: "http://insecure.test/jwks"})
	require.Error(t, err, "non-HTTPS JWKS URL")

	_, err = authn.NewJWTVerifier(ctx, authn.JWTConfig{
		Issuer: testIssuer, JWKSURL: "https://x.test/jwks", AllowedAlgorithms: []string{"none"},
	})
	require.Error(t, err, "alg none in config")

	_, err = authn.NewJWTVerifier(ctx, authn.JWTConfig{
		Issuer: testIssuer, JWKSURL: "https://x.test/jwks", AllowedAlgorithms: []string{"HS256"},
	})
	require.Error(t, err, "HMAC in config")
}

func TestNewJWTVerifier_OIDCDiscovery(t *testing.T) {
	t.Parallel()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	srv, client := jwksServer(t, rsaJWKSJSON("rsa-1", &key.PublicKey))

	v, err := authn.NewJWTVerifier(context.Background(),
		authn.JWTConfig{HTTPClient: client},
		authn.WithOIDCDiscovery(srv.URL))
	require.NoError(t, err, "discovery resolves issuer + jwks_uri")

	// The discovered issuer is the server URL, so sign with that issuer.
	tok := signRS256(t, key, "rsa-1", jwt.MapClaims{
		"iss": srv.URL, "aud": "api", "sub": "u", "exp": time.Now().Add(time.Hour).Unix(),
	})
	id, err := v.Verify(context.Background(), tok)
	require.NoError(t, err)
	assert.Equal(t, "u", id.Subject)
}

func TestNewJWTVerifier_OIDCDiscovery_Rejections(t *testing.T) {
	t.Parallel()

	// Non-HTTPS issuer.
	_, err := authn.NewJWTVerifier(context.Background(), authn.JWTConfig{},
		authn.WithOIDCDiscovery("http://insecure.test"))
	require.Error(t, err)

	// Discovery doc whose issuer does not match the requested URL.
	mux := http.NewServeMux()
	srv := httptest.NewTLSServer(mux)
	t.Cleanup(srv.Close)
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprintf(w, `{"issuer":"https://mismatch.test","jwks_uri":%q}`, srv.URL+"/jwks")
	})

	_, err = authn.NewJWTVerifier(context.Background(),
		authn.JWTConfig{HTTPClient: srv.Client()}, authn.WithOIDCDiscovery(srv.URL))
	require.Error(t, err, "issuer mismatch must be rejected")
}

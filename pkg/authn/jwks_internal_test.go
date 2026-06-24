package authn

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
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type clock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *clock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.t
}

func (c *clock) add(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

func enc(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }

func TestJWK_PublicKey_RSA(t *testing.T) {
	t.Parallel()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	good := jwk{Kty: "RSA", N: enc(key.N.Bytes()), E: enc(big.NewInt(int64(key.E)).Bytes())}
	pub, err := good.publicKey()
	require.NoError(t, err)
	assert.IsType(t, &rsa.PublicKey{}, pub)

	// Bad base64 in N / E, and an out-of-range exponent.
	_, err = jwk{Kty: "RSA", N: "!!!", E: "AQAB"}.publicKey()
	require.Error(t, err)
	_, err = jwk{Kty: "RSA", N: enc(key.N.Bytes()), E: "!!!"}.publicKey()
	require.Error(t, err)
	_, err = jwk{Kty: "RSA", N: enc(key.N.Bytes()), E: enc(big.NewInt(1).Bytes())}.publicKey()
	require.Error(t, err, "exponent < 2 rejected")
}

func TestJWK_PublicKey_EC(t *testing.T) {
	t.Parallel()

	for _, curve := range []elliptic.Curve{elliptic.P256(), elliptic.P384(), elliptic.P521()} {
		key, err := ecdsa.GenerateKey(curve, rand.Reader)
		require.NoError(t, err)

		crv := map[elliptic.Curve]string{elliptic.P256(): "P-256", elliptic.P384(): "P-384", elliptic.P521(): "P-521"}[curve]

		// Raw coords via the non-deprecated ECDH encoding (0x04 || X || Y).
		ek, err := key.ECDH()
		require.NoError(t, err)

		raw := ek.Bytes()
		size := (len(raw) - 1) / 2

		pub, err := jwk{Kty: "EC", Crv: crv, X: enc(raw[1 : 1+size]), Y: enc(raw[1+size:])}.publicKey()
		require.NoError(t, err)
		assert.IsType(t, &ecdsa.PublicKey{}, pub)
	}

	_, err := jwk{Kty: "EC", Crv: "P-999", X: "AA", Y: "AA"}.publicKey()
	require.Error(t, err, "unsupported curve")
	_, err = jwk{Kty: "EC", Crv: "P-256", X: "!!!", Y: "AA"}.publicKey()
	require.Error(t, err, "bad x")
	_, err = jwk{Kty: "EC", Crv: "P-256", X: "AA", Y: "!!!"}.publicKey()
	require.Error(t, err, "bad y")
}

func TestJWK_PublicKey_UnsupportedKty(t *testing.T) {
	t.Parallel()

	_, err := jwk{Kty: "oct"}.publicKey()
	require.Error(t, err)
}

func TestNewJWKSCache_URLValidation(t *testing.T) {
	t.Parallel()

	_, err := newJWKSCache("://bad", http.DefaultClient, time.Minute, time.Second, time.Now)
	require.Error(t, err, "unparseable URL")

	_, err = newJWKSCache("http://insecure/jwks", http.DefaultClient, time.Minute, time.Second, time.Now)
	require.Error(t, err, "non-HTTPS")
}

func tlsServer(t *testing.T, handler http.HandlerFunc) (*httptest.Server, *http.Client) {
	t.Helper()

	srv := httptest.NewTLSServer(handler)
	t.Cleanup(srv.Close)

	return srv, srv.Client()
}

func TestJWKSCache_FetchErrors(t *testing.T) {
	t.Parallel()

	manyKeys := `{"keys":[` + strings.Repeat(`{"kty":"RSA","kid":"k","n":"AA","e":"AQAB"},`, maxJWKSKeys) +
		`{"kty":"RSA","kid":"last","n":"AA","e":"AQAB"}]}`
	oversized := `{"keys":[],"pad":"` + strings.Repeat("x", maxJWKSBytes+10) + `"}`

	cases := map[string]struct {
		status int
		body   string
	}{
		"non-200":       {http.StatusInternalServerError, "{}"},
		"bad json":      {http.StatusOK, "{not json"},
		"too many keys": {http.StatusOK, manyKeys},
		"oversized":     {http.StatusOK, oversized},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			srv, client := tlsServer(t, func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			})

			c, err := newJWKSCache(srv.URL, client, time.Minute, time.Second, time.Now)
			require.NoError(t, err)
			require.Error(t, c.fetch(context.Background()))
		})
	}
}

func TestJWKSCache_SkipsUnparseableKeyKeepsRest(t *testing.T) {
	t.Parallel()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	body := fmt.Sprintf(`{"keys":[{"kty":"oct","kid":"bad"},{"kty":"RSA","kid":"good","n":%q,"e":%q}]}`,
		enc(key.N.Bytes()), enc(big.NewInt(int64(key.E)).Bytes()))

	srv, client := tlsServer(t, func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(body)) })

	c, err := newJWKSCache(srv.URL, client, time.Minute, time.Second, time.Now)
	require.NoError(t, err)
	require.NoError(t, c.fetch(context.Background()))

	pub, err := c.keyForKid(context.Background(), "good")
	require.NoError(t, err)
	assert.NotNil(t, pub)
}

func TestJWKSCache_StalenessFallbackAndRateLimit(t *testing.T) {
	t.Parallel()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	jwksJSON := fmt.Sprintf(`{"keys":[{"kty":"RSA","kid":"rsa-1","n":%q,"e":%q}]}`,
		enc(key.N.Bytes()), enc(big.NewInt(int64(key.E)).Bytes()))

	var fail atomic.Bool

	srv, client := tlsServer(t, func(w http.ResponseWriter, _ *http.Request) {
		if fail.Load() {
			w.WriteHeader(http.StatusInternalServerError)

			return
		}

		_, _ = w.Write([]byte(jwksJSON))
	})

	clk := &clock{t: time.Date(2026, 6, 24, 0, 0, 0, 0, time.UTC)}

	c, err := newJWKSCache(srv.URL, client, 15*time.Minute, time.Second, clk.now)
	require.NoError(t, err)
	require.NoError(t, c.fetch(context.Background()))

	// A miss while fresh does not refetch (covers the fresh-skip branch).
	_, err = c.keyForKid(context.Background(), "unknown")
	require.ErrorIs(t, err, ErrUnauthenticated)

	// Go stale and make the endpoint fail: a known kid falls back to the cache.
	fail.Store(true)
	clk.add(20 * time.Minute)

	pub, err := c.keyForKid(context.Background(), "rsa-1")
	require.NoError(t, err, "stale + refresh-failure falls back to cached key")
	assert.NotNil(t, pub)

	// An unknown kid now: refresh is rate-limited (just attempted), so it errors.
	_, err = c.keyForKid(context.Background(), "unknown")
	require.Error(t, err)
}

func TestAudienceValues(t *testing.T) {
	t.Parallel()

	assert.Equal(t, []string{"a"}, audienceValues("a"))
	assert.Equal(t, []string{"a", "b"}, audienceValues([]string{"a", "b"}))
	assert.Equal(t, []string{"a", "b"}, audienceValues([]any{"a", 7, "b"}), "non-strings skipped")
	assert.Nil(t, audienceValues(42))
}

func TestParseScopes(t *testing.T) {
	t.Parallel()

	assert.Equal(t, []string{"a", "b"}, parseScopes(jwt.MapClaims{"scope": "a b"}))
	assert.Equal(t, []string{"x"}, parseScopes(jwt.MapClaims{"scp": "x"}), "scp fallback")
	assert.Nil(t, parseScopes(jwt.MapClaims{}))
}

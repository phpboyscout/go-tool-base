package authn

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/cockroachdb/errors"
)

const (
	// maxJWKSBytes caps the JWKS document read from the network (defence against
	// a hostile or misconfigured endpoint).
	maxJWKSBytes = 1 << 20 // 1 MiB
	// maxJWKSKeys caps the number of keys accepted from one document.
	maxJWKSKeys = 64
	// minRefreshInterval is the floor between network fetches, so a stream of
	// unknown-kid tokens cannot hammer the JWKS endpoint.
	minRefreshInterval = 30 * time.Second

	defaultJWKSRefresh      = 15 * time.Minute
	defaultJWKSFetchTimeout = 10 * time.Second
)

// jwk is one JSON Web Key — an RSA or EC public key.
type jwk struct {
	Kty string `json:"kty"`
	Kid string `json:"kid"`
	N   string `json:"n"` // RSA modulus
	E   string `json:"e"` // RSA exponent
	Crv string `json:"crv"`
	X   string `json:"x"` // EC x coordinate
	Y   string `json:"y"` // EC y coordinate
}

type jwkSet struct {
	Keys []jwk `json:"keys"`
}

// publicKey converts a JWK into a crypto.PublicKey (RSA or EC).
func (k jwk) publicKey() (crypto.PublicKey, error) {
	switch k.Kty {
	case "RSA":
		return k.rsaKey()
	case "EC":
		return k.ecKey()
	default:
		return nil, errors.Newf("authn: unsupported JWK key type %q", k.Kty)
	}
}

func (k jwk) rsaKey() (crypto.PublicKey, error) {
	nBytes, err := base64.RawURLEncoding.DecodeString(k.N)
	if err != nil {
		return nil, errors.Wrap(err, "authn: decode RSA modulus")
	}

	eBytes, err := base64.RawURLEncoding.DecodeString(k.E)
	if err != nil {
		return nil, errors.Wrap(err, "authn: decode RSA exponent")
	}

	e := new(big.Int).SetBytes(eBytes)
	if !e.IsInt64() || e.Int64() < 2 || e.Int64() > (1<<31-1) {
		return nil, errors.New("authn: RSA exponent out of range")
	}

	return &rsa.PublicKey{N: new(big.Int).SetBytes(nBytes), E: int(e.Int64())}, nil
}

func (k jwk) ecKey() (crypto.PublicKey, error) {
	var curve elliptic.Curve

	switch k.Crv {
	case "P-256":
		curve = elliptic.P256()
	case "P-384":
		curve = elliptic.P384()
	case "P-521":
		curve = elliptic.P521()
	default:
		return nil, errors.Newf("authn: unsupported EC curve %q", k.Crv)
	}

	xBytes, err := base64.RawURLEncoding.DecodeString(k.X)
	if err != nil {
		return nil, errors.Wrap(err, "authn: decode EC x")
	}

	yBytes, err := base64.RawURLEncoding.DecodeString(k.Y)
	if err != nil {
		return nil, errors.Wrap(err, "authn: decode EC y")
	}

	return &ecdsa.PublicKey{
		Curve: curve,
		X:     new(big.Int).SetBytes(xBytes),
		Y:     new(big.Int).SetBytes(yBytes),
	}, nil
}

// jwksCache is a bounded, single-purpose cache of JWKS public keys keyed by kid.
// It refreshes on staleness or on an unknown kid, serialised and rate-limited so
// an attacker cannot drive unbounded fetches. It is NOT a general cache.
type jwksCache struct {
	url          string
	client       *http.Client
	refresh      time.Duration
	fetchTimeout time.Duration
	now          func() time.Time

	mu        sync.RWMutex
	keys      map[string]crypto.PublicKey
	fetchedAt time.Time

	refreshMu   sync.Mutex
	lastAttempt time.Time
}

func newJWKSCache(jwksURL string, client *http.Client, refresh, fetchTimeout time.Duration, now func() time.Time) (*jwksCache, error) {
	u, err := url.Parse(jwksURL)
	if err != nil {
		return nil, errors.Wrap(err, "authn: parse JWKS URL")
	}

	if u.Scheme != "https" {
		return nil, errors.Newf("authn: JWKS URL must be HTTPS, got %q", u.Scheme)
	}

	return &jwksCache{
		url:          jwksURL,
		client:       client,
		refresh:      refresh,
		fetchTimeout: fetchTimeout,
		now:          now,
		keys:         make(map[string]crypto.PublicKey),
	}, nil
}

// keyForKid returns the public key for kid, refreshing the cache (rate-limited)
// on a miss or when stale. On a refresh failure a still-valid cached key is used.
func (c *jwksCache) keyForKid(ctx context.Context, kid string) (crypto.PublicKey, error) {
	c.mu.RLock()
	key, ok := c.keys[kid]
	stale := c.now().Sub(c.fetchedAt) >= c.refresh
	c.mu.RUnlock()

	if ok && !stale {
		return key, nil
	}

	if err := c.maybeRefresh(ctx); err != nil {
		if ok {
			return key, nil // fall back to the cached key on a refresh failure
		}

		return nil, err
	}

	c.mu.RLock()
	key, ok = c.keys[kid]
	c.mu.RUnlock()

	if !ok {
		return nil, errors.Wrapf(ErrUnauthenticated, "no JWKS key for kid %q", kid)
	}

	return key, nil
}

// maybeRefresh fetches the JWKS at most once per minRefreshInterval, serialised
// across goroutines (single-flight).
func (c *jwksCache) maybeRefresh(ctx context.Context) error {
	c.refreshMu.Lock()
	defer c.refreshMu.Unlock()

	// Another goroutine may have refreshed while we waited for the lock; or we
	// fetched very recently. Either way, don't re-hit the endpoint.
	c.mu.RLock()
	fresh := c.now().Sub(c.fetchedAt) < c.refresh
	c.mu.RUnlock()

	if fresh || c.now().Sub(c.lastAttempt) < minRefreshInterval {
		return nil
	}

	c.lastAttempt = c.now()

	return c.fetch(ctx)
}

func (c *jwksCache) fetch(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, c.fetchTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.url, nil)
	if err != nil {
		return errors.Wrap(err, "authn: build JWKS request")
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return errors.Wrap(err, "authn: fetch JWKS")
	}

	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return errors.Newf("authn: JWKS endpoint returned %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxJWKSBytes+1))
	if err != nil {
		return errors.Wrap(err, "authn: read JWKS body")
	}

	if len(body) > maxJWKSBytes {
		return errors.Newf("authn: JWKS document exceeds %d bytes", maxJWKSBytes)
	}

	var set jwkSet
	if err := json.Unmarshal(body, &set); err != nil {
		return errors.Wrap(err, "authn: parse JWKS document")
	}

	if len(set.Keys) > maxJWKSKeys {
		return errors.Newf("authn: JWKS document has %d keys, max %d", len(set.Keys), maxJWKSKeys)
	}

	keys := make(map[string]crypto.PublicKey, len(set.Keys))
	for _, k := range set.Keys {
		pub, err := k.publicKey()
		if err != nil {
			continue // skip an unparseable/unsupported key, keep the rest
		}

		keys[k.Kid] = pub
	}

	c.mu.Lock()
	c.keys = keys
	c.fetchedAt = c.now()
	c.mu.Unlock()

	return nil
}

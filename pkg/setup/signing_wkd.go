package setup

import (
	"bytes"
	"context"
	"crypto/sha1"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/ProtonMail/go-crypto/openpgp"
	"github.com/cockroachdb/errors"
)

// zBase32Alphabet is the Z-Base-32 alphabet from RFC 6189 §5.1.6, used
// by the WKD draft for hashing the local part of an email address.
const zBase32Alphabet = "ybndrfg8ejkmcpqxot1uwisza345h769"

const (
	bitsPerByte          = 8
	zBase32BitsPerChar   = 5
	zBase32CharMask      = 0x1f
	wkdHashEncodedLength = sha1.Size * bitsPerByte / zBase32BitsPerChar
)

// wkdLocalPartHash returns the Z-Base-32 encoding of the SHA-1 of the
// lower-cased local part, per draft-koch-openpgp-webkey-service §3.1.
// SHA-1 is used here as a fixed identifier hash defined by the WKD
// wire format — not for cryptographic integrity. Signature verification
// uses ed25519/RSA via go-crypto and never relies on this hash.
func wkdLocalPartHash(localPart string) string {
	sum := sha1.Sum([]byte(strings.ToLower(localPart)))

	out := make([]byte, wkdHashEncodedLength)

	var (
		bits     uint32
		bitCount uint
	)

	j := 0

	for _, b := range sum {
		bits = (bits << bitsPerByte) | uint32(b)
		bitCount += bitsPerByte

		for bitCount >= zBase32BitsPerChar {
			bitCount -= zBase32BitsPerChar
			out[j] = zBase32Alphabet[(bits>>bitCount)&zBase32CharMask]
			j++
		}
	}

	return string(out)
}

// WKDURLs derives the advanced and direct WKD URLs and the canonical
// advanced host ("openpgpkey.<domain>") for an email address, per
// draft-koch-openpgp-webkey-service. The advanced URL is the preferred
// fetch target; resolvers fall back to the direct URL on 404. Returns
// an error for malformed input (missing '@', empty local or domain).
func WKDURLs(email string) (advanced, direct, advancedHost string, _ error) {
	at := strings.LastIndex(email, "@")
	if at <= 0 || at == len(email)-1 {
		return "", "", "", errors.Newf("wkd: invalid email %q", email)
	}

	local := email[:at]
	domain := strings.ToLower(email[at+1:])
	hash := wkdLocalPartHash(local)
	qs := "?l=" + url.QueryEscape(local)

	advancedHost = "openpgpkey." + domain
	advanced = "https://" + advancedHost + "/.well-known/openpgpkey/" + domain + "/hu/" + hash + qs
	direct = "https://" + domain + "/.well-known/openpgpkey/hu/" + hash + qs

	return advanced, direct, advancedHost, nil
}

// WKDResolverConfig parameterises NewWKDResolver. Email and HTTPClient
// are required; URLOverride is for tests pointing at a local TLS
// server (the canonical scheme+host is replaced, the WKD path+query
// from Email are preserved).
type WKDResolverConfig struct {
	Email       string
	HTTPClient  *http.Client
	URLOverride string
}

// wkdResolver implements KeyResolver against a Web Key Directory.
type wkdResolver struct {
	advancedURL string
	directURL   string
	client      *http.Client
	name        string
}

// NewWKDResolver returns a KeyResolver that fetches a public key from
// the Web Key Directory derived from the supplied email. The hardened
// HTTPClient is mandatory: callers should construct it via
// pkg/http.NewClient so TLS 1.2+, certificate validation, the request
// timeout, and the HTTPS-downgrade redirect policy are enforced.
//
// Resolve tries the advanced URL first and falls back to the direct
// URL only on HTTP 404 — any other failure (network, non-200, TLS,
// oversize, weak key) is returned to the caller and does not fall
// through to the direct URL.
func NewWKDResolver(cfg WKDResolverConfig) (KeyResolver, error) {
	if cfg.Email == "" {
		return nil, errors.New("wkd: email is required")
	}

	if cfg.HTTPClient == nil {
		return nil, errors.New("wkd: HTTPClient is required")
	}

	advanced, direct, canonicalHost, err := WKDURLs(cfg.Email)
	if err != nil {
		return nil, err
	}

	if cfg.URLOverride != "" {
		advanced, err = rebaseURL(advanced, cfg.URLOverride)
		if err != nil {
			return nil, err
		}

		direct, err = rebaseURL(direct, cfg.URLOverride)
		if err != nil {
			return nil, err
		}
	}

	return &wkdResolver{
		advancedURL: advanced,
		directURL:   direct,
		client:      cfg.HTTPClient,
		name:        "wkd:" + canonicalHost,
	}, nil
}

// Name returns a stable diagnostic identifier of the form
// "wkd:openpgpkey.<domain>", derived from the canonical email even
// when URLOverride is in use.
func (r *wkdResolver) Name() string {
	return r.name
}

// Resolve fetches the WKD public key, parses it as binary OpenPGP
// (WKD's wire format), and applies the trust-set minimum-strength
// policy. Returns ErrKeyResolverUnavailable for network/HTTP failures,
// ErrWKDResponseTooLarge for oversized responses, ErrWeakKey for
// rejected keys, and wraps the parser error for malformed responses.
func (r *wkdResolver) Resolve(ctx context.Context) (*TrustSet, error) {
	body, err := r.fetch(ctx, r.advancedURL)
	if errors.Is(err, errWKDNotFound) {
		body, err = r.fetch(ctx, r.directURL)
	}

	if err != nil {
		if errors.Is(err, errWKDNotFound) {
			return nil, errors.Wrap(ErrKeyResolverUnavailable, "wkd: no key at advanced or direct URL")
		}

		return nil, err
	}

	entities, err := openpgp.ReadKeyRing(bytes.NewReader(body))
	if err != nil {
		return nil, errors.Wrap(err, "wkd: parsing key response")
	}

	return loadTrustSetFromEntities(entities)
}

// errWKDNotFound is an internal sentinel used to drive the
// advanced→direct fallback. It is not exported: callers see
// ErrKeyResolverUnavailable when both URLs return 404.
//
//nolint:gochecknoglobals // internal sentinel for fallback control flow
var errWKDNotFound = errors.New("wkd: not found (404)")

func (r *wkdResolver) fetch(ctx context.Context, target string) ([]byte, error) {
	if !strings.HasPrefix(target, "https://") {
		return nil, errors.Newf("wkd: refusing non-HTTPS URL")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, errors.Wrap(ErrKeyResolverUnavailable, err.Error())
	}

	resp, err := r.client.Do(req)
	if err != nil {
		return nil, errors.Wrap(ErrKeyResolverUnavailable, err.Error())
	}

	defer func() { _ = resp.Body.Close() }()

	switch resp.StatusCode {
	case http.StatusOK:
		// fall through
	case http.StatusNotFound:
		return nil, errWKDNotFound
	default:
		return nil, errors.Wrapf(ErrKeyResolverUnavailable,
			"wkd: unexpected status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, MaxWKDResponseSize+1))
	if err != nil {
		return nil, errors.Wrap(ErrKeyResolverUnavailable, err.Error())
	}

	if int64(len(body)) > MaxWKDResponseSize {
		return nil, ErrWKDResponseTooLarge
	}

	return body, nil
}

// rebaseURL replaces the scheme+host of original with those of
// baseOverride, preserving original's path and raw query. Used by the
// WKD URLOverride test hook to point at an httptest server while
// keeping the WKD-derived path. The original URL is always
// well-formed (constructed by WKDURLs); the override comes from the
// caller and is the realistic failure point.
func rebaseURL(original, baseOverride string) (string, error) {
	orig, _ := url.Parse(original)

	base, err := url.Parse(baseOverride)
	if err != nil {
		return "", errors.Wrap(err, "wkd: parse base override")
	}

	base.Path = orig.Path
	base.RawQuery = orig.RawQuery

	return base.String(), nil
}

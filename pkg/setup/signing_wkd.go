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

	if err := validateWKDDomain(domain); err != nil {
		return "", "", "", err
	}

	hash := wkdLocalPartHash(local)
	qs := "?l=" + url.QueryEscape(local)

	advancedHost = "openpgpkey." + domain
	advanced = "https://" + advancedHost + "/.well-known/openpgpkey/" + domain + "/hu/" + hash + qs
	direct = "https://" + domain + "/.well-known/openpgpkey/hu/" + hash + qs

	return advanced, direct, advancedHost, nil
}

// validateWKDDomain rejects email domains that are not safe to splice into
// a WKD URL. It mirrors the publish-side hostname check in
// pkg/openpgpkey (validateDomain) so the resolver never derives an
// advanced/direct URL from a domain carrying a path separator, "..",
// leading/trailing dot, or any non-hostname character — values that could
// otherwise reshape the request target or escape the WKD path.
func validateWKDDomain(domain string) error {
	switch {
	case domain == "":
		return errors.New("wkd: email domain is empty")
	case strings.ContainsAny(domain, "/\\"):
		return errors.Newf("wkd: email domain %q contains a path separator", domain)
	case strings.Contains(domain, ".."):
		return errors.Newf("wkd: email domain %q contains '..'", domain)
	case strings.HasPrefix(domain, "."), strings.HasSuffix(domain, "."):
		return errors.Newf("wkd: email domain %q has a leading or trailing dot", domain)
	}

	for _, r := range domain {
		if !isWKDHostnameRune(r) {
			return errors.Newf("wkd: email domain %q contains invalid character %q", domain, r)
		}
	}

	return nil
}

// isWKDHostnameRune reports whether r is permitted in a DNS hostname label
// (letters, digits, hyphens, and the label separator '.').
func isWKDHostnameRune(r rune) bool {
	switch {
	case r >= 'a' && r <= 'z':
		return true
	case r >= 'A' && r <= 'Z':
		return true
	case r >= '0' && r <= '9':
		return true
	case r == '.' || r == '-':
		return true
	default:
		return false
	}
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
	email       string
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
		email:       cfg.Email,
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
// (WKD's wire format), filters the returned entities to those carrying a
// UID matching the resolver's email, and applies the trust-set
// minimum-strength policy. Returns ErrKeyResolverUnavailable for
// network/HTTP failures, ErrWKDResponseTooLarge for oversized responses,
// ErrWeakKey for rejected keys, and wraps the parser error for malformed
// responses.
//
// The UID-to-email filter closes the key_source=external gap: a WKD
// endpoint must not be able to inject an unrelated key (one whose UIDs do
// not name the requested address) into the trust set. The composite
// resolver already cross-checks fingerprints across sources; this filter
// gives the single-source path an equivalent guarantee.
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

	matched := filterEntitiesByEmail(entities, r.email)
	if len(matched) == 0 {
		return nil, errors.Wrapf(ErrKeyResolverUnavailable,
			"wkd: no returned key carries a UID matching %q", r.email)
	}

	return loadTrustSetFromEntities(matched)
}

// filterEntitiesByEmail returns the subset of entities that carry at least
// one User ID whose email matches want (case-insensitively). WKD responses
// are addressed by the local-part hash, but the server still controls the
// returned bytes, so a response could include keys for unrelated UIDs;
// trusting only the UID-matching subset prevents a malicious or
// misconfigured directory from smuggling an off-address key into the trust
// set.
func filterEntitiesByEmail(entities openpgp.EntityList, want string) openpgp.EntityList {
	want = strings.ToLower(strings.TrimSpace(want))

	var matched openpgp.EntityList

	for _, ent := range entities {
		if entityHasEmail(ent, want) {
			matched = append(matched, ent)
		}
	}

	return matched
}

// entityHasEmail reports whether ent has an identity whose UserId email
// equals want (already lower-cased). It falls back to scanning the raw
// UserId string when the parsed Email field is empty, covering keys whose
// UID was not split into structured fields.
func entityHasEmail(ent *openpgp.Entity, want string) bool {
	for _, ident := range ent.Identities {
		if ident.UserId == nil {
			continue
		}

		if strings.ToLower(strings.TrimSpace(ident.UserId.Email)) == want {
			return true
		}

		if ident.UserId.Email == "" &&
			strings.Contains(strings.ToLower(ident.UserId.Id), "<"+want+">") {
			return true
		}
	}

	return false
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

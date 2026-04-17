package chat

// This file implements the BaseURL validation contract for every chat
// provider that honours [Config.BaseURL]. Every chat.New call routes
// through [ValidateBaseURL] before provider construction, so a
// misconfigured endpoint fails fast with a typed error instead of
// sending credentials to an attacker-controlled host.
//
// # Threat model
//
// An operator who can influence config — tampered config file, hostile
// environment variable, compromised setup wizard — could redirect
// chat-provider traffic to an attacker-controlled HTTPS host and
// capture the Authorization header. URLs containing userinfo
// (`https://user:pass@host/`) are a related trap: some HTTP libraries
// propagate the userinfo as Basic auth, others log the full URL at
// DEBUG. Both paths are closed here by rejecting at the chat boundary.
//
// # Rejection rules
//
// Applied in order, cheapest first:
//
//  1. Length > [MaxBaseURLLength] rejected.
//  2. ASCII control characters (0x00–0x1F, 0x7F) rejected.
//  3. url.Parse failure rejected.
//  4. URL.User populated (any userinfo, including password-less)
//     rejected unconditionally — credentials belong in the Token field.
//  5. Scheme must be "https", unless the caller explicitly opts into
//     HTTP via allowInsecure (tests with httptest.Server only).
//  6. Missing host rejected.
//  7. Placeholder host (example.com, example.net, example.org,
//     localhost.localdomain) rejected — catches scaffolding that
//     never got replaced with a real endpoint.
//
// See docs/development/specs/2026-04-17-chat-baseurl-validation.md
// (M-3 from the 2026-04-17 security audit) for the full design.

import (
	"net/url"
	"strings"

	"github.com/cockroachdb/errors"
)

// MaxBaseURLLength caps the length, in bytes, of a provider BaseURL.
// Normal BaseURLs are well under 200 bytes; 2 KiB is generous for
// legitimate proxy configurations and far short of any pathological
// input.
const MaxBaseURLLength = 2048

// ErrInvalidBaseURL is returned when [Config.BaseURL] fails validation.
// Callers can distinguish validation failures from other errors via
// [errors.Is].
var ErrInvalidBaseURL = errors.New("invalid chat provider base URL")

// placeholderHosts are rejected to stop config scaffolding (e.g.
// "https://api.example.com/v1") from silently reaching the wire and
// potentially hitting a typosquatted real domain.
var placeholderHosts = map[string]bool{
	"example.com":           true,
	"example.net":           true,
	"example.org":           true,
	"localhost.localdomain": true,
}

// ValidateBaseURL returns nil if baseURL is acceptable for use as a
// chat provider endpoint, or an error wrapping [ErrInvalidBaseURL]
// otherwise.
//
// An empty baseURL is always accepted — callers that require a value
// (e.g. [ProviderOpenAICompatible]) must enforce non-emptiness
// separately. Every non-empty URL is checked against the seven
// rejection rules documented at the top of this file.
//
// Pass allowInsecure=true ONLY from tests that point at an
// [net/http/httptest.Server] (which serves HTTP). Production callers
// must leave it false; the [Config.AllowInsecureBaseURL] field that
// drives this is tagged `json:"-"` so config files cannot set it.
//
// Downstream tool authors should call this at the boundary where they
// accept BaseURL input (their own setup wizard, CLI flag, env var) so
// misconfiguration surfaces early rather than at [New] time.
func ValidateBaseURL(baseURL string, allowInsecure bool) error {
	if baseURL == "" {
		return nil
	}

	if err := checkBaseURLShape(baseURL); err != nil {
		return err
	}

	parsed, err := url.Parse(baseURL)
	if err != nil {
		// The parse error itself may echo attacker-controlled input,
		// so keep it as a wrapped-but-hint-free error; the caller's
		// surrounding context is the safer place to log.
		return errors.Wrap(ErrInvalidBaseURL, "parsing base URL")
	}

	return checkBaseURLSemantics(parsed, allowInsecure)
}

// checkBaseURLShape applies the byte-level checks (length, control
// characters) that don't need url.Parse — cheap pre-filtering.
func checkBaseURLShape(baseURL string) error {
	if len(baseURL) > MaxBaseURLLength {
		return errors.WithHintf(ErrInvalidBaseURL,
			"base URL is %d bytes; max is %d", len(baseURL), MaxBaseURLLength)
	}

	for _, r := range baseURL {
		if r < 0x20 || r == 0x7F {
			return errors.WithHint(ErrInvalidBaseURL,
				"base URL contains ASCII control characters")
		}
	}

	return nil
}

// checkBaseURLSemantics applies the structural checks that require a
// parsed URL (userinfo, scheme, host, placeholder).
func checkBaseURLSemantics(parsed *url.URL, allowInsecure bool) error {
	if parsed.User != nil {
		// Reject any userinfo — with or without password. Never log
		// the URL itself because it contains the credential.
		return errors.WithHint(ErrInvalidBaseURL,
			"base URL must not contain credentials; use the Token field instead")
	}

	if err := checkBaseURLScheme(parsed.Scheme, allowInsecure); err != nil {
		return err
	}

	if parsed.Host == "" {
		return errors.WithHint(ErrInvalidBaseURL, "base URL must include a host")
	}

	hostname := strings.ToLower(parsed.Hostname())
	if isPlaceholderHost(hostname) {
		return errors.WithHintf(ErrInvalidBaseURL,
			"base URL host %q is a placeholder; replace it with your provider's real endpoint", hostname)
	}

	return nil
}

// checkBaseURLScheme enforces https-by-default with an explicit
// opt-in to http for tests using httptest.Server.
func checkBaseURLScheme(scheme string, allowInsecure bool) error {
	switch strings.ToLower(scheme) {
	case "https":
		return nil
	case "http":
		if !allowInsecure {
			return errors.WithHint(ErrInvalidBaseURL,
				"base URL must use https; AllowInsecureBaseURL is test-only")
		}

		return nil
	default:
		return errors.WithHintf(ErrInvalidBaseURL,
			"base URL scheme %q is not supported; use https", scheme)
	}
}

// isPlaceholderHost reports whether hostname is one of the reserved
// placeholder domains — either exactly (e.g. "example.com") or as a
// subdomain (e.g. "api.example.com"). The spec explicitly calls out
// "https://api.example.com/v1" as scaffolding that must be caught.
func isPlaceholderHost(hostname string) bool {
	if placeholderHosts[hostname] {
		return true
	}

	for root := range placeholderHosts {
		if strings.HasSuffix(hostname, "."+root) {
			return true
		}
	}

	return false
}

// baseURLHost returns the hostname portion of baseURL for audit logging
// at successful provider initialisation, or "" if the URL is empty or
// does not parse. The URL path and query are never returned — they may
// carry provider-specific identifiers that should not appear in logs.
func baseURLHost(baseURL string) string {
	if baseURL == "" {
		return ""
	}

	parsed, err := url.Parse(baseURL)
	if err != nil {
		return ""
	}

	return parsed.Hostname()
}

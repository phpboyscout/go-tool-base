package otelcore

import (
	"net/url"
	"strings"

	"github.com/cockroachdb/errors"
)

// MaxEndpointLength caps the length, in bytes, of an OTLP endpoint URL.
// Legitimate collector endpoints are well under 200 bytes; 2 KiB is
// generous for proxy configurations and far short of any pathological
// input.
const MaxEndpointLength = 2048

// ErrInvalidEndpoint is returned when an OTLP endpoint fails validation.
// Callers can distinguish validation failures from other errors via
// [errors.Is].
var ErrInvalidEndpoint = errors.New("invalid OTLP endpoint")

// Endpoint is a parsed OTLP/HTTP base URL, split into the components every signal
// exporter needs. Each signal appends its own suffix to BasePath: "/v1/traces",
// "/v1/metrics" or "/v1/logs".
type Endpoint struct {
	Host     string            // host:port, e.g. "collector:4318"
	BasePath string            // path prefix the per-signal suffix is appended to
	Insecure bool              // plaintext OTLP (http scheme, or an explicit insecure flag)
	Headers  map[string]string // exporter headers, e.g. an auth token
}

// ParseEndpoint splits an OTLP/HTTP base URL into exporter components. An http
// scheme, or insecure being true, marks the endpoint as plaintext — use that
// only for a local collector.
//
// The endpoint is validated fail-fast, mirroring chat.ValidateBaseURL: an empty,
// over-long, control-character-bearing, unparseable, schemeless or hostless URL
// is rejected with an error wrapping [ErrInvalidEndpoint], as is any URL carrying
// userinfo (`http://user:pass@host`) — credentials belong in headers, never the
// URL. Only http and https schemes are accepted (OTLP/HTTP commonly allows both;
// http is plaintext and marks the endpoint insecure). A malformed endpoint thus
// surfaces immediately rather than failing later or silently at export time.
func ParseEndpoint(rawURL string, insecure bool, headers map[string]string) (Endpoint, error) {
	if err := validateEndpointShape(rawURL); err != nil {
		return Endpoint{}, err
	}

	u, err := url.Parse(rawURL)
	if err != nil {
		// The parse error may echo attacker-influenced input, so keep it
		// hint-free; the caller's surrounding context is the safer log site.
		return Endpoint{}, errors.Wrap(ErrInvalidEndpoint, "parsing OTLP endpoint URL")
	}

	if err := validateEndpointSemantics(u); err != nil {
		return Endpoint{}, err
	}

	return Endpoint{
		Host:     u.Host,
		BasePath: u.Path,
		Insecure: u.Scheme == "http" || insecure,
		Headers:  headers,
	}, nil
}

// validateEndpointShape applies the byte-level checks (non-empty, length,
// control characters) that do not need url.Parse.
func validateEndpointShape(rawURL string) error {
	if rawURL == "" {
		return errors.WithHint(ErrInvalidEndpoint, "OTLP endpoint must not be empty")
	}

	if len(rawURL) > MaxEndpointLength {
		return errors.WithHintf(ErrInvalidEndpoint,
			"OTLP endpoint is %d bytes; max is %d", len(rawURL), MaxEndpointLength)
	}

	for _, r := range rawURL {
		if r < 0x20 || r == 0x7F {
			return errors.WithHint(ErrInvalidEndpoint,
				"OTLP endpoint contains ASCII control characters")
		}
	}

	return nil
}

// validateEndpointSemantics applies the structural checks that require a parsed
// URL (userinfo, scheme, host).
func validateEndpointSemantics(u *url.URL) error {
	if u.User != nil {
		// Reject any userinfo — with or without password. Never log the URL
		// itself because it contains the credential.
		return errors.WithHint(ErrInvalidEndpoint,
			"OTLP endpoint must not contain credentials; use headers instead")
	}

	switch strings.ToLower(u.Scheme) {
	case "http", "https":
	default:
		return errors.WithHintf(ErrInvalidEndpoint,
			"OTLP endpoint scheme %q is not supported; use http or https", u.Scheme)
	}

	if u.Host == "" {
		return errors.WithHint(ErrInvalidEndpoint, "OTLP endpoint must include a host")
	}

	return nil
}

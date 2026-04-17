package browser

import (
	"context"
	"net/url"
	"strings"

	clibrowser "github.com/cli/browser"
	"github.com/cockroachdb/errors"
)

// MaxURLLength is the maximum accepted length for a URL, in bytes.
//
// The value is set conservatively below OS command-line limits (Windows:
// ~32 KB, Linux: ~128 KB ARG_MAX, macOS: ~256 KB) so a single constant
// is safe on all supported platforms. 8 KiB is generous for legitimate
// URLs including mailto: with long bodies.
const MaxURLLength = 8192

// AllowedSchemes lists the URI schemes that [OpenURL] permits.
//
// Scheme comparison is case-insensitive per RFC 3986. The list is
// intentionally not configurable — extending it requires a code change
// and a security-review sign-off.
var AllowedSchemes = []string{"https", "http", "mailto"}

// ErrInvalidURL is returned when a URL fails hygiene validation:
// empty, too long, contains control characters, or fails to parse.
var ErrInvalidURL = errors.New("invalid URL")

// ErrDisallowedScheme is returned when a URL's scheme is not in
// [AllowedSchemes].
var ErrDisallowedScheme = errors.New("disallowed URL scheme")

// Opener is the signature of the function that performs the actual OS
// URL open. The default is github.com/cli/browser.OpenURL. Callers may
// override via [WithOpener] — primarily for testing, but also to
// integrate with custom OS handlers.
type Opener func(rawURL string) error

type options struct {
	opener Opener
}

// Option configures [OpenURL].
type Option func(*options)

// WithOpener replaces the default OS URL opener.
//
// Primarily intended for testing (so tests do not launch real browsers)
// but also useful for custom OS integrations. Passing nil is treated as
// "use the default".
func WithOpener(opener Opener) Option {
	return func(o *options) {
		if opener != nil {
			o.opener = opener
		}
	}
}

// defaultOpener delegates to github.com/cli/browser.OpenURL, which
// selects the appropriate platform handler (open, xdg-open, rundll32)
// and invokes it via exec.Command (no shell interpolation).
func defaultOpener(rawURL string) error {
	return clibrowser.OpenURL(rawURL)
}

// OpenURL validates rawURL and, if accepted, opens it in the user's
// default browser or mail client.
//
// Validation runs fail-fast in this order:
//
//  1. Length ≤ [MaxURLLength]
//  2. No ASCII control characters (0x00–0x1F, 0x7F)
//  3. net/url.Parse succeeds
//  4. Scheme matches [AllowedSchemes] (case-insensitive)
//  5. Context not cancelled
//
// Returns [ErrInvalidURL] wrapped with a hint for hygiene failures,
// [ErrDisallowedScheme] with a hint for scheme failures, the context's
// error for cancellation, or a wrapped error from the underlying opener
// for OS-level failures.
//
// The URL is never logged by this function: callers that wish to surface
// a diagnostic message to the user should extract the scheme and host
// from their own copy of rawURL rather than rely on errors generated here.
func OpenURL(ctx context.Context, rawURL string, opts ...Option) error {
	o := &options{opener: defaultOpener}
	for _, opt := range opts {
		opt(o)
	}

	if err := validate(rawURL); err != nil {
		return err
	}

	if err := ctx.Err(); err != nil {
		return err
	}

	if err := o.opener(rawURL); err != nil {
		return errors.Wrap(err, "invoking URL opener")
	}

	return nil
}

// validate runs the hygiene + scheme checks.
func validate(rawURL string) error {
	if rawURL == "" {
		return errors.WithHint(ErrInvalidURL, "URL is empty.")
	}

	if len(rawURL) > MaxURLLength {
		return errors.WithHintf(ErrInvalidURL,
			"URL exceeds maximum length of %d bytes.", MaxURLLength)
	}

	if idx := strings.IndexFunc(rawURL, isControl); idx >= 0 {
		return errors.WithHint(ErrInvalidURL,
			"URL contains control characters.")
	}

	parsed, err := url.Parse(rawURL)
	if err != nil {
		return errors.Wrap(ErrInvalidURL, "parsing URL")
	}

	if !isAllowedScheme(parsed.Scheme) {
		return errors.WithHintf(
			ErrDisallowedScheme,
			"scheme %q is not permitted; allowed: %v", parsed.Scheme, AllowedSchemes,
		)
	}

	return nil
}

// isControl reports whether r is an ASCII control character (0x00–0x1F
// or 0x7F). Control characters are rejected at the URL boundary so that
// platform URL handlers (rundll32, xdg-open, open) cannot receive them.
func isControl(r rune) bool {
	return r < 0x20 || r == 0x7F
}

// isAllowedScheme reports whether scheme is in [AllowedSchemes]. The
// comparison is case-insensitive per RFC 3986.
func isAllowedScheme(scheme string) bool {
	for _, s := range AllowedSchemes {
		if strings.EqualFold(scheme, s) {
			return true
		}
	}

	return false
}

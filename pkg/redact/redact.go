package redact

import (
	"regexp"
	"strings"
)

// prefixPattern pairs a credential-prefix regexp with the number of
// leading characters to keep when redacting a match.
type prefixPattern struct {
	re   *regexp.Regexp
	keep int
}

// Patterns are compiled once at package init via MustCompile. All of
// them use RE2 syntax (no backtracking), so pattern matching is
// linear in the input length.
var (
	// URL userinfo: scheme://user:pass@host/...
	// Group 1 is the scheme, preserved verbatim so the shape of the
	// URL remains recognisable after redaction.
	urlUserinfoPattern = regexp.MustCompile(
		`(https?://)[^/\s:@]+:[^/\s@]+@`,
	)

	// Query-string credential parameters. The `auth` and `authorization`
	// entries are here so a stray `?authorization=sk-abc123` is caught
	// even when it does not also match the Authorization-header rule.
	queryCredPattern = regexp.MustCompile(
		`(?i)\b(apikey|api_key|key|access_token|refresh_token|token|secret|password|auth|authorization|signature)=([^&\s]+)`,
	)

	// Authorization-style headers mentioned in free text (error
	// messages, log lines). Group 1 is the "authorization: " prefix,
	// group 2 is the scheme (Bearer, Basic, Digest, ApiKey), group 3
	// is the credential.
	authHeaderPattern = regexp.MustCompile(
		`(?i)(authorization:\s*)(bearer|basic|digest|apikey)\s+([A-Za-z0-9._~\-+/=]+)`,
	)

	// Well-known credential prefixes. These formats are unambiguous —
	// the appearance of the prefix followed by sufficient alphanumeric
	// content is almost certainly a real credential. keep is the number
	// of leading characters preserved for debug readability (the literal
	// prefix); the remainder is replaced with ***. keep is anchored to the
	// known prefix rather than discovered from the matched text, so a token
	// whose body happens to contain "_" or "-" cannot leak.
	prefixPatterns = []prefixPattern{
		{regexp.MustCompile(`sk-[A-Za-z0-9_\-]{16,}`), 3},        // OpenAI / Anthropic-style
		{regexp.MustCompile(`ghp_[A-Za-z0-9]{30,}`), 4},          // GitHub PAT classic
		{regexp.MustCompile(`gho_[A-Za-z0-9]{30,}`), 4},          // GitHub OAuth
		{regexp.MustCompile(`ghs_[A-Za-z0-9]{30,}`), 4},          // GitHub app server
		{regexp.MustCompile(`github_pat_[A-Za-z0-9_]{30,}`), 11}, // GitHub fine-grained PAT
		{regexp.MustCompile(`glpat-[A-Za-z0-9_\-]{16,}`), 6},     // GitLab personal access token
		{regexp.MustCompile(`glrt-[A-Za-z0-9_\-]{16,}`), 5},      // GitLab runner token
		{regexp.MustCompile(`gldt-[A-Za-z0-9_\-]{16,}`), 5},      // GitLab deploy token
		{regexp.MustCompile(`xox[baprs]-[A-Za-z0-9-]{10,}`), 5},  // Slack
		{regexp.MustCompile(`AIza[A-Za-z0-9_\-]{30,}`), 4},       // Google API key
		{regexp.MustCompile(`AKIA[A-Z0-9]{16}`), 4},              // AWS access key ID
	}

	// JSON Web Tokens: three base64url segments separated by dots. The
	// distinctive `eyJ` header prefix keeps false positives low; the whole
	// token is replaced (no meaningful prefix to preserve). Catches bare
	// `Bearer eyJ...` tokens that the authorization-header rule misses.
	jwtPattern = regexp.MustCompile(
		`eyJ[A-Za-z0-9_\-]+\.[A-Za-z0-9_\-]+\.[A-Za-z0-9_\-]+`,
	)

	// AWS secret access keys rarely carry a distinguishing prefix, so they
	// are matched by their assignment form (env var / ini / query) instead
	// of by value — matching a bare 40-char value would also scrub git
	// SHA-1 hashes.
	awsSecretAssignPattern = regexp.MustCompile(
		`(?i)(aws_secret_access_key|secret_access_key)(\s*[=:]\s*)\S+`,
	)

	// Fuzzy fallback: very long alphanumeric runs. 41 chars avoids
	// false positives on UUIDs (32 or 36 with hyphens), MD5 (32),
	// and SHA-1 / git-commit (40) hashes. SHA-256 (64) will match —
	// a tradeoff documented in doc.go.
	longOpaqueTokenPattern = regexp.MustCompile(
		`\b[A-Za-z0-9_\-]{41,}\b`,
	)
)

// String applies all redaction patterns to s and returns the
// sanitised result. Safe to call on any string; idempotent; returns
// the input unchanged when no sensitive patterns match.
//
// Invariants guaranteed by [FuzzRedactString]:
//
//   - No panic on any input.
//   - len(String(s)) <= len(s) + K, where K is a small constant
//     accounting for the fixed-length replacements (e.g. "***",
//     "<redacted-token>"). Never pathologically grows the input.
//   - String(String(s)) == String(s) — applying redaction twice
//     produces the same output as applying it once.
func String(s string) string {
	if s == "" {
		return s
	}

	s = urlUserinfoPattern.ReplaceAllString(s, "${1}<redacted>@")
	s = queryCredPattern.ReplaceAllString(s, "$1=***")
	s = authHeaderPattern.ReplaceAllString(s, "${1}$2 ***")
	s = awsSecretAssignPattern.ReplaceAllString(s, "${1}${2}***")
	s = jwtPattern.ReplaceAllString(s, "<redacted-token>")

	for _, p := range prefixPatterns {
		s = p.re.ReplaceAllStringFunc(s, func(match string) string {
			return keepPrefix(match, p.keep)
		})
	}

	s = longOpaqueTokenPattern.ReplaceAllString(s, "<redacted-token>")

	return s
}

// Error is a convenience wrapper equivalent to String(err.Error()).
// Returns "" for a nil error so callers do not need to guard.
func Error(err error) string {
	if err == nil {
		return ""
	}

	return String(err.Error())
}

// keepPrefix preserves the first keep characters of match (the known
// literal credential prefix) and replaces the remainder with ***. The
// keep length is anchored to the pattern, never discovered from the
// matched text, so a token body containing "_" or "-" cannot leak.
func keepPrefix(match string, keep int) string {
	if keep > 0 && keep < len(match) {
		return match[:keep] + "***"
	}

	return "***"
}

// SensitiveHeaderKeys is the default set of HTTP header names whose
// values should be redacted whenever headers are written to a log,
// telemetry, or error surface. Match is case-insensitive.
//
// Callers that need to add more entries should compose a wider set
// locally rather than mutating this slice at runtime.
var SensitiveHeaderKeys = []string{
	"Authorization",
	"Proxy-Authorization",
	"Cookie",
	"Set-Cookie",
	"X-API-Key",
	"X-API-Token",
	"X-Auth-Token",
	"X-Access-Token",
	"X-CSRF-Token",
	"X-Session-Token",
}

// sensitiveHeaderSet is the lowercased lookup-map built once at
// package init from SensitiveHeaderKeys. Case-insensitive comparison
// against HTTP header names.
var sensitiveHeaderSet = func() map[string]struct{} {
	m := make(map[string]struct{}, len(SensitiveHeaderKeys))
	for _, k := range SensitiveHeaderKeys {
		m[strings.ToLower(k)] = struct{}{}
	}

	return m
}()

// sensitiveHeaderFuzzyPattern is the substring pattern used by
// [IsSensitiveHeaderKey] to flag header names that aren't in the
// explicit list but nonetheless look credential-bearing. Deliberately
// wider than SensitiveHeaderKeys — the intent is "is the OPERATOR
// likely to have put a secret in here", not "is this on my allowlist".
var sensitiveHeaderFuzzyPattern = regexp.MustCompile(
	`(?i)\b(auth|token|key|secret|bearer|password|credential)\b|authorization`,
)

// IsSensitiveHeaderKey reports whether name identifies a header whose
// value should be redacted before logging. Matches either (a) an
// entry of [SensitiveHeaderKeys] (case-insensitive exact) or (b) the
// fuzzy substring pattern used for advisory warnings.
//
// Use (a) when deciding what to redact; use the result of this
// function when deciding whether to WARN about a caller-supplied
// header name.
func IsSensitiveHeaderKey(name string) bool {
	lower := strings.ToLower(strings.TrimSpace(name))
	if lower == "" {
		return false
	}

	if _, ok := sensitiveHeaderSet[lower]; ok {
		return true
	}

	return sensitiveHeaderFuzzyPattern.MatchString(lower)
}

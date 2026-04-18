package generator

// Input validation for the fields that flow from user input (CLI
// flags, interactive wizard, regenerate manifest) into the skeleton
// templates. Validation is the primary defence against the template
// injection class catalogued in
// docs/development/specs/2026-04-02-generator-template-escaping.md:
// most injection vectors collapse if the input character class is
// constrained. The template_escape.go helpers provide defence-in-depth
// on top.
//
// Every exported Validate* function:
//
//   - Normalises the input to Unicode NFC before validating. Homoglyph
//     attacks and combining-mark tricks fail fast this way.
//   - Returns a wrapped [ErrInvalidInput] sentinel on rejection so
//     callers can discriminate validation errors via errors.Is.
//   - Never echoes the offending value above DEBUG — callers may log
//     the field name and the rule that failed, not the input.

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/cockroachdb/errors"
	"golang.org/x/net/idna"
	"golang.org/x/text/unicode/norm"
)

const (
	maxDescriptionLen       = 500
	maxOrgLenGitHub         = 39
	maxGitLabSubgroupDepth  = 4
	maxGitLabNamespaceLen   = 255
	maxTeamsLen             = 100
	maxTelemetryEndpointLen = 2048
	truncatedInputLen       = 32
)

// ErrInvalidInput is the sentinel wrapped by every Validate* failure.
// Discriminate with errors.Is in callers that need to distinguish
// validation failures from other error shapes.
var ErrInvalidInput = errors.New("invalid generator input")

var (
	nameRe         = regexp.MustCompile(`^[a-z][a-z0-9-]{0,63}$`)
	envPrefixRe    = regexp.MustCompile(`^[A-Z][A-Z0-9_]{0,31}$`)
	slackChannelRe = regexp.MustCompile(`^[a-z0-9-]{1,80}$`)
	slackTeamRe    = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9-]{0,20}$`)
	ghOrgRe        = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9-]{0,38}$`)
	glSegmentRe    = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,254}$`)
	// Go module path segment: letters, digits, ., _, ~, -. Path
	// components are separated by "/"; a leading domain component may
	// contain ":" when a port is appended.
	repoSegmentRe = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._~-]*$`)
)

// ValidateName enforces the naming rule for the scaffolded tool —
// lowercase alphanumeric with optional hyphens, a letter first, and
// at most 64 characters. This tight rule simultaneously forecloses
// path traversal, Unicode spoofing, and YAML/TOML/Markdown/shell
// injection because none of the dangerous characters are in the class.
func ValidateName(name string) error {
	n := norm.NFC.String(name)
	if n == "" {
		return rejectf("Name", "name must not be empty", "")
	}

	if !nameRe.MatchString(n) {
		return rejectf("Name",
			"name must match ^[a-z][a-z0-9-]{0,63}$ — lowercase letter first, then lowercase letters, digits, or hyphens, max 64 chars",
			n)
	}

	return nil
}

// ValidateDescription enforces a bounded-length, control-character-free
// description that is safe to interpolate into YAML/TOML string values
// and Markdown prose. The rule explicitly forbids `{{` / `}}` as a
// belt-and-braces guard: text/template does not re-parse interpolated
// data, so this is not exploitable today, but matching the pattern
// lets a future change (e.g. switching to html/template with its
// `{{`-as-action reparsing) remain safe.
func ValidateDescription(desc string) error {
	d := norm.NFC.String(desc)

	if len(d) > maxDescriptionLen {
		return rejectf("Description",
			fmt.Sprintf("description must be at most %d bytes after NFC normalisation", maxDescriptionLen),
			d)
	}

	if strings.Contains(d, "{{") || strings.Contains(d, "}}") {
		return rejectf("Description",
			"description must not contain `{{` or `}}` (template-directive lookalikes)",
			d)
	}

	if err := rejectControlChars(d, "Description", []rune{'\t'}); err != nil {
		return err
	}

	return nil
}

// ValidateRepo enforces Go module path rules: a domain-style first
// component followed by one or more `[a-zA-Z0-9._~-]+` path segments,
// no leading/trailing `/`, and no `..` segments. `go mod tidy` would
// also reject invalid paths, but failing early surfaces a useful
// error at generation time rather than at first build.
func ValidateRepo(repo string) error {
	r := norm.NFC.String(repo)
	if r == "" {
		return rejectf("Repo", "repository must not be empty", "")
	}

	if strings.HasPrefix(r, "/") || strings.HasSuffix(r, "/") {
		return rejectf("Repo", "repository must not start or end with `/`", r)
	}

	const minRepoSegments = 2

	segments := strings.Split(r, "/")
	if len(segments) < minRepoSegments {
		return rejectf("Repo", "repository must contain at least one `/` (e.g. github.com/org/repo)", r)
	}

	for _, seg := range segments {
		if seg == "" || seg == "." || seg == ".." {
			return rejectf("Repo", fmt.Sprintf("repository segment %q is not permitted", seg), r)
		}

		if !repoSegmentRe.MatchString(seg) {
			return rejectf("Repo",
				fmt.Sprintf("repository segment %q must match ^[a-zA-Z0-9][a-zA-Z0-9._~-]*$", seg),
				r)
		}
	}

	return nil
}

// ValidateHost enforces an RFC 1123 hostname (optionally with `:port`).
// Punycode labels (`xn--...`) are accepted; raw Unicode labels are
// rejected — callers that need an internationalised host must supply
// the punycode form explicitly so homoglyph attacks fail visibly at
// input time rather than in a rendered URL.
func ValidateHost(host string) error {
	h := norm.NFC.String(host)
	if h == "" {
		return rejectf("Host", "host must not be empty", "")
	}

	hostname, port, hasPort := splitHostPort(h)
	if hasPort {
		if port == "" {
			return rejectf("Host", "port must not be empty when `:` is present", h)
		}

		for _, r := range port {
			if r < '0' || r > '9' {
				return rejectf("Host", "port must be numeric", h)
			}
		}
	}

	if !isASCII(hostname) {
		return rejectf("Host",
			"host must be ASCII (use the punycode form for internationalised domains)",
			h)
	}

	if _, err := idna.Lookup.ToASCII(hostname); err != nil {
		return rejectf("Host",
			"host must be a valid RFC 1123 hostname: "+err.Error(),
			h)
	}

	return nil
}

// ValidateOrg enforces GitHub-org syntax for the `github` release
// provider and GitLab-namespace syntax for `gitlab`, including
// `/`-separated subgroups up to a reasonable depth. CODEOWNERS
// silently drops invalid `@`-mentions, so catching bad input early
// prevents the scaffolded project from shipping broken ownership rules.
func ValidateOrg(org, releaseProvider string) error {
	o := norm.NFC.String(org)
	if o == "" {
		return rejectf("Org", "org must not be empty", "")
	}

	switch releaseProvider {
	case "gitlab":
		return validateGitLabOrg(o)
	case "github", "":
		return validateGitHubOrg(o)
	default:
		return rejectf("Org", fmt.Sprintf("unknown release provider %q", releaseProvider), o)
	}
}

func validateGitHubOrg(o string) error {
	if len(o) > maxOrgLenGitHub {
		return rejectf("Org", fmt.Sprintf("GitHub org must be at most %d characters", maxOrgLenGitHub), o)
	}

	if !ghOrgRe.MatchString(o) {
		return rejectf("Org",
			"GitHub org must match ^[a-zA-Z0-9][a-zA-Z0-9-]{0,38}$",
			o)
	}

	return nil
}

func validateGitLabOrg(o string) error {
	if len(o) > maxGitLabNamespaceLen {
		return rejectf("Org", fmt.Sprintf("GitLab namespace must be at most %d characters", maxGitLabNamespaceLen), o)
	}

	segments := strings.Split(o, "/")
	if len(segments) > maxGitLabSubgroupDepth {
		return rejectf("Org",
			fmt.Sprintf("GitLab namespace depth must be at most %d", maxGitLabSubgroupDepth),
			o)
	}

	for _, seg := range segments {
		if !glSegmentRe.MatchString(seg) {
			return rejectf("Org",
				fmt.Sprintf("GitLab namespace segment %q must match ^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,254}$", seg),
				o)
		}
	}

	return nil
}

// ValidateEnvPrefix accepts an empty string (meaning "no prefix")
// and otherwise requires an upper-snake-case prefix matching
// `^[A-Z][A-Z0-9_]{0,31}$`. Shell metacharacters are excluded by the
// class; length is bounded so the rendered env-var name stays below
// POSIX limits.
func ValidateEnvPrefix(prefix string) error {
	if prefix == "" {
		return nil
	}

	p := norm.NFC.String(prefix)
	if !envPrefixRe.MatchString(p) {
		return rejectf("EnvPrefix",
			"env prefix must match ^[A-Z][A-Z0-9_]{0,31}$",
			p)
	}

	return nil
}

// ValidateSlackChannel accepts an empty string and otherwise enforces
// Slack's own channel-name rules — lowercase, alphanumeric, hyphens,
// 1–80 characters.
func ValidateSlackChannel(channel string) error {
	if channel == "" {
		return nil
	}

	c := norm.NFC.String(strings.TrimPrefix(channel, "#"))
	if !slackChannelRe.MatchString(c) {
		return rejectf("SlackChannel",
			"slack channel must match ^[a-z0-9-]{1,80}$ (leading `#` is stripped)",
			c)
	}

	return nil
}

// ValidateSlackTeam accepts an empty string and otherwise enforces
// Slack's workspace-name rules.
func ValidateSlackTeam(team string) error {
	if team == "" {
		return nil
	}

	t := norm.NFC.String(team)
	if !slackTeamRe.MatchString(t) {
		return rejectf("SlackTeam",
			"slack team must match ^[a-zA-Z0-9][a-zA-Z0-9-]{0,20}$",
			t)
	}

	return nil
}

// ValidateTeamsChannel accepts an empty string and otherwise enforces
// generic "safe for YAML and Markdown" rules: bounded length, no
// control characters, no template-brace sequences.
func ValidateTeamsChannel(channel string) error {
	return validateTeamsValue(channel, "TeamsChannel")
}

// ValidateTeamsTeam mirrors [ValidateTeamsChannel].
func ValidateTeamsTeam(team string) error {
	return validateTeamsValue(team, "TeamsTeam")
}

func validateTeamsValue(v, field string) error {
	if v == "" {
		return nil
	}

	n := norm.NFC.String(v)

	if len(n) > maxTeamsLen {
		return rejectf(field, fmt.Sprintf("%s must be at most %d bytes", field, maxTeamsLen), n)
	}

	if strings.Contains(n, "{{") || strings.Contains(n, "}}") {
		return rejectf(field, fmt.Sprintf("%s must not contain `{{` or `}}`", field), n)
	}

	return rejectControlChars(n, field, nil)
}

// ValidateTelemetryEndpoint accepts an empty string (meaning "no
// endpoint") and otherwise requires a syntactically valid HTTP or
// HTTPS URL, bounded in length and free of control characters.
func ValidateTelemetryEndpoint(endpoint string) error {
	if endpoint == "" {
		return nil
	}

	e := norm.NFC.String(endpoint)

	if len(e) > maxTelemetryEndpointLen {
		return rejectf("TelemetryEndpoint",
			fmt.Sprintf("endpoint must be at most %d bytes", maxTelemetryEndpointLen),
			e)
	}

	if err := rejectControlChars(e, "TelemetryEndpoint", nil); err != nil {
		return err
	}

	u, err := url.Parse(e)
	if err != nil {
		return rejectf("TelemetryEndpoint", "endpoint must parse as a URL", e)
	}

	if u.Scheme != "http" && u.Scheme != "https" {
		return rejectf("TelemetryEndpoint", "endpoint scheme must be http or https", e)
	}

	if u.Host == "" {
		return rejectf("TelemetryEndpoint", "endpoint must include a host", e)
	}

	return nil
}

// rejectf constructs an ErrInvalidInput-wrapped error whose hint
// identifies the failing field, the rule, and the offending input
// (truncated) so the user sees actionable context without unbounded
// log amplification.
func rejectf(field, rule, input string) error {
	hint := fmt.Sprintf("%s: %s", field, rule)
	if input != "" {
		hint += fmt.Sprintf(" (got %q)", truncateInput(input, truncatedInputLen))
	}

	return errors.WithHint(ErrInvalidInput, hint)
}

func truncateInput(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}

	rs := []rune(s)

	return string(rs[:n]) + "…"
}

// rejectControlChars returns nil if s contains no ASCII control
// characters (0x00–0x1F, 0x7F) other than those in `allow`, or a
// typed error naming the field and the offending code point.
func rejectControlChars(s, field string, allow []rune) error {
	allowed := make(map[rune]bool, len(allow))
	for _, r := range allow {
		allowed[r] = true
	}

	for _, r := range s {
		if unicode.IsControl(r) && !allowed[r] {
			return rejectf(field,
				fmt.Sprintf("must not contain control character U+%04X", r),
				s)
		}
	}

	return nil
}

func isASCII(s string) bool {
	for _, r := range s {
		if r > unicode.MaxASCII {
			return false
		}
	}

	return true
}

// splitHostPort parses host[:port] without going through net.SplitHostPort,
// which errors on bare hostnames. Returns hostname, port, and a flag
// indicating whether a port was present.
func splitHostPort(h string) (host, port string, hasPort bool) {
	i := strings.LastIndex(h, ":")
	// Ignore any `:` inside bracketed IPv6 — not supported in our
	// inputs, but we guard rather than mis-split.
	if i < 0 || strings.Contains(h, "[") {
		return h, "", false
	}

	return h[:i], h[i+1:], true
}

// ValidateManifest runs every user-influenced field of a loaded
// [Manifest] through the validators above. Used by regenerate and
// manifest-update paths so a tampered manifest fails fast before
// driving file writes.
//
// Only [Manifest.Properties.Name] is unconditionally required — a
// manifest missing the tool name is structurally broken. Other
// fields are optional in the YAML schema and are validated only when
// populated; empty fields short-circuit to nil, matching the
// forgiving behaviour of the fine-grained validators above.
func ValidateManifest(m *Manifest) error {
	if m == nil {
		return rejectf("Manifest", "manifest must not be nil", "")
	}

	if err := validateManifestProperties(&m.Properties); err != nil {
		return err
	}

	return validateManifestReleaseSource(&m.ReleaseSource)
}

// validateManifestProperties groups the Properties-level validations
// so ValidateManifest stays under the cyclomatic-complexity budget.
func validateManifestProperties(p *ManifestProperties) error {
	if err := ValidateName(p.Name); err != nil {
		return err
	}

	if err := ValidateDescription(string(p.Description)); err != nil {
		return err
	}

	if err := ValidateEnvPrefix(p.EnvPrefix); err != nil {
		return err
	}

	if err := ValidateSlackChannel(p.Help.SlackChannel); err != nil {
		return err
	}

	if err := ValidateSlackTeam(p.Help.SlackTeam); err != nil {
		return err
	}

	if err := ValidateTeamsChannel(p.Help.TeamsChannel); err != nil {
		return err
	}

	if err := ValidateTeamsTeam(p.Help.TeamsTeam); err != nil {
		return err
	}

	if err := ValidateTelemetryEndpoint(p.Telemetry.Endpoint); err != nil {
		return err
	}

	return ValidateTelemetryEndpoint(p.Telemetry.OTelEndpoint)
}

// validateManifestReleaseSource validates Host and Owner only when
// populated; absent fields are permitted in the YAML schema.
func validateManifestReleaseSource(rs *ManifestReleaseSource) error {
	if rs.Host != "" {
		if err := ValidateHost(rs.Host); err != nil {
			return err
		}
	}

	if rs.Owner != "" {
		if err := ValidateOrg(rs.Owner, rs.Type); err != nil {
			return err
		}
	}

	return nil
}

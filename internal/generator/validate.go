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
	"go/token"
	"net/url"
	"path"
	"regexp"
	"slices"
	"strings"
	"time"
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
	maxCIComponentSourceLen = 255
	maxUpdateIntervalLen    = 32
	truncatedInputLen       = 32
)

// ErrInvalidInput is the sentinel wrapped by every Validate* failure.
// Discriminate with errors.Is in callers that need to distinguish
// validation failures from other error shapes.
var ErrInvalidInput = errors.New("invalid generator input")

var (
	nameRe         = regexp.MustCompile(`^[a-z][a-z0-9-]{0,63}$`)
	commandNameRe  = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,63}$`)
	envPrefixRe    = regexp.MustCompile(`^[A-Z][A-Z0-9_]{0,31}$`)
	slackChannelRe = regexp.MustCompile(`^[a-z0-9-]{1,80}$`)
	slackTeamRe    = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9-]{0,20}$`)
	ghOrgRe        = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9-]{0,38}$`)
	glSegmentRe    = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,254}$`)
	// Go module path segment: letters, digits, ., _, ~, -. Path
	// components are separated by "/"; a leading domain component may
	// contain ":" when a port is appended.
	repoSegmentRe = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._~-]*$`)
	// signingBackendRe matches registered `gtb sign` backend names
	// (e.g. "aws-kms", "local"): lowercase alphanumeric with hyphens.
	signingBackendRe = regexp.MustCompile(`^[a-z][a-z0-9-]{0,31}$`)
	// kmsRegionRe matches AWS region identifiers (e.g. "eu-west-2",
	// "us-gov-west-1"): lowercase alphanumeric with hyphens.
	kmsRegionRe = regexp.MustCompile(`^[a-z][a-z0-9-]{0,31}$`)
	// signingKeyIDRe matches KMS key ids, ARNs
	// (arn:aws:kms:region:account:key/uuid), aliases (alias/name), and
	// the local backend's PEM paths (./release.pem). The class excludes
	// quotes, whitespace, and control characters, so a value can never
	// break out of the double-quoted YAML scalar it is rendered into.
	signingKeyIDRe = regexp.MustCompile(`^[a-zA-Z0-9:/_.=+,@-]{1,256}$`)
	// publicKeySegmentRe matches one path segment of the armored
	// public-key path. A leading alphanumeric forecloses "." and ".."
	// segments.
	publicKeySegmentRe = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]{0,254}$`)
	// flagNameRe matches a generated command's flag name (the long form
	// passed to cobra's Flags().XxxVar): kebab-case, a lowercase letter
	// first, at most 64 characters. The name flows into a generated Go
	// identifier (pascalCase) and a cobra flag registration, so the class
	// is constrained the same way command names are.
	flagNameRe = regexp.MustCompile(`^[a-z][a-z0-9-]{0,63}$`)
)

// validFlagTypes is the set of flag types the command generator knows
// how to render (kept in sync with templates/command.go's flagFuncMap
// plus the empty/"string" default). A flag type outside this set would
// silently fall back to a string flag at generation time, so the
// non-interactive add-flag path rejects it up front.
var validFlagTypes = map[string]bool{
	"":            true,
	"string":      true,
	"bool":        true,
	"int":         true,
	"int32":       true,
	"int64":       true,
	"uint":        true,
	"uint32":      true,
	"uint64":      true,
	"float64":     true,
	"duration":    true,
	"stringSlice": true,
	"stringslice": true,
	"intSlice":    true,
	"intslice":    true,
}

// reservedCommandNames are command names the generator claims for itself:
// "root" is the scaffolded root command package and "options" collides with
// the generated options struct file.
var reservedCommandNames = map[string]bool{
	"options": true,
	"root":    true,
}

// ValidateCommandName enforces the naming rule for generated commands —
// kebab-case plus underscore, a lowercase letter first, at most 64
// characters: ^[a-z][a-z0-9_-]{0,63}$. Path separators and dots are
// rejected explicitly (before the character-class check) because the name
// flows into filepath.Join(path, "pkg", "cmd", name) and FS.RemoveAll —
// this rule is the gate that forecloses path traversal from CLI flags and
// tampered manifests alike.
func ValidateCommandName(name string) error {
	n := norm.NFC.String(name)
	if n == "" {
		return rejectf("CommandName", "command name must not be empty", "")
	}

	if strings.ContainsAny(n, `/\`) || strings.Contains(n, ".") {
		return rejectf("CommandName",
			"command name must not contain path separators (`/`, `\\`) or dots (`.`, `..`)",
			n)
	}

	if reservedCommandNames[n] {
		return rejectf("CommandName", fmt.Sprintf("command name %q is reserved", n), n)
	}

	// A command name becomes a Go package name (`package <name>`) and
	// directory, so a Go reserved word produces uncompilable output. Reject
	// it with a clear message rather than emitting a broken command.
	if token.IsKeyword(n) {
		return rejectf("CommandName",
			fmt.Sprintf("command name %q is a Go reserved word; choose another name", n), n)
	}

	if !commandNameRe.MatchString(n) {
		return rejectf("CommandName",
			"command name must match ^[a-z][a-z0-9_-]{0,63}$ — lowercase letter first, then lowercase letters, digits, hyphens, or underscores, max 64 chars",
			n)
	}

	return nil
}

// ValidateParentPath validates a `/`-separated parent command path as
// supplied via --parent. The literal "root" (and the empty string) mean
// the root command and are accepted as-is; every other segment must be a
// valid command name.
func ValidateParentPath(parent string) error {
	p := strings.TrimSpace(parent)
	if p == "" || p == "root" {
		return nil
	}

	for _, seg := range strings.Split(strings.Trim(p, "/"), "/") {
		if err := ValidateCommandName(seg); err != nil {
			return err
		}
	}

	return nil
}

// validateOptionalPattern accepts an empty value (these fields are all
// optional in the manifest schema) and otherwise requires the
// NFC-normalised value to match the given anchored pattern.
func validateOptionalPattern(field, value string, re *regexp.Regexp, rule string) error {
	if value == "" {
		return nil
	}

	v := norm.NFC.String(value)
	if !re.MatchString(v) {
		return rejectf(field, rule, v)
	}

	return nil
}

// ValidateSigningBackend accepts an empty string (meaning "default") and
// otherwise enforces the registered-backend-name character class.
func ValidateSigningBackend(backend string) error {
	return validateOptionalPattern("SigningBackend", backend, signingBackendRe,
		"signing backend must match ^[a-z][a-z0-9-]{0,31}$")
}

// ValidateSigningKMSRegion accepts an empty string and otherwise enforces
// the AWS region character class.
func ValidateSigningKMSRegion(region string) error {
	return validateOptionalPattern("SigningKMSRegion", region, kmsRegionRe,
		"KMS region must match ^[a-z][a-z0-9-]{0,31}$")
}

// ValidateSigningKeyID accepts an empty string and otherwise enforces the
// KMS key-id / ARN / alias character class (which also admits the local
// backend's PEM paths). The value is rendered into the generated
// .goreleaser.yaml, so quotes, whitespace, and control characters are all
// outside the class. A literal `..` substring is rejected as
// defence-in-depth: legitimate KMS ids, ARNs, aliases, and the local
// backend's relative PEM paths never contain it, mirroring how
// ValidateCommandName forecloses traversal.
func ValidateSigningKeyID(keyID string) error {
	if keyID == "" {
		return nil
	}

	if strings.Contains(keyID, "..") {
		return rejectf("SigningKeyID",
			"signing key id must not contain `..`",
			keyID)
	}

	return validateOptionalPattern("SigningKeyID", keyID, signingKeyIDRe,
		"signing key id must match ^[a-zA-Z0-9:/_.=+,@-]{1,256}$ (a KMS id/ARN/alias, or a PEM path for the local backend)")
}

// ValidateSigningPublicKey accepts an empty string and otherwise requires a
// clean, slash-separated path relative to the project root (the manifest
// documents the field as the path to the armored public-key file, e.g.
// internal/trustkeys/keys/signing-key-v1.asc). A single leading `./` is
// normalised away as a friendliness affordance, so `./key.asc` is treated
// as `key.asc`. Absolute paths, `..` escapes, backslashes, and any other
// unclean forms are still rejected — the path must resolve inside the
// project root.
func ValidateSigningPublicKey(publicKey string) error {
	if publicKey == "" {
		return nil
	}

	p := norm.NFC.String(publicKey)
	if strings.HasPrefix(p, "/") || strings.Contains(p, `\`) {
		return rejectf("SigningPublicKey",
			"public key must be a `/`-separated path relative to the project root",
			p)
	}

	// Normalise a single leading `./` before the cleanliness check so that
	// the friendly `./key.asc` form is accepted. Anything beyond the one
	// leading `./` (e.g. `././`, `./../`) remains unclean and is rejected
	// by the path.Clean comparison below.
	p = strings.TrimPrefix(p, "./")

	if p == "" || path.Clean(p) != p {
		return rejectf("SigningPublicKey",
			"public key must be a clean relative path (no `.`, `..`, doubled, or trailing slashes)",
			p)
	}

	for _, seg := range strings.Split(p, "/") {
		if !publicKeySegmentRe.MatchString(seg) {
			return rejectf("SigningPublicKey",
				fmt.Sprintf("public key path segment %q must match ^[a-zA-Z0-9][a-zA-Z0-9._-]{0,254}$", seg),
				p)
		}
	}

	return nil
}

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

// ValidateFlagName enforces the naming rule for a generated command's
// flag: kebab-case, a lowercase letter first, at most 64 characters
// (^[a-z][a-z0-9-]{0,63}$). The name becomes a Go identifier in the
// generated options struct and a cobra flag registration, so the same
// constraint as command names applies.
func ValidateFlagName(name string) error {
	n := norm.NFC.String(name)
	if n == "" {
		return rejectf("FlagName", "flag name must not be empty", "")
	}

	if !flagNameRe.MatchString(n) {
		return rejectf("FlagName",
			"flag name must match ^[a-z][a-z0-9-]{0,63}$ — lowercase letter first, then lowercase letters, digits, or hyphens, max 64 chars",
			n)
	}

	return nil
}

// ValidateFlagShorthand accepts an empty string (meaning "no shorthand") and
// otherwise requires exactly one ASCII letter, which becomes cobra's single-rune
// shorthand (the `-x` form). Anything longer or non-letter is rejected before it
// reaches the StringVarP registration in the generated code.
func ValidateFlagShorthand(shorthand string) error {
	if shorthand == "" {
		return nil
	}

	if len(shorthand) != 1 || !isASCIILetter(rune(shorthand[0])) {
		return rejectf("FlagShorthand",
			"flag shorthand must be a single ASCII letter (e.g. v for -v)",
			shorthand)
	}

	return nil
}

func isASCIILetter(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')
}

// ValidateFlagType rejects a flag type the command generator can't
// render. The empty string and "string" are accepted (both map to a
// string flag); every other value must be one the generator's
// flagFuncMap knows. Without this gate an unknown type silently
// degrades to a string flag in the generated code.
func ValidateFlagType(flagType string) error {
	t := norm.NFC.String(flagType)
	if !validFlagTypes[t] {
		return rejectf("FlagType",
			"flag type must be one of: string, bool, int, int32, int64, uint, uint32, uint64, float64, duration, stringSlice, intSlice",
			t)
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

// ValidateUpdatePolicy accepts an empty string (meaning "use the framework
// default of disabled") and otherwise requires one of the three known
// posture values: disabled, prompt, or enabled. The value selects a typed
// props.UpdatePolicy constant rendered into the generated tool, so an
// unknown value is rejected rather than silently treated as disabled.
func ValidateUpdatePolicy(policy string) error {
	switch policy {
	case "", "disabled", "prompt", "enabled":
		return nil
	default:
		return rejectf("UpdatePolicy",
			"update policy must be one of: disabled, prompt, enabled",
			policy)
	}
}

// ValidateFeatureName rejects any name that is not one of the toggleable
// built-in features (see ToggleableFeatures). Used by `gtb enable <feature>` /
// `gtb disable <feature>` so an unknown name fails fast with the valid set
// listed, rather than silently writing a junk manifest entry.
func ValidateFeatureName(name string) error {
	if slices.Contains(ToggleableFeatures, name) {
		return nil
	}

	return rejectf("Feature",
		"unknown feature (valid: "+strings.Join(ToggleableFeatures, ", ")+")",
		name)
}

// ValidateUpdateCheckInterval accepts an empty string (meaning "use the
// framework default of 24h") and otherwise requires a valid, non-negative Go
// duration (as understood by time.ParseDuration, e.g. "24h", "168h", "30m").
// The value is rendered into the generated tool's props.Tool.UpdateCheckInterval
// as a time.Duration expression, so a malformed or negative value is rejected
// rather than silently falling back. A length cap bounds the parse input.
func ValidateUpdateCheckInterval(interval string) error {
	if interval == "" {
		return nil
	}

	if len(interval) > maxUpdateIntervalLen {
		return rejectf("UpdateCheckInterval",
			fmt.Sprintf("update check interval must be at most %d characters", maxUpdateIntervalLen),
			interval)
	}

	d, err := time.ParseDuration(interval)
	if err != nil {
		return rejectf("UpdateCheckInterval",
			"update check interval must be a Go duration, e.g. 24h or 168h",
			interval)
	}

	if d < 0 {
		return rejectf("UpdateCheckInterval",
			"update check interval must not be negative",
			interval)
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

// ValidateCIComponentSource accepts an empty string (meaning "use the
// framework default") and otherwise requires a bare host/path component
// source such as `gitlab.com/phpboyscout/cicd`. The value is interpolated
// verbatim into an unquoted YAML include path in the scaffolded
// .gitlab-ci.yml, so it is restricted to a strict character class
// (alphanumerics, `.`, `-`, `_`, `/`) and rejected if it carries a URL
// scheme, whitespace, control characters, or template delimiters.
func ValidateCIComponentSource(source string) error {
	if source == "" {
		return nil
	}

	s := norm.NFC.String(source)

	const field = "CIComponentSource"

	if len(s) > maxCIComponentSourceLen {
		return rejectf(field,
			fmt.Sprintf("component source must be at most %d bytes", maxCIComponentSourceLen),
			s)
	}

	if err := rejectControlChars(s, field, nil); err != nil {
		return err
	}

	if strings.Contains(s, "{{") || strings.Contains(s, "}}") {
		return rejectf(field, "component source must not contain `{{` or `}}`", s)
	}

	for _, r := range s {
		if !isComponentSourceRune(r) {
			return rejectf(field,
				"component source must be a bare host/path (letters, digits, `.`, `-`, `_`, `/`)",
				s)
		}
	}

	return nil
}

// componentSourcePunct is the set of non-alphanumeric ASCII runes permitted in
// a CI component source (a bare host/path).
const componentSourcePunct = "._-/"

// isComponentSourceRune reports whether r is allowed in a CI component source:
// ASCII letters, digits, and the path punctuation in componentSourcePunct.
func isComponentSourceRune(r rune) bool {
	if r > unicode.MaxASCII {
		return false
	}

	return isASCIIAlphanumeric(r) || strings.ContainsRune(componentSourcePunct, r)
}

// isASCIIAlphanumeric reports whether r is an ASCII letter or digit.
func isASCIIAlphanumeric(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')
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

	if err := validateManifestSigning(&m.Properties.Signing); err != nil {
		return err
	}

	if err := validateManifestCommands(m.Commands); err != nil {
		return err
	}

	if err := validateManifestTemplates(m.Properties.Templates); err != nil {
		return err
	}

	return validateManifestReleaseSource(&m.ReleaseSource)
}

// validateManifestCommands walks the manifest command tree and validates
// every command name, so a tampered manifest cannot drive the
// filepath.Join / RemoveAll sinks through the regenerate path.
func validateManifestCommands(cmds []ManifestCommand) error {
	for i := range cmds {
		if err := ValidateCommandName(cmds[i].Name); err != nil {
			return err
		}

		if err := validateManifestCommands(cmds[i].Commands); err != nil {
			return err
		}
	}

	return nil
}

// validateManifestSigning validates every ManifestSigning field that is
// rendered into the CI-executed .goreleaser.yaml signs block.
func validateManifestSigning(s *ManifestSigning) error {
	if err := ValidateSigningBackend(s.Backend); err != nil {
		return err
	}

	if err := ValidateSigningKMSRegion(s.KMSRegion); err != nil {
		return err
	}

	if err := ValidateSigningKeyID(s.KeyID); err != nil {
		return err
	}

	return ValidateSigningPublicKey(s.PublicKey)
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

	if err := ValidateUpdatePolicy(p.UpdatePolicy); err != nil {
		return err
	}

	if err := ValidateUpdateCheckInterval(p.UpdateCheckInterval); err != nil {
		return err
	}

	if err := validateManifestHelp(&p.Help); err != nil {
		return err
	}

	if err := ValidateTelemetryEndpoint(p.Telemetry.Endpoint); err != nil {
		return err
	}

	if err := ValidateTelemetryEndpoint(p.Telemetry.OTelEndpoint); err != nil {
		return err
	}

	return ValidateCIComponentSource(p.CI.ComponentSource)
}

// validateManifestHelp validates the Slack/Teams help-channel fields,
// keeping validateManifestProperties under the cyclomatic-complexity budget.
func validateManifestHelp(h *ManifestHelp) error {
	if err := ValidateSlackChannel(h.SlackChannel); err != nil {
		return err
	}

	if err := ValidateSlackTeam(h.SlackTeam); err != nil {
		return err
	}

	if err := ValidateTeamsChannel(h.TeamsChannel); err != nil {
		return err
	}

	return ValidateTeamsTeam(h.TeamsTeam)
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

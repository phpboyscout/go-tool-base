package generator

// Validation for the templates manifest block. A tampered templates entry
// must never drive a fetch or a write outside the rules, so ValidateManifest
// runs every entry through ValidateTemplateSource (mirroring how it already
// validates Commands and Signing). On the regenerate path invalid entries are
// skipped, not fatal (consistent with the rest of the manifest gate).

import (
	"regexp"
	"strings"

	"golang.org/x/text/unicode/norm"
)

const (
	maxTemplateLocationLen = 512
	maxTemplateRefLen      = 256
	maxTemplateNameLen     = 64
	shaLenAbbrevMin        = 7
	shaLenFull             = 64
)

var (
	// templateNameRe matches an operator-assigned source handle: a lowercase
	// letter first, then lowercase letters, digits, hyphens, underscores.
	templateNameRe = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,63}$`)
	// templateRefRe matches a git ref the operator may pin to: branch, tag,
	// or commit. The class excludes whitespace, quotes, shell metacharacters,
	// and control bytes so a ref can never break out of an argument or a
	// rendered URL. Slashes and dots are allowed (feature/x, v1.2.0).
	templateRefRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/-]{0,255}$`)
	// templateSHARe matches a resolved commit SHA: hex, abbreviated (7) to
	// full (40/64) length. We accept up to 64 to admit SHA-256 object ids.
	templateSHARe = regexp.MustCompile(`^[0-9a-f]{7,64}$`)
	// templateFingerprintRe matches the local-source content fingerprint: a
	// full SHA-256 hex digest.
	templateFingerprintRe = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

// ValidateTemplateSource validates a single manifest templates entry: the type
// discriminator, the location character class (no traversal), the ref/SHA
// shape, and the optional name. It is the gate that forecloses a tampered
// manifest driving a fetch or write outside the rules.
func ValidateTemplateSource(ts *TemplateSource) error {
	switch ts.Type {
	case TemplateSourceGit, TemplateSourceLocal:
	default:
		return rejectf("TemplateSourceType",
			`template source type must be "git" or "local"`,
			string(ts.Type))
	}

	if ts.Name != "" {
		n := norm.NFC.String(ts.Name)
		if len(n) > maxTemplateNameLen || !templateNameRe.MatchString(n) {
			return rejectf("TemplateSourceName",
				"template source name must match ^[a-z][a-z0-9_-]{0,63}$", n)
		}
	}

	if err := validateTemplateLocation(ts.Type, ts.Location); err != nil {
		return err
	}

	if err := validateTemplateRef(ts.Ref); err != nil {
		return err
	}

	if err := validateTemplateResolved(ts.Resolved); err != nil {
		return err
	}

	return validateTemplateFingerprint(ts.Fingerprint)
}

// validateTemplateLocation enforces the location rules per source type. A git
// location is a forge repo path or clone URL; a local location is a filesystem
// path. Both reject `..` traversal and over-long values; neither may be empty.
func validateTemplateLocation(t TemplateSourceType, location string) error {
	if location == "" {
		return rejectf("TemplateSourceLocation", "template source location must not be empty", "")
	}

	l := norm.NFC.String(location)
	if len(l) > maxTemplateLocationLen {
		return rejectf("TemplateSourceLocation",
			"template source location is too long", l)
	}

	if err := rejectControlChars(l, "TemplateSourceLocation", nil); err != nil {
		return err
	}

	// `..` traversal is rejected for both types: a git forge path never needs
	// it, and a local path that climbs out is exactly the threat. (Absolute
	// local paths are permitted — the operator owns their filesystem — but the
	// write side is always contained under the project root regardless.)
	if hasDotDotSegment(l) {
		return rejectf("TemplateSourceLocation",
			"template source location must not contain a `..` path segment", l)
	}

	if t == TemplateSourceGit {
		return validateGitLocation(l)
	}

	return nil
}

// validateGitLocation accepts either a full https/ssh clone URL or a bare
// forge repo path (org/repo, nested GitLab groups). Plaintext http and file
// URLs are rejected — a template source must be fetched over a secure
// transport or read from a local path (TemplateSourceLocal), never plain http.
func validateGitLocation(l string) error {
	if strings.Contains(l, "://") {
		if !strings.HasPrefix(l, "https://") && !strings.HasPrefix(l, "ssh://") && !strings.HasPrefix(l, "git@") {
			return rejectf("TemplateSourceLocation",
				"git template source URL must use https:// or ssh:// (got an insecure or unsupported scheme)", l)
		}

		return nil
	}

	if strings.HasPrefix(l, "git@") {
		return nil
	}

	return validateBareGitPath(l)
}

// validateBareGitPath validates a bare forge path: at least host-less
// org/repo (2 segments) up to a host-qualified nested group path. Each segment
// must be a clean repo segment (no `.`/`..`, no leading separator).
func validateBareGitPath(l string) error {
	clean := strings.Trim(l, "/")
	if clean == "" {
		return rejectf("TemplateSourceLocation", "git template source path must not be empty", l)
	}

	const minGitPathSegments = 2

	segments := strings.Split(clean, "/")
	if len(segments) < minGitPathSegments {
		return rejectf("TemplateSourceLocation",
			"git template source path must be org/repo or host/org/repo", l)
	}

	for _, seg := range segments {
		if seg == "" || seg == "." || seg == ".." || !repoSegmentRe.MatchString(seg) {
			return rejectf("TemplateSourceLocation",
				"git template source path segment must match ^[a-zA-Z0-9][a-zA-Z0-9._~-]*$", l)
		}
	}

	return nil
}

func validateTemplateRef(ref string) error {
	if ref == "" {
		return nil
	}

	r := norm.NFC.String(ref)
	if len(r) > maxTemplateRefLen || !templateRefRe.MatchString(r) || strings.Contains(r, "..") {
		return rejectf("TemplateSourceRef",
			"template ref must match ^[A-Za-z0-9][A-Za-z0-9._/-]{0,255}$ with no `..`", r)
	}

	return nil
}

func validateTemplateResolved(resolved string) error {
	if resolved == "" {
		return nil
	}

	if !templateSHARe.MatchString(resolved) {
		return rejectf("TemplateSourceResolved",
			"resolved commit must be a hex SHA (7–64 chars)", resolved)
	}

	return nil
}

func validateTemplateFingerprint(fp string) error {
	if fp == "" {
		return nil
	}

	if !templateFingerprintRe.MatchString(fp) {
		return rejectf("TemplateSourceFingerprint",
			"content fingerprint must be a 64-char SHA-256 hex digest", fp)
	}

	return nil
}

// hasDotDotSegment reports whether the path contains a literal `..` segment
// under either separator convention.
func hasDotDotSegment(p string) bool {
	normalised := strings.ReplaceAll(p, `\`, "/")
	for _, seg := range strings.Split(normalised, "/") {
		if seg == ".." {
			return true
		}
	}

	return false
}

// validateManifestTemplates validates every templates entry. Used by
// ValidateManifest so a tampered entry fails the gate.
func validateManifestTemplates(sources []TemplateSource) error {
	for i := range sources {
		if err := ValidateTemplateSource(&sources[i]); err != nil {
			return err
		}
	}

	return nil
}

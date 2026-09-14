package generator

// Parsing the CLI `<src>@<ref>` template-source spec into a TemplateSource.
// Shared by the `generate project --template` flag and the `gtb template`
// command group so both build the same manifest representation.

import (
	"strings"

	"github.com/spf13/afero"

	"gitlab.com/phpboyscout/go/errors"
)

// ParseTemplateSpec parses a `<src>@<ref>` spec into a TemplateSource. The
// `@<ref>` suffix is optional (defaults to the source's default branch for
// git). The source type is inferred: a value that exists as a directory on fs,
// or that looks like a filesystem path (starts with ./ ../ / or ~), is a
// local source; otherwise it is a git source. The parsed entry is validated
// before return so a malformed spec fails at the CLI boundary.
//
// An explicit name (e.g. from `--name`) overrides the inferred default; pass
// "" to take the default (the repo/dir base name).
func ParseTemplateSpec(fs afero.Fs, spec, name string) (TemplateSource, error) {
	raw := strings.TrimSpace(spec)
	if raw == "" {
		return TemplateSource{}, errors.New("template source must not be empty")
	}

	location, ref := splitSpecRef(raw)

	ts := TemplateSource{
		Name:     name,
		Location: location,
		Ref:      ref,
		Type:     inferSourceType(fs, location),
	}

	if ts.Name == "" {
		ts.Name = defaultSourceName(location)
	}

	if err := ValidateTemplateSource(&ts); err != nil {
		return TemplateSource{}, err
	}

	return ts, nil
}

// splitSpecRef splits a `<src>@<ref>` spec on the LAST `@`, but only when the
// `@` follows a path/host segment (so an `ssh://git@host/...` URL is not
// mis-split). The ref is whatever follows the final `@` when it contains no
// `/` (refs never contain a host); otherwise the whole value is the location.
func splitSpecRef(raw string) (location, ref string) {
	at := strings.LastIndex(raw, "@")
	if at <= 0 || at == len(raw)-1 {
		return raw, ""
	}

	candidateRef := raw[at+1:]
	// A scp-style git@host or ssh URL has a `/` after the `@` (the path),
	// which a bare ref never does — keep those intact.
	if strings.Contains(candidateRef, "/") {
		return raw, ""
	}

	return raw[:at], candidateRef
}

// inferSourceType returns local for a value that resolves to an existing
// directory or that is shaped like a filesystem path; git otherwise.
func inferSourceType(fs afero.Fs, location string) TemplateSourceType {
	if strings.HasPrefix(location, "./") || strings.HasPrefix(location, "../") ||
		strings.HasPrefix(location, "/") || strings.HasPrefix(location, "~") {
		return TemplateSourceLocal
	}

	if strings.Contains(location, "://") || strings.HasPrefix(location, "git@") {
		return TemplateSourceGit
	}

	if fs != nil {
		if exists, _ := afero.DirExists(fs, location); exists {
			return TemplateSourceLocal
		}
	}

	return TemplateSourceGit
}

// defaultSourceName derives a stable handle from the location's last path
// segment (repo or dir name), lower-cased with non-handle characters mapped to
// hyphens. Returns "" when no usable name can be derived (the caller then
// leaves Name empty and falls back to Location for labels).
func defaultSourceName(location string) string {
	trimmed := strings.TrimSuffix(strings.Trim(location, "/"), ".git")
	if trimmed == "" {
		return ""
	}

	segments := strings.Split(trimmed, "/")
	base := segments[len(segments)-1]

	var b strings.Builder

	for _, r := range strings.ToLower(base) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}

	name := strings.Trim(b.String(), "-")
	if name == "" || !templateNameRe.MatchString(name) {
		return ""
	}

	return name
}

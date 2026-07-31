package generator

// Validation for the external_commands manifest block. A tampered entry must
// never drive a go.mod require or a rendered Go call outside the rules, so
// ValidateManifest runs every entry through ValidateExternalCommand (mirroring
// how it already validates Commands, Signing, and Templates). The module path,
// import path, and identifiers are all rendered into generated Go source and the
// go.mod require, so each is gated here.

import (
	"regexp"
	"strings"

	"golang.org/x/text/unicode/norm"

	"gitlab.com/phpboyscout/go-tool-base/internal/generator/templates"
)

const (
	maxExternalModulePathLen = 512
	maxExternalVersionLen    = 128
	maxExternalAliasLen      = 64
	maxExternalConstructor   = 128
)

var (
	// externalVersionRe matches an explicit module version pin: a semver tag
	// (v1.2.3, with optional pre-release / build metadata) or a Go
	// pseudo-version (v0.0.0-20200101000000-abcdef123456, which fits the
	// pre-release grammar). The class excludes whitespace, quotes, and control
	// bytes so the value can never break out of the go.mod require line.
	externalVersionRe = regexp.MustCompile(
		`^v[0-9]+\.[0-9]+\.[0-9]+(?:-[0-9A-Za-z][0-9A-Za-z.-]*)?(?:\+[0-9A-Za-z][0-9A-Za-z.-]*)?$`)
	// externalAliasRe matches an import alias: a valid Go identifier. It is
	// emitted verbatim as the package alias in the generated root import.
	externalAliasRe = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]{0,63}$`)
	// externalConstructorRe matches an EXPORTED Go identifier — the constructor
	// symbol called on the external package. Exported (upper-first) is required
	// because an external package's constructor must be exported to be callable.
	externalConstructorRe = regexp.MustCompile(`^[A-Z][A-Za-z0-9_]{0,127}$`)
)

// ValidateExternalCommand validates a single manifest external_commands entry:
// the module path, the required version pin, the optional import path and
// alias, and every attach descriptor. It is the gate that forecloses a tampered
// manifest driving a go.mod require or a rendered constructor call outside the
// rules.
func ValidateExternalCommand(ec *ManifestExternalCommand) error {
	if err := validateExternalModulePath(ec.Module, "ExternalCommandModule"); err != nil {
		return err
	}

	if err := validateExternalVersion(ec.Version); err != nil {
		return err
	}

	if err := validateExternalImportAndAlias(ec); err != nil {
		return err
	}

	if len(ec.Attach) == 0 {
		return rejectf("ExternalCommandAttach",
			"external command must declare at least one attach entry", ec.Module)
	}

	for i := range ec.Attach {
		if err := validateExternalAttach(&ec.Attach[i]); err != nil {
			return err
		}
	}

	return nil
}

// validateExternalImportAndAlias validates the two optional identity fields:
// the import path (when it overrides the module path) and the import alias.
func validateExternalImportAndAlias(ec *ManifestExternalCommand) error {
	if ec.ImportPath != "" {
		if err := validateExternalModulePath(ec.ImportPath, "ExternalCommandImportPath"); err != nil {
			return err
		}
	}

	if ec.Alias != "" && (len(ec.Alias) > maxExternalAliasLen || !externalAliasRe.MatchString(ec.Alias)) {
		return rejectf("ExternalCommandAlias",
			"alias must be a valid Go identifier ^[a-zA-Z_][a-zA-Z0-9_]{0,63}$", ec.Alias)
	}

	return nil
}

// validateExternalAttach validates one constructor call descriptor: the
// exported constructor identifier, every injection token against the closed
// vocabulary, and the optional collision-check name.
func validateExternalAttach(a *ManifestExternalAttach) error {
	if a.Constructor == "" {
		return rejectf("ExternalCommandConstructor", "constructor must not be empty", "")
	}

	if len(a.Constructor) > maxExternalConstructor || !externalConstructorRe.MatchString(a.Constructor) {
		return rejectf("ExternalCommandConstructor",
			"constructor must be an exported Go identifier ^[A-Z][A-Za-z0-9_]{0,127}$", a.Constructor)
	}

	for _, tok := range a.Args {
		if !templates.IsExternalArgToken(tok) {
			return rejectf("ExternalCommandArg",
				"unknown injection token; must be one of "+strings.Join(templates.ExternalArgTokens, ", "),
				tok)
		}
	}

	if a.Name != "" {
		if err := ValidateCommandName(a.Name); err != nil {
			return err
		}
	}

	return nil
}

// validateExternalVersion enforces an explicit, well-shaped version pin. An
// empty version is rejected: the declarative channel requires an explicit
// @version (there is no implicit latest resolution in v1).
func validateExternalVersion(v string) error {
	if v == "" {
		return rejectf("ExternalCommandVersion",
			"version is required — pin an explicit module version (e.g. v1.2.3)", "")
	}

	if len(v) > maxExternalVersionLen || !externalVersionRe.MatchString(v) {
		return rejectf("ExternalCommandVersion",
			"version must be a semver pin like v1.2.3 (pre-release and pseudo-versions allowed)", v)
	}

	return nil
}

// validateExternalModulePath validates a Go module (or import) path: a clean,
// ASCII, /-separated path whose every segment matches the Go module segment
// class and contains no traversal. The value is rendered into the go.mod
// require and the generated root import, so it is gated the same way the
// release-source module path is.
func validateExternalModulePath(mp, field string) error {
	if mp == "" {
		return rejectf(field, "module path must not be empty", "")
	}

	m := norm.NFC.String(mp)
	if len(m) > maxExternalModulePathLen || !isASCII(m) {
		return rejectf(field, "module path is too long or non-ASCII", m)
	}

	if !isCleanSlashPath(m) {
		return rejectf(field, "module path must be a clean `/`-separated path", m)
	}

	for _, seg := range strings.Split(m, "/") {
		if !isModulePathSegment(seg) {
			return rejectf(field,
				"module path segment must match ^[a-zA-Z0-9][a-zA-Z0-9._~-]*$ (no traversal)", seg)
		}
	}

	return nil
}

// isCleanSlashPath reports whether p is a `/`-separated path with no leading or
// trailing separator and no backslash.
func isCleanSlashPath(p string) bool {
	return !strings.HasPrefix(p, "/") && !strings.HasSuffix(p, "/") && !strings.Contains(p, `\`)
}

// isModulePathSegment reports whether seg is a valid Go module path segment and
// not a traversal component.
func isModulePathSegment(seg string) bool {
	return seg != "." && seg != ".." && repoSegmentRe.MatchString(seg)
}

// validateManifestExternalCommands validates every external_commands entry and
// rejects a duplicate (module, constructor) attachment. Used by ValidateManifest
// so a tampered entry fails the gate before it drives a require or a render.
func validateManifestExternalCommands(ecs []ManifestExternalCommand) error {
	seen := make(map[string]bool)

	for i := range ecs {
		if err := ValidateExternalCommand(&ecs[i]); err != nil {
			return err
		}

		for j := range ecs[i].Attach {
			key := ecs[i].Module + "\x00" + ecs[i].Attach[j].Constructor
			if seen[key] {
				return rejectf("ExternalCommandAttach",
					"duplicate (module, constructor) attachment",
					ecs[i].Module+"."+ecs[i].Attach[j].Constructor)
			}

			seen[key] = true
		}
	}

	return nil
}

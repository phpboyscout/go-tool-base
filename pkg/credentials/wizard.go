package credentials

import (
	"os"
	"regexp"
	"time"

	"charm.land/huh/v2"
	"github.com/cockroachdb/errors"
)

// KeychainOpTimeout bounds any single credentials-backend operation
// (probe, store, retrieve, delete) performed by a setup wizard so a
// misbehaving remote or locked keychain cannot stall first-run setup
// indefinitely. Wizards SHOULD derive a context with this timeout
// before calling [Probe], [Store], [Retrieve], or [Delete].
const KeychainOpTimeout = 5 * time.Second

// envVarNameRe enforces a conservative POSIX env var shape so a name
// chosen in the wizard is a valid environment variable and fits
// downstream shell/YAML contexts without quoting.
var envVarNameRe = regexp.MustCompile(`^[A-Z][A-Z0-9_]{0,63}$`)

// IsCI reports whether the process appears to be running under a CI
// system. The single source of truth for every setup wizard's CI
// gate — in particular the [ModeLiteral] refusal enforced by
// [RefuseLiteralUnderCI].
func IsCI() bool {
	return os.Getenv("CI") == "true"
}

// ValidateEnvVarName enforces a conservative `^[A-Z][A-Z0-9_]{0,63}$`
// shape so the chosen name is a valid POSIX environment variable and
// fits downstream shell/YAML contexts without quoting. Returns a
// descriptive error suitable for surfacing directly in a TUI form.
func ValidateEnvVarName(name string) error {
	if name == "" {
		return errors.New("env var name is required")
	}

	if !envVarNameRe.MatchString(name) {
		return errors.New("env var name must match ^[A-Z][A-Z0-9_]{0,63}$")
	}

	return nil
}

// RefuseLiteralUnderCI returns a hinted error when literal credential
// storage is selected while running under CI, and nil otherwise. This
// is the single source of truth for the CI-literal-refusal invariant:
// literal-mode writes in CI almost certainly leak the secret to build
// artefacts or logs, so every wizard funnels its defence-in-depth
// check through this helper rather than re-deriving it.
func RefuseLiteralUnderCI(mode Mode) error {
	if mode == ModeLiteral && IsCI() {
		return errors.WithHint(
			errors.New("literal credential storage is refused under CI"),
			"CI environments must use platform-injected secrets referenced via env-var mode.")
	}

	return nil
}

// StorageModeOptions returns the [huh.Option] list for a storage-mode
// selector, filtered by CI state and a live keychain probe. The
// keychain option surfaces only when keychainUsable is true (backend
// compiled in AND reachable — a locked keychain or a headless Linux
// host without D-Bus must not leave the user stuck on a dead option).
// The literal option is hidden under CI so the wizard never offers a
// mode that [RefuseLiteralUnderCI] would immediately reject.
//
// envVarLabel, keychainLabel, and literalLabel let each wizard tailor
// the option captions (Bitbucket's dual-credential model phrases them
// differently from the single-secret AI/GitHub wizards).
func StorageModeOptions(ci, keychainUsable bool, envVarLabel, keychainLabel, literalLabel string) []huh.Option[Mode] {
	opts := []huh.Option[Mode]{
		huh.NewOption(envVarLabel, ModeEnvVar),
	}

	if keychainUsable {
		opts = append(opts, huh.NewOption(keychainLabel, ModeKeychain))
	}

	if !ci {
		opts = append(opts, huh.NewOption(literalLabel, ModeLiteral))
	}

	return opts
}

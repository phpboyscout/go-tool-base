package credentials

import "github.com/cockroachdb/errors"

// Mode identifies how a credential is persisted by the setup wizard
// and resolved at runtime. See package doc for the full trust model.
type Mode string

const (
	// ModeEnvVar stores the name of an environment variable that
	// contains the credential. Recommended default; the only mode
	// permitted under CI.
	ModeEnvVar Mode = "env"

	// ModeKeychain stores the credential in the OS keychain. The
	// config records a keychain reference; the secret never hits
	// disk. Available only when built with `-tags keychain`.
	ModeKeychain Mode = "keychain"

	// ModeLiteral stores the credential value directly in the
	// config file. Supported for backward compatibility and
	// throwaway environments. Refused under CI.
	ModeLiteral Mode = "literal"
)

// ErrCredentialUnsupported is returned by keychain operations in
// builds that omit the `keychain` tag. Callers can errors.Is against
// it to present an informative message at setup time.
var ErrCredentialUnsupported = errors.New("keychain support not compiled (build with -tags keychain)")

// ErrCredentialNotFound is returned by [Retrieve] when the keychain
// is available and functional but no entry exists for the given
// service/account pair. Distinguished from ErrCredentialUnsupported
// so resolvers can decide whether to fall through to a literal
// config value.
var ErrCredentialNotFound = errors.New("credential not found in keychain")

// keychainCompiled is set to true by the keychain-enabled build
// file's init(), and remains false in the stub build. Exposed via
// [KeychainAvailable] so callers can decide whether to offer the
// keychain option in a UI without importing a build-tagged symbol.
//
//nolint:gochecknoglobals // set once by package init at startup
var keychainCompiled bool

// KeychainAvailable reports whether OS keychain support was
// compiled into this binary. When false, [Store] / [Retrieve] /
// [Delete] return [ErrCredentialUnsupported].
func KeychainAvailable() bool {
	return keychainCompiled
}

// AvailableModes returns the credential storage modes supported by
// the current build. [ModeKeychain] is present only when
// [KeychainAvailable] returns true.
func AvailableModes() []Mode {
	modes := []Mode{ModeEnvVar}

	if keychainCompiled {
		modes = append(modes, ModeKeychain)
	}

	modes = append(modes, ModeLiteral)

	return modes
}

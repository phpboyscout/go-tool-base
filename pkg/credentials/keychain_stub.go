//go:build !keychain

package credentials

// Default build: no keychain link. All operations return
// [ErrCredentialUnsupported] so resolvers can fall through to the
// next step and the setup wizard can hide the keychain option.

// Store returns [ErrCredentialUnsupported] in the default build.
// To use the OS keychain, rebuild with `-tags keychain`.
func Store(_, _, _ string) error {
	return ErrCredentialUnsupported
}

// Retrieve returns an empty string and [ErrCredentialUnsupported] in
// the default build. Callers should errors.Is against it and fall
// through to the next credential-resolution step.
func Retrieve(_, _ string) (string, error) {
	return "", ErrCredentialUnsupported
}

// Delete returns [ErrCredentialUnsupported] in the default build.
func Delete(_, _ string) error {
	return ErrCredentialUnsupported
}

package credentials

// stubBackend is the always-compiled default backend. It never
// touches a platform keychain, D-Bus session bus, or any other
// external service — every operation returns
// [ErrCredentialUnsupported] so resolvers fall through to the next
// step (env var or literal) and setup wizards hide the keychain
// option. A binary that never imports pkg/credentials/keychain (or
// calls [RegisterBackend] with a custom implementation) links no
// go-keyring code, no IPC code paths, nothing to audit — the
// regulated-deployment baseline.
type stubBackend struct{}

// Store always fails. Callers MUST treat [ErrCredentialUnsupported]
// as a signal to fall through, not to abort — see the resolvers in
// pkg/chat and pkg/vcs.
func (stubBackend) Store(_, _, _ string) error {
	return ErrCredentialUnsupported
}

// Retrieve always fails. The empty string return is an extra
// belt-and-braces guard — a caller that ignores the error still
// sees no credential material from the stub.
func (stubBackend) Retrieve(_, _ string) (string, error) {
	return "", ErrCredentialUnsupported
}

// Delete always fails. Idempotence guarantees only apply to real
// backends; a caller that hits the stub's Delete is misusing the
// API in a configuration that never could have stored anything.
func (stubBackend) Delete(_, _ string) error {
	return ErrCredentialUnsupported
}

// Available is false for the stub so [KeychainAvailable] and setup
// wizards suppress the keychain option when no real backend has
// been registered.
func (stubBackend) Available() bool {
	return false
}

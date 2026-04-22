package credentials

import "sync/atomic"

// Backend is the minimal contract the credential store presents to
// the setup wizards, resolvers, and doctor checks. The default
// implementation (see default_backend.go) returns
// [ErrCredentialUnsupported] from every call, so callers that never
// register a real backend behave exactly as if the keychain feature
// were compiled out entirely.
//
// Real backends are registered by a downstream optional module —
// github.com/phpboyscout/go-tool-base/pkg/credentials/keychain — via
// [RegisterBackend]. Tool authors may also plug in a custom backend
// (e.g. Vault, AWS SSM) by implementing this interface and calling
// RegisterBackend during program init.
type Backend interface {
	// Store writes a secret under the service/account pair. Must
	// overwrite any existing entry with the same key. Neither argument
	// may be logged by the implementation — resolvers rely on being
	// able to pass these to DEBUG log surfaces safely.
	Store(service, account, secret string) error

	// Retrieve reads a secret. MUST return [ErrCredentialNotFound] when
	// the backend is healthy but the entry does not exist, so resolvers
	// can distinguish "missing" from "unavailable" and fall through
	// cleanly. Other failures should wrap the underlying error.
	Retrieve(service, account string) (string, error)

	// Delete removes a secret. Idempotent: must return nil when the
	// entry does not exist. Only surface real failures (e.g. keychain
	// locked) as errors.
	Delete(service, account string) error

	// Available reports whether this backend can currently satisfy
	// Store/Retrieve/Delete without an immediate [ErrCredentialUnsupported].
	// A freshly-registered keychain backend typically returns true; the
	// default stub returns false. [Probe] does a live round-trip on top.
	Available() bool
}

// backend holds the currently-registered Backend. Swapped atomically
// so registration from a side-effect import (e.g. the optional
// pkg/credentials/keychain subpackage's init()) is safe even if the
// surrounding program has already started making credential calls.
var backend atomic.Pointer[Backend] //nolint:gochecknoglobals // process-wide singleton by design

// init installs the default stub backend so every public API in this
// package has something to delegate to from t=0, even in a regulated
// build that never imports the keychain subpackage.
//
//nolint:gochecknoinits // one-time registration at startup
func init() {
	var b Backend = stubBackend{}

	backend.Store(&b)
}

// RegisterBackend swaps the active backend. The keychain subpackage
// calls this from its init(); custom implementations may call it from
// anywhere safe, typically a tool's main() before credential use.
func RegisterBackend(b Backend) {
	backend.Store(&b)
}

// currentBackend returns the active backend. Always non-nil because
// the package init() installs the stub.
func currentBackend() Backend {
	return *backend.Load()
}

package credentials

import (
	"context"
	"sync/atomic"
)

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
// (Vault, AWS SSM, 1Password Connect, …) by implementing this
// interface and calling RegisterBackend during program init. See
// docs/how-to/custom-credential-backend.md for a worked example.
//
// Every method takes a [context.Context] so backends that perform
// network I/O (remote secret stores, hardware security modules) can
// honour deadlines and cancellation. OS-keychain backends ignore
// the context — local IPC is fast — but implementers SHOULD still
// accept it for uniformity.
type Backend interface {
	// Store writes a secret under the service/account pair. Must
	// overwrite any existing entry with the same key. Neither argument
	// may be logged by the implementation — resolvers rely on being
	// able to pass these to DEBUG log surfaces safely. Implementations
	// SHOULD return ctx.Err() when the context is cancelled before
	// the write commits.
	Store(ctx context.Context, service, account, secret string) error

	// Retrieve reads a secret. MUST return [ErrCredentialNotFound] when
	// the backend is healthy but the entry does not exist, so resolvers
	// can distinguish "missing" from "unavailable" and fall through
	// cleanly. Other failures should wrap the underlying error. A
	// cancelled context MUST abort the call.
	Retrieve(ctx context.Context, service, account string) (string, error)

	// Delete removes a secret. Idempotent: must return nil when the
	// entry does not exist. Only surface real failures (e.g. keychain
	// locked, remote unreachable) as errors.
	Delete(ctx context.Context, service, account string) error

	// Available reports whether this backend can currently satisfy
	// Store/Retrieve/Delete without an immediate [ErrCredentialUnsupported].
	// A freshly-registered keychain backend typically returns true; the
	// default stub returns false. Available SHOULD be a cheap static
	// check — use [Probe] for a live round-trip.
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
// calls this from its init(); custom implementations (Vault, AWS
// SSM, …) may call it from anywhere safe, typically a tool's main()
// before the first credential call.
func RegisterBackend(b Backend) {
	backend.Store(&b)
}

// currentBackend returns the active backend. Always non-nil because
// the package init() installs the stub.
func currentBackend() Backend {
	return *backend.Load()
}

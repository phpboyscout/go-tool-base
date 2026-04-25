package credentials

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"os"
	"strconv"

	"github.com/cockroachdb/errors"
)

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
	// disk. Available only when the process has registered a
	// keychain-capable [Backend] — typically by importing the
	// optional pkg/credentials/keychain subpackage.
	ModeKeychain Mode = "keychain"

	// ModeLiteral stores the credential value directly in the
	// config file. Supported for backward compatibility and
	// throwaway environments. Refused under CI.
	ModeLiteral Mode = "literal"
)

// ErrCredentialUnsupported is returned by [Store], [Retrieve], and
// [Delete] when no keychain-capable [Backend] has been registered —
// either because the optional pkg/credentials/keychain subpackage was
// not imported, or because a custom backend has opted out. Callers
// that want to fall through to a literal or env-var step should
// errors.Is against this sentinel.
var ErrCredentialUnsupported = errors.New("keychain support not compiled (import pkg/credentials/keychain or register a custom Backend)")

// ErrCredentialNotFound is returned by [Retrieve] when the backend
// is reachable but no entry exists for the given service/account
// pair. Distinguished from [ErrCredentialUnsupported] so resolvers
// can decide whether to fall through to a literal config value.
var ErrCredentialNotFound = errors.New("credential not found in keychain")

// Store writes a secret to the registered [Backend]. Returns
// [ErrCredentialUnsupported] when no real backend is registered.
// The context is forwarded to the backend so network-backed
// implementations (Vault, AWS SSM) can honour deadlines and
// cancellation; OS-keychain backends ignore it.
func Store(ctx context.Context, service, account, secret string) error {
	return currentBackend().Store(ctx, service, account, secret)
}

// Retrieve reads a secret from the registered [Backend]. Returns
// [ErrCredentialUnsupported] when no real backend is registered,
// [ErrCredentialNotFound] when the backend is present but the entry
// is missing, or a wrapped backend error for other failures. A
// cancelled context aborts the lookup.
func Retrieve(ctx context.Context, service, account string) (string, error) {
	return currentBackend().Retrieve(ctx, service, account)
}

// Delete removes a secret from the registered [Backend]. Idempotent:
// returns nil when the entry is missing. Returns
// [ErrCredentialUnsupported] when no real backend is registered.
func Delete(ctx context.Context, service, account string) error {
	return currentBackend().Delete(ctx, service, account)
}

// KeychainAvailable reports whether a keychain-capable [Backend] is
// registered. Returns false in the default (stub-backend) process.
// Setup wizards use this as a first-pass gate before [Probe].
func KeychainAvailable() bool {
	return currentBackend().Available()
}

// AvailableModes returns the credential storage modes supported by
// this process. [ModeKeychain] is present only when a keychain-capable
// [Backend] is registered.
func AvailableModes() []Mode {
	modes := []Mode{ModeEnvVar}

	if KeychainAvailable() {
		modes = append(modes, ModeKeychain)
	}

	modes = append(modes, ModeLiteral)

	return modes
}

// probeService is the synthetic keychain service used by [Probe] to
// verify the backend is reachable without touching any real user
// credentials. A per-process random account nonce avoids collisions
// when multiple tools run concurrently on the same host.
const probeService = "gtb-keychain-probe"

// probeAccount returns a per-invocation account identifier combining
// the current PID and a 32-bit random nonce. Two concurrent probes
// from the same process cannot collide, and probes from distinct
// processes are isolated by PID.
func probeAccount() string {
	var nonce [4]byte
	// crypto/rand.Read on a stack-allocated slice does not fail in
	// practice, but fall through to an empty nonce if it ever does —
	// the PID alone still gives a reasonable level of uniqueness.
	_, _ = rand.Read(nonce[:])

	return "probe-" + strconv.Itoa(os.Getpid()) + "-" + hex.EncodeToString(nonce[:])
}

// Probe reports whether the registered [Backend] is reachable at the
// time of the call — useful for setup wizards that want to hide the
// ModeKeychain option on hosts where the backend is compiled in but
// locked, unreachable (headless Linux without a Secret Service
// provider, Vault unreachable), or otherwise unusable. It performs
// a canary Set → Get → Delete round-trip under the reserved
// "gtb-keychain-probe" service with a per-invocation random account
// and discards the result.
//
// With the stub backend, Probe short-circuits to false without
// touching anything. A true return therefore means both "a backend
// is registered" AND "the backend accepts round-trip calls right
// now", which is the precise guard the wizard needs before offering
// keychain as a storage option.
//
// Callers SHOULD pass a context with a short timeout (a few seconds
// is reasonable) so a misbehaving remote backend cannot stall the
// setup wizard indefinitely.
func Probe(ctx context.Context) bool {
	if !KeychainAvailable() {
		return false
	}

	account := probeAccount()

	if err := Store(ctx, probeService, account, "probe"); err != nil {
		return false
	}

	// Round-trip read to confirm the item landed in a readable place,
	// not a write-only sink. Any error here marks the backend as
	// unsuitable — we clean up best-effort and report false.
	if _, err := Retrieve(ctx, probeService, account); err != nil {
		_ = Delete(ctx, probeService, account)

		return false
	}

	return Delete(ctx, probeService, account) == nil
}

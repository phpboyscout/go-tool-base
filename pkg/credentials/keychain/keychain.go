// Package keychain is the optional OS-keychain backend for
// github.com/phpboyscout/go-tool-base/pkg/credentials. Importing this
// package (even as a blank import) registers a go-keyring-backed
// implementation of [credentials.Backend] at init time, so any
// credential calls that follow route through the platform keychain:
// macOS Keychain, Linux Secret Service (GNOME Keyring, KWallet) via
// godbus, Windows Credential Manager via danieljoos/wincred.
//
// The package carries the go-keyring dependency chain, so tools that
// must run without session-bus / keychain IPC (regulated builds, air-
// gapped deployments) simply omit the blank import from their cmd
// package. Go's linker dead-code elimination keeps go-keyring,
// godbus, and wincred out of their binary — verifiable via any SBOM
// tool that inspects the linked artefact. The same mechanism applies
// to cmd/gtb: delete cmd/gtb/keychain.go to ship a keychain-free gtb.
package keychain

import (
	"context"
	stderrors "errors"

	"github.com/cockroachdb/errors"
	"github.com/zalando/go-keyring"

	"github.com/phpboyscout/go-tool-base/pkg/credentials"
)

// Backend implements [credentials.Backend] against the OS keychain
// via github.com/zalando/go-keyring. Its zero value is usable.
type Backend struct{}

// Store writes a secret under the given service/account pair.
// Overwrites any existing entry. Neither argument is logged —
// callers may pass them to DEBUG log surfaces safely. Context is
// accepted for interface uniformity but ignored: go-keyring's
// underlying platform APIs (Keychain Services, Secret Service over
// D-Bus, Windows Credential Manager) do not expose cancellation.
// Callers needing a deadline on misbehaving local IPC should run
// this in a goroutine and drop the result when the context fires.
func (Backend) Store(_ context.Context, service, account, secret string) error {
	if err := keyring.Set(service, account, secret); err != nil {
		// Wrap without the secret: go-keyring sometimes quotes
		// portions of the payload on failure.
		return errors.Wrapf(err, "keyring.Set %s/%s", service, account)
	}

	return nil
}

// Retrieve reads a secret. Returns [credentials.ErrCredentialNotFound]
// when the backend is functional but no entry exists for the pair —
// resolvers use this specific sentinel to decide whether to fall
// through. Other failures wrap the underlying error. Context is
// accepted for interface uniformity; see Store for the caveat.
func (Backend) Retrieve(_ context.Context, service, account string) (string, error) {
	v, err := keyring.Get(service, account)
	if err == nil {
		return v, nil
	}

	if stderrors.Is(err, keyring.ErrNotFound) {
		return "", credentials.ErrCredentialNotFound
	}

	return "", errors.Wrapf(err, "keyring.Get %s/%s", service, account)
}

// Delete removes a secret. Idempotent: returns nil when the entry
// does not exist. Only real failures surface as errors.
func (Backend) Delete(_ context.Context, service, account string) error {
	err := keyring.Delete(service, account)
	if err == nil {
		return nil
	}

	if stderrors.Is(err, keyring.ErrNotFound) {
		return nil
	}

	return errors.Wrapf(err, "keyring.Delete %s/%s", service, account)
}

// Available reports true — importing this subpackage is the caller's
// declaration that they want keychain-capable behaviour. The live
// "does it actually work right now" check is [credentials.Probe].
func (Backend) Available() bool {
	return true
}

// init registers the go-keyring backend so a blank import
// (`_ ".../pkg/credentials/keychain"`) is sufficient to activate
// keychain mode for the process. Programs that prefer explicit
// wiring may instead call [credentials.RegisterBackend] with their
// own [Backend] instance at any time.
//
//nolint:gochecknoinits // side-effect registration is the whole point
func init() {
	credentials.RegisterBackend(Backend{})
}

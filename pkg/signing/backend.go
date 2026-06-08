// Package signing exposes a registry of signing-key backends. Each
// backend knows how to talk to a specific HSM/KMS/keyring and produce
// a crypto.Signer that callers (typically pkg/openpgpkey) use to mint
// an OpenPGP-armored public key.
//
// Backends register themselves at process start via init() calling
// Register. Downstream binaries opt into a backend by blank-importing
// its package — this is the same activate-by-side-effect pattern used
// in net/http/pprof, image/* decoders, and the framework's own
// pkg/credentials/keychain. The signing-key minter (gtb keys mint)
// reads the registered names via Names() and dispatches based on the
// --backend flag the user passes.
//
// Built-in backends shipping with the standard `gtb` binary:
//
//   - "aws-kms" (pkg/signing/kms):   RSA-4096 keys held in AWS KMS.
//   - "local"   (pkg/signing/local): RSA keys loaded from an
//     unencrypted PKCS#1 or PKCS#8 PEM file on disk. Use for the
//     onboarding tutorial / local signing flows; never in CI.
//
// Third parties add backends by implementing Backend and calling
// Register from their package's init(). No upstream change is needed.
// See docs/how-to/add-signing-backend.md for the contract details.
//
// API stability: Beta. The interface is small and stable; the only
// known evolution path is additive (e.g. ECDSA support, capability-
// discovery methods).
package signing

import (
	"context"
	"crypto"

	"github.com/cockroachdb/errors"
	"github.com/spf13/pflag"
)

// Backend constructs a crypto.Signer for an HSM/KMS-held signing key.
// Implementations are registered globally via Register and selected at
// runtime by Name (matching the user's --backend flag).
//
// The contract:
//
//   - Name returns a stable identifier used at the CLI (--backend
//     <name>). Must be lowercase, kebab-case, unique across the
//     process — duplicate Register calls panic.
//   - RegisterFlags lets the backend declare its own CLI flags
//     (region, endpoint, keyring path, etc.) on the supplied flag set.
//     Called once per process startup, before flag parsing.
//   - NewSigner returns a crypto.Signer wrapping the remote key. The
//     backend interprets keyID's format — AWS uses ARNs/aliases, GPG
//     uses uids, GCP uses resource names, etc. The signer's Public()
//     must return a type the caller can consume; pkg/openpgpkey
//     currently requires *rsa.PublicKey.
//
// Backends do not own credential resolution; callers configure the
// SDK / agent / environment before invoking the minter.
type Backend interface {
	Name() string
	RegisterFlags(fs *pflag.FlagSet)
	NewSigner(ctx context.Context, keyID string) (crypto.Signer, error)
}

// ErrUnknownBackend is returned by Get when the named backend is not
// registered. The error message lists the available backends so users
// can see what their binary actually ships with.
var ErrUnknownBackend = errors.New("unknown signing backend")

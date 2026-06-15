// Package setup signing primitives.
//
// This file defines the cryptographic foundation for Phase 2 of the
// remote-update-checksum-verification spec: a TrustSet of vetted public
// keys, a minimum-strength policy that runs at TrustSet construction
// time, and a verifier for a detached ASCII-armored signature over the
// checksums manifest.
//
// Trust sets are immutable once constructed. Verification iterates the
// set and returns nil on the first key that validates the signature;
// the failure path returns ErrSignatureInvalid without naming the keys
// tried.
package setup

import (
	"bytes"
	"context"
	"encoding/hex"
	"sort"
	"strings"

	"github.com/ProtonMail/go-crypto/openpgp"
	"github.com/ProtonMail/go-crypto/openpgp/packet"
	"github.com/cockroachdb/errors"
)

// Size bounds on signing-path untrusted inputs. Exported as variables so
// tool authors with exceptional release layouts can adjust them; the
// defaults are generous but protect against an unbounded download.
//
//nolint:gochecknoglobals // size bounds are tool-author tunables
var (
	// MaxSignatureSize caps the bytes read from a detached signature
	// download. GPG detached signatures are typically 400-800 bytes;
	// 8 KiB is 10x headroom.
	MaxSignatureSize int64 = 8 << 10

	// MaxWKDResponseSize caps the bytes read from a WKD public-key fetch.
	MaxWKDResponseSize int64 = 64 << 10
)

// Compile-time defaults for signature enforcement. Tool authors override
// these in main() to change behaviour for their binary; downstream
// config and env vars override per-invocation.
//
//nolint:gochecknoglobals // compile-time defaults are intentional
var (
	// DefaultRequireSignature is the compile-time default for signature
	// enforcement. Tool authors should set this to true in main() once
	// the first signed release is available and clients have received an
	// embedded key in a prior release (see the N+1 / N+2 / N+3 rollout
	// in docs/development/phase2-signing-prep.md).
	DefaultRequireSignature = false

	// DefaultKeySource is the compile-time default for the key-source
	// mode. Accepted values: "embedded", "external", "both" (default).
	DefaultKeySource = "both"

	// DefaultRequireExternalCrosscheck controls whether a failure to
	// reach the external key resolver (WKD) aborts the update. Set to
	// true in locked-down environments where silent fallback to
	// embedded-only is unacceptable.
	DefaultRequireExternalCrosscheck = false

	// DefaultExternalKeyEmail is the email used to derive the WKD URL.
	// Tool authors should set this in main() to their release email.
	DefaultExternalKeyEmail = ""
)

// Sentinel errors raised by the signing primitives. Callers should
// match on these with errors.Is rather than string comparison.
//
//nolint:gochecknoglobals // sentinel errors are the idiomatic shape
var (
	// ErrSignatureInvalid is returned when no key in the trust set
	// validates the detached signature over the checksums manifest. The
	// same error is used for malformed signatures: an unparseable
	// signature is not distinguishable from a forged one for the
	// caller's purpose.
	ErrSignatureInvalid = errors.New("signature verification failed")

	// ErrSignatureMissing is returned when require_signature is true and
	// no signature asset was found in the release.
	ErrSignatureMissing = errors.New("signature asset not found in release")

	// ErrWeakKey is returned when a public key (embedded or fetched)
	// fails the minimum-strength policy at LoadTrustSet time.
	ErrWeakKey = errors.New("public key fails minimum-strength policy")

	// ErrSignatureTooLarge is returned when the signature download
	// exceeds MaxSignatureSize.
	ErrSignatureTooLarge = errors.New("signature download exceeds maximum size")

	// ErrKeyResolverMismatch is returned by CompositeResolver when the
	// fingerprint sets returned by child resolvers do not match.
	ErrKeyResolverMismatch = errors.New("key resolvers returned mismatched trust sets")

	// ErrKeyResolverUnavailable is returned when a key resolver cannot
	// fetch its keys (network failure, DNS, TLS) and RequireAll or
	// RequireExternalCrosscheck is true.
	ErrKeyResolverUnavailable = errors.New("key resolver unavailable")

	// ErrWKDResponseTooLarge is returned when a WKD response exceeds
	// MaxWKDResponseSize.
	ErrWKDResponseTooLarge = errors.New("WKD response exceeds maximum size")
)

// minRSAStrengthBits is the lower bound for an RSA public key to pass
// the strength policy. 3072 bits is the conservative modern threshold;
// 2048 is considered weakened, and 1024 is broken.
const minRSAStrengthBits = 3072

// TrustSet is the collection of public keys that can validate update
// signatures. It is constructed by a KeyResolver per update attempt
// and is immutable once constructed: LoadTrustSet validates the
// strength policy at construction time so weak keys never enter the
// trust set even transiently.
type TrustSet struct {
	entities openpgp.EntityList
}

// Fingerprints returns the 40-char uppercase hex fingerprints of every
// key in the trust set, sorted ascending so two TrustSets can be
// compared for equality by their fingerprint slices.
func (t *TrustSet) Fingerprints() []string {
	out := make([]string, 0, len(t.entities))
	for _, e := range t.entities {
		out = append(out, strings.ToUpper(hex.EncodeToString(e.PrimaryKey.Fingerprint)))
	}

	sort.Strings(out)

	return out
}

// LoadTrustSet parses one or more ASCII-armored public-key blobs and
// enforces the minimum-strength policy: Ed25519 keys are accepted, RSA
// keys are accepted at 3072 bits or stronger, everything else is
// rejected with ErrWeakKey. Any weak key in the input aborts the load,
// so weak keys cannot enter the trust set even transiently.
func LoadTrustSet(armoredKeys ...[]byte) (*TrustSet, error) {
	if len(armoredKeys) == 0 {
		return nil, errors.New("LoadTrustSet: no keys provided")
	}

	var all openpgp.EntityList

	for i, blob := range armoredKeys {
		ring, err := openpgp.ReadArmoredKeyRing(bytes.NewReader(blob))
		if err != nil {
			return nil, errors.Wrapf(err, "LoadTrustSet: parse key #%d", i)
		}

		if len(ring) == 0 {
			return nil, errors.Newf("LoadTrustSet: key #%d contained no entities", i)
		}

		all = append(all, ring...)
	}

	return loadTrustSetFromEntities(all)
}

// loadTrustSetFromEntities applies the minimum-strength policy to a
// pre-parsed entity list and returns the resulting trust set. Used by
// LoadTrustSet (armored input) and by WKDResolver (binary input).
func loadTrustSetFromEntities(entities openpgp.EntityList) (*TrustSet, error) {
	if len(entities) == 0 {
		return nil, errors.New("loadTrustSetFromEntities: no entities provided")
	}

	ts := &TrustSet{}

	for _, ent := range entities {
		if err := checkKeyStrength(ent.PrimaryKey); err != nil {
			return nil, err
		}

		// Signature verification resolves issuers including signing-capable
		// subkeys, so the strength floor must cover them too — otherwise a
		// weak subkey could validate a signature under a strong primary.
		for _, sub := range ent.Subkeys {
			if !subkeyCanSign(sub) {
				continue
			}

			if err := checkKeyStrength(sub.PublicKey); err != nil {
				return nil, err
			}
		}

		ts.entities = append(ts.entities, ent)
	}

	return ts, nil
}

// subkeyCanSign reports whether sub may be used to verify signatures. When the
// binding signature's key flags are valid, it honours the Sign flag; when they
// are absent it fails closed (treats the subkey as signing-capable) so an
// unflagged subkey is still strength-checked.
func subkeyCanSign(sub openpgp.Subkey) bool {
	if sub.PublicKey == nil {
		return false
	}

	if sub.Sig != nil && sub.Sig.FlagsValid {
		return sub.Sig.FlagSign
	}

	return true
}

// checkKeyStrength enforces the minimum-strength policy. The accept-
// list is short and explicit (Ed25519 in both legacy EdDSA and modern
// Ed25519 packet forms; RSA at minRSAStrengthBits or above) and every
// other algorithm in the OpenPGP packet enum is rejected by name.
// A final default branch catches any new algorithm a future
// go-crypto release might introduce, so an unknown algorithm fails
// closed rather than slipping into the trust set.
func checkKeyStrength(pk *packet.PublicKey) error {
	switch pk.PubKeyAlgo {
	case packet.PubKeyAlgoEdDSA, packet.PubKeyAlgoEd25519:
		return nil
	case packet.PubKeyAlgoRSA, packet.PubKeyAlgoRSASignOnly:
		bits, err := pk.BitLength()
		if err != nil {
			return errors.Wrap(err, "reading RSA key bit length")
		}

		if uint64(bits) < minRSAStrengthBits {
			return errors.Wrapf(ErrWeakKey,
				"RSA key has %d bits, minimum is %d", bits, minRSAStrengthBits)
		}

		return nil
	case packet.PubKeyAlgoDSA,
		packet.PubKeyAlgoElGamal,
		packet.PubKeyAlgoECDH,
		packet.PubKeyAlgoECDSA,
		packet.PubKeyAlgoX25519,
		packet.PubKeyAlgoX448,
		packet.PubKeyAlgoEd448,
		packet.PubKeyAlgoRSAEncryptOnly:
		return errors.Wrapf(ErrWeakKey,
			"algorithm %d rejected (only Ed25519 and RSA >= %d bits accepted)",
			pk.PubKeyAlgo, minRSAStrengthBits)
	default:
		return errors.Wrapf(ErrWeakKey,
			"unknown algorithm %d rejected (only Ed25519 and RSA >= %d bits accepted)",
			pk.PubKeyAlgo, minRSAStrengthBits)
	}
}

// VerifyManifestSignature verifies an ASCII-armored detached signature
// against the checksums manifest using any key in the trust set.
//
// Returns nil on the first successful verification. Returns
// ErrSignatureInvalid (via errors.Is) for an empty, malformed, or
// non-validating signature; the underlying parser error is wrapped for
// diagnostics but the sentinel is the contract the caller matches.
//
// The error returned for a failed signature deliberately does not name
// the keys tried, so a caller that logs only the sentinel does not leak
// which key in the set rejected the signature.
func (t *TrustSet) VerifyManifestSignature(manifest, signature []byte) error {
	_, err := t.VerifyManifestSignatureSigner(manifest, signature)

	return err
}

// VerifyManifestSignatureSigner is the fingerprint-returning form of
// [TrustSet.VerifyManifestSignature]. On success it returns the 40-char
// uppercase hex fingerprint of the trust-set key that validated the
// signature, so callers can record which key was used for the audit trail.
//
// The error contract is identical to VerifyManifestSignature: it returns
// ErrSignatureInvalid (via errors.Is) for an empty, malformed, or
// non-validating signature, and the returned fingerprint is empty on any
// error.
func (t *TrustSet) VerifyManifestSignatureSigner(manifest, signature []byte) (string, error) {
	if len(signature) == 0 {
		return "", errors.Wrap(ErrSignatureInvalid, "signature is empty")
	}

	// Defense-in-depth: enforce the RSA strength floor at verification time
	// too, so a weak key cannot validate a signature even if it reached the
	// trust set through a path that skipped loadTrustSetFromEntities.
	signer, err := openpgp.CheckArmoredDetachedSignature(
		t.entities,
		bytes.NewReader(manifest),
		bytes.NewReader(signature),
		&packet.Config{MinRSABits: uint16(minRSAStrengthBits)},
	)
	if err != nil {
		return "", errors.Wrap(ErrSignatureInvalid, err.Error())
	}

	if signer == nil || signer.PrimaryKey == nil {
		return "", errors.Wrap(ErrSignatureInvalid, "no signer in trust set matched")
	}

	return strings.ToUpper(hex.EncodeToString(signer.PrimaryKey.Fingerprint)), nil
}

// KeyResolver returns the TrustSet used to verify release signatures.
// Implementations may read embedded data, fetch over the network, or
// combine multiple sources with cross-checks. The interface separates
// "where the trust anchor comes from" from "how a signature is
// verified against it", so SelfUpdater can be wired with whichever
// resolver chain a tool needs without changing verification logic.
type KeyResolver interface {
	// Name returns a short identifier used in logs and diagnostics
	// (e.g. "embedded", "wkd:openpgpkey.example.com", "composite").
	Name() string

	// Resolve returns the trust set for the current update attempt.
	// Callers are responsible for caching where appropriate; Resolve
	// may perform I/O on every call.
	Resolve(ctx context.Context) (*TrustSet, error)
}

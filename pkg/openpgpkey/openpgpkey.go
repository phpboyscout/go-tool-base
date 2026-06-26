// Package openpgpkey mints an ASCII-armored OpenPGP public key from a
// crypto.Signer. It exists to bridge a KMS/HSM-held signing key (which
// exposes only a public key plus a remote Sign operation) into the
// OpenPGP form that the self-update verifier (pkg/setup) and Web Key
// Directory require. It also generates the WKD tree for publishing the
// key ([WriteWKDTree]).
//
// Why this is needed: the verifier rejects a bare public-key packet
// ("v4 entity without any identities") — it needs a User ID and a
// self-signature. Producing that self-signature requires *signing*
// with the private key. go-crypto signs RSA keys through the
// crypto.Signer interface, so an opaque KMS-backed signer works
// without the private key ever leaving the KMS.
//
// Supported key algorithm: **RSA only.**
//
// signer.Public() must return *rsa.PublicKey. The package produces a
// v4 RSA OpenPGP public-key packet — used by the primary
// release-signing key path (the aws-kms backend targets RSA in v0.1)
// and by `gtb keys generate --algorithm rsa` for
// the local-signing tutorial flow.
//
// Other key types return ErrUnsupportedKeyType. Ed25519 key
// generation lives in `internal/cmd/keys/generate.go`, where it
// goes through `openpgp.NewEntity(... PubKeyAlgoEdDSA ...)` to
// produce a v4-EdDSA (algorithm 22) key that GnuPG 2.4 and older
// can import. Bringing Ed25519 into this package would require
// reaching go-crypto's `internal/ecc` package, which is unreachable
// from outside the go-crypto module — see
// docs/development/specs/2026-06-08-keys-mint-command.md D12.
package openpgpkey

import (
	"bytes"
	"crypto"
	"crypto/rsa"
	"io"
	"time"

	"github.com/ProtonMail/go-crypto/openpgp"
	"github.com/ProtonMail/go-crypto/openpgp/armor"
	"github.com/ProtonMail/go-crypto/openpgp/packet"
	"github.com/cockroachdb/errors"
)

// ErrUnsupportedKeyType is returned by ArmoredPublicKey when the
// signer's Public() returns a key type this package does not handle.
// Only *rsa.PublicKey is supported in v0.1 — see the package doc.
var ErrUnsupportedKeyType = errors.New("unsupported key type: only RSA is supported")

// ArmoredPublicKey builds a self-signed OpenPGP key from signer and
// returns its ASCII-armored *public* half — ready to embed
// (internal/trustkeys/keys) or publish via WKD. creationTime is baked
// into the key and its self-signature; keep it stable (rotations get a
// new key, not a new timestamp on the same key).
func ArmoredPublicKey(signer crypto.Signer, name, email string, creationTime time.Time) ([]byte, error) {
	var buf bytes.Buffer

	if err := WriteArmoredPublicKey(&buf, signer, name, email, creationTime); err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}

// WriteArmoredPublicKey writes the same content as ArmoredPublicKey
// directly to w. The two-function shape exists so callers that want
// to stream into a file or socket can skip the intermediate buffer;
// it also lets the test suite inject a failing writer to exercise
// the error-wrapping branches around armor.Encode / Close.
func WriteArmoredPublicKey(w io.Writer, signer crypto.Signer, name, email string, creationTime time.Time) error {
	ent, err := Entity(signer, name, email, creationTime)
	if err != nil {
		return err
	}

	enc, err := armor.Encode(w, openpgp.PublicKeyType, nil)
	if err != nil {
		return errors.WithStack(err)
	}

	// Entity.Serialize writes the public half (key + UID + self-sig);
	// SerializePrivate would be needed for the secret key.
	if err := ent.Serialize(enc); err != nil {
		return errors.Wrap(err, "serializing public key")
	}

	if err := enc.Close(); err != nil {
		return errors.WithStack(err)
	}

	return nil
}

// Entity assembles a self-signed, signing-capable OpenPGP entity
// around signer: an RSA public-key packet, an RSA private-key packet
// driven by signer (so KMS-held keys work), a User ID, and a
// positive-cert self-signature.
//
// Callers that need only the armored public half should prefer
// [ArmoredPublicKey], which wraps Entity + armor.Encode +
// Entity.Serialize in one call. Direct callers of Entity get both
// halves and can drive Entity.Serialize / Entity.SerializePrivate
// themselves — useful when writing both halves to separate files,
// which is what `gtb keys generate` does for the RSA path.
//
// Supports opaque signers (any crypto.Signer with *rsa.PublicKey
// from Public()) — go-crypto's RSA signing path dispatches via the
// crypto.Signer interface, so a KMS-backed signer works.
//
// Non-RSA signers return ErrUnsupportedKeyType. Ed25519 generation
// is handled separately in `internal/cmd/keys/generate.go` via
// openpgp.NewEntity with PubKeyAlgoEdDSA — see the package doc for
// the architectural rationale.
func Entity(signer crypto.Signer, name, email string, creationTime time.Time) (*openpgp.Entity, error) {
	rsaPub, ok := signer.Public().(*rsa.PublicKey)
	if !ok {
		return nil, errors.Wrapf(ErrUnsupportedKeyType, "got %T", signer.Public())
	}

	pubPkt := packet.NewRSAPublicKey(creationTime, rsaPub)
	// Construct the private-key packet directly (rather than
	// packet.NewSignerPrivateKey, which panics on opaque signers):
	// the crypto.Signer drives the actual signing, so a KMS-backed
	// signer works here.
	privPkt := &packet.PrivateKey{PublicKey: *pubPkt, PrivateKey: signer}

	ent := &openpgp.Entity{
		PrimaryKey: pubPkt,
		PrivateKey: privPkt,
		Identities: make(map[string]*openpgp.Identity),
		Subkeys:    []openpgp.Subkey{},
		Signatures: []*packet.Signature{},
	}

	cfg := &packet.Config{Time: func() time.Time { return creationTime }}

	// AddUserId attaches the User ID and signs the binding self-
	// signature with ent.PrivateKey, marking the key sign+certify
	// capable.
	if err := ent.AddUserId(name, "", email, cfg); err != nil {
		return nil, errors.Wrap(err, "adding user id / self-signature")
	}

	return ent, nil
}

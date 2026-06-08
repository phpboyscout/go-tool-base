// Package openpgpkey mints an ASCII-armored OpenPGP public key from a
// crypto.Signer. It exists to bridge a KMS/HSM-held signing key (which
// exposes only a public key plus a remote Sign operation) into the
// OpenPGP form that the self-update verifier (pkg/setup) and Web Key
// Directory require.
//
// Why this is needed: the verifier rejects a bare public-key packet
// ("v4 entity without any identities") — it needs a User ID and a
// self-signature. Producing that self-signature requires *signing* with
// the private key. go-crypto signs RSA (and ECDSA) keys through the
// crypto.Signer interface, so an opaque KMS-backed signer works without
// the private key ever leaving the KMS.
//
// Usage:
//   - Local/dev: pass a concrete *rsa.PrivateKey.
//   - Production: pass a crypto.Signer whose Sign() calls AWS KMS (or
//     another HSM). Its Public() must return the *rsa.PublicKey.
//
// Only RSA keys are supported, matching the Phase 2 KMS choice
// (RSA-4096). See docs/development/phase2-signing-prep.md.
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
	ent, err := entity(signer, name, email, creationTime)
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

// entity assembles a self-signed, signing-capable OpenPGP entity around
// signer: an RSA public-key packet, a User ID, and a positive-cert
// self-signature produced via signer.Sign (the crypto.Signer path).
func entity(signer crypto.Signer, name, email string, creationTime time.Time) (*openpgp.Entity, error) {
	rsaPub, ok := signer.Public().(*rsa.PublicKey)
	if !ok {
		return nil, errors.Newf("unsupported key type %T: only RSA is supported (KMS signing keys are RSA)", signer.Public())
	}

	pub := packet.NewRSAPublicKey(creationTime, rsaPub)

	// Construct the private-key packet directly (rather than
	// packet.NewSignerPrivateKey, which panics on opaque signers): the
	// public half is derived above and the crypto.Signer drives the
	// actual signing, so a KMS-backed signer works here.
	priv := &packet.PrivateKey{PublicKey: *pub, PrivateKey: signer}

	ent := &openpgp.Entity{
		PrimaryKey: pub,
		PrivateKey: priv,
		Identities: make(map[string]*openpgp.Identity),
		Subkeys:    []openpgp.Subkey{},
		Signatures: []*packet.Signature{},
	}

	cfg := &packet.Config{Time: func() time.Time { return creationTime }}

	// AddUserId attaches the User ID and signs the binding self-signature
	// with ent.PrivateKey (the crypto.Signer), marking the key
	// sign+certify capable.
	if err := ent.AddUserId(name, "", email, cfg); err != nil {
		return nil, errors.Wrap(err, "adding user id / self-signature")
	}

	return ent, nil
}

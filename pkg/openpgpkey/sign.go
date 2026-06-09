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

// DetachSign produces an ASCII-armored OpenPGP detached signature
// over data using signer.
//
// publicKey is the armored OpenPGP public-key block that identifies
// the signer (the .asc file previously produced by ArmoredPublicKey
// and embedded in the verifier's trust set / published via WKD). Its
// creation time and primary-UID drive the OpenPGP entity that
// DetachSign rebuilds around signer, so the signer-fingerprint that
// lands in the signature is identical to publicKey's fingerprint.
//
// signer must be the private half of the same RSA key as publicKey
// — if it isn't, the resulting signature will name publicKey's
// fingerprint but the underlying signature bytes will be wrong, and
// every verifier will reject it.
//
// sigCreationTime is the signature's own creation-time subpacket
// (RFC 4880 §5.2.3.4). It is independent of the key's creation time
// embedded in publicKey. Pinning it produces byte-identical signatures
// across re-runs over the same content.
//
// Errors:
//   - ErrUnsupportedKeyType if signer.Public() is not *rsa.PublicKey
//     (matches ArmoredPublicKey's contract).
//   - If publicKey cannot be parsed as a single-entity armored key
//     ring.
//   - If signer.Public() disagrees with the public half embedded in
//     publicKey (we'd be producing a signature with a fingerprint
//     that doesn't match the actual signing key).
//   - Any error from signer.Sign() (KMS network/permissions/etc.).
//   - Any error from go-crypto's packet framing.
//
// API stability: Beta. Surface is additive to ArmoredPublicKey /
// Entity / WriteWKDTree.
func DetachSign(signer crypto.Signer, publicKey []byte, data io.Reader, sigCreationTime time.Time) ([]byte, error) {
	signerPub, ok := signer.Public().(*rsa.PublicKey)
	if !ok {
		return nil, errors.Wrapf(ErrUnsupportedKeyType, "DetachSign: %T", signer.Public())
	}

	entity, err := loadIdentityFromArmoredKey(publicKey)
	if err != nil {
		return nil, errors.Wrap(err, "loading public key identity")
	}

	if err := assertSignerMatchesPublicKey(signerPub, entity); err != nil {
		return nil, err
	}

	// Replace the entity's PrivateKey with our signer-backed shim so
	// go-crypto's signing path drives the remote signer (KMS) for the
	// actual signature computation. The PUBLIC half stays exactly as
	// parsed from publicKey, so the fingerprint that ends up in the
	// emitted signature subpacket matches what verifiers expect.
	entity.PrivateKey = &packet.PrivateKey{
		PublicKey:  *entity.PrimaryKey,
		PrivateKey: signer,
	}

	// Round to whole seconds — OpenPGP v4 sig packets store creation
	// time as uint32 seconds since epoch (RFC 4880 §5.2.3.4); sub-
	// second precision would be silently truncated at encode time.
	sigCreationTime = sigCreationTime.Truncate(time.Second)

	body, err := io.ReadAll(data)
	if err != nil {
		return nil, errors.Wrap(err, "reading input data")
	}

	// Disable go-crypto's random-salt notation (added by default as a
	// fault-injection defence for EdDSA on consumer hardware). For us
	// the signer is AWS KMS, which has its own HSM-level defences, and
	// the salt notation defeats the spec D4 reproducibility property.
	deterministic := false
	cfg := &packet.Config{
		Time:                                  func() time.Time { return sigCreationTime },
		NonDeterministicSignaturesViaNotation: &deterministic,
	}

	var sigBuf bytes.Buffer
	if err := openpgp.DetachSign(&sigBuf, entity, bytes.NewReader(body), cfg); err != nil {
		return nil, errors.Wrap(err, "computing detached signature")
	}

	var armored bytes.Buffer

	enc, err := armor.Encode(&armored, openpgp.SignatureType, nil)
	if err != nil {
		return nil, errors.WithStack(err)
	}

	if _, err := io.Copy(enc, &sigBuf); err != nil {
		return nil, errors.Wrap(err, "armoring signature")
	}

	if err := enc.Close(); err != nil {
		return nil, errors.WithStack(err)
	}

	return armored.Bytes(), nil
}

// loadIdentityFromArmoredKey parses an armored OpenPGP key block and
// returns the contained Entity. We expect a single-entity public-key
// file (the shape ArmoredPublicKey emits and the standard layout for
// release-signing keys).
func loadIdentityFromArmoredKey(publicKey []byte) (*openpgp.Entity, error) {
	ring, err := openpgp.ReadArmoredKeyRing(bytes.NewReader(publicKey))
	if err != nil {
		return nil, errors.Wrap(err, "parsing armored public key")
	}

	if len(ring) != 1 {
		return nil, errors.Newf("expected exactly one entity in armored public key, got %d", len(ring))
	}

	if len(ring[0].Identities) == 0 {
		return nil, errors.New("armored public key has no User ID; cannot sign with an unidentified key")
	}

	return ring[0], nil
}

// assertSignerMatchesPublicKey confirms that the public RSA key
// derived from signer matches the public half embedded in entity.
// If they disagree, we would be signing under a fingerprint that
// doesn't match the actual signing key — every verifier would reject.
func assertSignerMatchesPublicKey(signerPub *rsa.PublicKey, entity *openpgp.Entity) error {
	keyPub, ok := entity.PrimaryKey.PublicKey.(*rsa.PublicKey)
	if !ok {
		return errors.Newf("public key embeds %T but signer is RSA", entity.PrimaryKey.PublicKey)
	}

	if keyPub.N.Cmp(signerPub.N) != 0 || keyPub.E != signerPub.E {
		return errors.New("signer's RSA public half does not match the public key block — wrong key?")
	}

	return nil
}

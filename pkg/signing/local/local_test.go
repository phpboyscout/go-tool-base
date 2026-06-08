package local_test

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/pkg/signing"
	"gitlab.com/phpboyscout/go-tool-base/pkg/signing/local"
)

// writePEM is a test helper that writes a PEM file at a path inside
// the test's TempDir and returns the path.
func writePEM(t *testing.T, blockType string, der []byte) string {
	t.Helper()

	dir := t.TempDir()
	path := filepath.Join(dir, "key.pem")

	require.NoError(t, os.WriteFile(path, pem.EncodeToMemory(&pem.Block{
		Type:  blockType,
		Bytes: der,
	}), 0o600))

	return path
}

func TestRegistration(t *testing.T) {
	// init() in local.go registers the backend at package load.
	names := signing.Names()
	assert.Contains(t, names, "local",
		"importing pkg/signing/local must register the local backend")

	b, err := signing.Get("local")
	require.NoError(t, err)
	assert.Equal(t, "local", b.Name())
}

func TestNewSigner_PKCS1_HappyPath(t *testing.T) {
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	der := x509.MarshalPKCS1PrivateKey(priv)
	path := writePEM(t, "RSA PRIVATE KEY", der)

	signer, err := local.NewSigner(context.Background(), path)
	require.NoError(t, err)

	got, ok := signer.Public().(*rsa.PublicKey)
	require.True(t, ok, "Public() must return *rsa.PublicKey")
	assert.Equal(t, priv.N.Bytes(), got.N.Bytes(),
		"public modulus must match the PEM-loaded private key")
}

func TestNewSigner_PKCS8_HappyPath(t *testing.T) {
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	der, err := x509.MarshalPKCS8PrivateKey(priv)
	require.NoError(t, err)

	path := writePEM(t, "PRIVATE KEY", der)

	signer, err := local.NewSigner(context.Background(), path)
	require.NoError(t, err)

	got, ok := signer.Public().(*rsa.PublicKey)
	require.True(t, ok)
	assert.Equal(t, priv.N.Bytes(), got.N.Bytes())
}

func TestNewSigner_EncryptedPEM_Refused(t *testing.T) {
	// We never write a real encrypted PKCS#8 here — the stdlib
	// can't produce one and we can't read one. Synthesising the
	// header is sufficient to drive the "encrypted PEM rejected"
	// branch.
	path := writePEM(t, "ENCRYPTED PRIVATE KEY", []byte{0x30, 0x00})

	_, err := local.NewSigner(context.Background(), path)
	require.Error(t, err)
	assert.ErrorIs(t, err, local.ErrEncryptedPEMUnsupported,
		"encrypted PEMs must surface ErrEncryptedPEMUnsupported, not a parse error")
}

func TestNewSigner_NonRSA_PKCS8(t *testing.T) {
	ec, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	der, err := x509.MarshalPKCS8PrivateKey(ec)
	require.NoError(t, err)

	path := writePEM(t, "PRIVATE KEY", der)

	_, err = local.NewSigner(context.Background(), path)
	require.Error(t, err)
	assert.ErrorIs(t, err, local.ErrUnsupportedKeyType,
		"non-RSA PKCS#8 keys must surface ErrUnsupportedKeyType")
}

func TestNewSigner_UnknownPEMType(t *testing.T) {
	path := writePEM(t, "EC PRIVATE KEY", []byte{0x30, 0x00})

	_, err := local.NewSigner(context.Background(), path)
	require.Error(t, err)
	assert.ErrorIs(t, err, local.ErrMissingPEMBlock,
		"unrecognised PEM types are wrapped through ErrMissingPEMBlock")
}

func TestNewSigner_NoPEMBlock(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "not-pem.txt")
	require.NoError(t, os.WriteFile(path, []byte("this is not a PEM file\n"), 0o644))

	_, err := local.NewSigner(context.Background(), path)
	require.Error(t, err)
	assert.ErrorIs(t, err, local.ErrMissingPEMBlock)
}

func TestNewSigner_FileNotFound(t *testing.T) {
	_, err := local.NewSigner(context.Background(), "/does/not/exist.pem")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "reading PEM file")
}

func TestNewSigner_PKCS1_MalformedDER(t *testing.T) {
	path := writePEM(t, "RSA PRIVATE KEY", []byte{0x30, 0x00, 0x00})

	_, err := local.NewSigner(context.Background(), path)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "PKCS#1")
}

func TestNewSigner_PKCS8_MalformedDER(t *testing.T) {
	path := writePEM(t, "PRIVATE KEY", []byte{0x30, 0x00, 0x00})

	_, err := local.NewSigner(context.Background(), path)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "PKCS#8")
}

func TestBackend_NewSigner_GoesThroughRegistry(t *testing.T) {
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	der := x509.MarshalPKCS1PrivateKey(priv)
	path := writePEM(t, "RSA PRIVATE KEY", der)

	b, err := signing.Get("local")
	require.NoError(t, err)

	signer, err := b.NewSigner(context.Background(), path)
	require.NoError(t, err)
	assert.IsType(t, &rsa.PrivateKey{}, signer)
}

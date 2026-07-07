package tls_test

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	gtbtls "gitlab.com/phpboyscout/go-tool-base/pkg/tls"
)

// writeTestCert generates a self-signed ECDSA certificate/key pair, writes them
// as PEM files into dir, and returns the cert and key paths.
func writeTestCert(t *testing.T, dir string) (certPath, keyPath string) {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	template := x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "gtb-test"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}

	der, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
	require.NoError(t, err)

	certPath = filepath.Join(dir, "cert.pem")
	keyPath = filepath.Join(dir, "key.pem")

	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	require.NoError(t, os.WriteFile(certPath, certPEM, 0o600))

	keyDER, err := x509.MarshalECPrivateKey(key)
	require.NoError(t, err)

	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	require.NoError(t, os.WriteFile(keyPath, keyPEM, 0o600))

	return certPath, keyPath
}

func TestPair_Certificate(t *testing.T) {
	t.Parallel()

	t.Run("loads valid key pair", func(t *testing.T) {
		t.Parallel()

		certPath, keyPath := writeTestCert(t, t.TempDir())
		pair := gtbtls.Pair{Enabled: true, Cert: certPath, Key: keyPath}

		cert, err := pair.Certificate()
		require.NoError(t, err)
		assert.NotEmpty(t, cert.Certificate)
	})

	t.Run("errors on missing files", func(t *testing.T) {
		t.Parallel()

		pair := gtbtls.Pair{Enabled: true, Cert: "/no/such/cert.pem", Key: "/no/such/key.pem"}

		_, err := pair.Certificate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "loading TLS certificate")
	})
}

func TestPair_ServerConfig(t *testing.T) {
	t.Parallel()

	t.Run("loads certificate with defaults", func(t *testing.T) {
		t.Parallel()

		certPath, keyPath := writeTestCert(t, t.TempDir())
		pair := gtbtls.Pair{Enabled: true, Cert: certPath, Key: keyPath}

		cfg, err := pair.ServerConfig()
		require.NoError(t, err)
		require.NotNil(t, cfg)
		assert.Len(t, cfg.Certificates, 1)
		assert.NotEmpty(t, cfg.CipherSuites)
		assert.Empty(t, cfg.NextProtos)
	})

	t.Run("advertises ALPN protocols", func(t *testing.T) {
		t.Parallel()

		certPath, keyPath := writeTestCert(t, t.TempDir())
		pair := gtbtls.Pair{Enabled: true, Cert: certPath, Key: keyPath}

		cfg, err := pair.ServerConfig("h2", "http/1.1")
		require.NoError(t, err)
		assert.Equal(t, []string{"h2", "http/1.1"}, cfg.NextProtos)
	})

	t.Run("errors when certificate cannot be loaded", func(t *testing.T) {
		t.Parallel()

		pair := gtbtls.Pair{Enabled: true, Cert: "/no/such/cert.pem", Key: "/no/such/key.pem"}

		cfg, err := pair.ServerConfig("h2")
		require.Error(t, err)
		assert.Nil(t, cfg)
	})
}

func TestCertPool_Success(t *testing.T) {
	t.Parallel()

	certPath, _ := writeTestCert(t, t.TempDir())

	pool, err := gtbtls.CertPool(certPath)
	require.NoError(t, err)
	require.NotNil(t, pool)
}

func TestCertPool_NoCertsInFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	empty := filepath.Join(dir, "empty.pem")
	require.NoError(t, os.WriteFile(empty, []byte("not a pem"), 0o600))

	_, err := gtbtls.CertPool(empty)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no certificates found")
}

func TestCertPool_Empty(t *testing.T) {
	t.Parallel()

	pool, err := gtbtls.CertPool()
	require.NoError(t, err)
	require.NotNil(t, pool)
}

func TestClientConfig(t *testing.T) {
	t.Parallel()

	t.Run("no CA files trusts system roots", func(t *testing.T) {
		t.Parallel()

		cfg, err := gtbtls.ClientConfig()
		require.NoError(t, err)
		require.NotNil(t, cfg)
		assert.Nil(t, cfg.RootCAs)
		assert.NotEmpty(t, cfg.CipherSuites)
	})

	t.Run("with CA files sets a custom pool", func(t *testing.T) {
		t.Parallel()

		certPath, _ := writeTestCert(t, t.TempDir())

		cfg, err := gtbtls.ClientConfig(certPath)
		require.NoError(t, err)
		require.NotNil(t, cfg)
		assert.NotNil(t, cfg.RootCAs)
	})

	t.Run("propagates CA pool errors", func(t *testing.T) {
		t.Parallel()

		cfg, err := gtbtls.ClientConfig("/no/such/ca.pem")
		require.Error(t, err)
		assert.Nil(t, cfg)
	})
}

func TestPair_Valid(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		pair gtbtls.Pair
		want bool
	}{
		{"all set", gtbtls.Pair{Enabled: true, Cert: "c", Key: "k"}, true},
		{"disabled", gtbtls.Pair{Enabled: false, Cert: "c", Key: "k"}, false},
		{"missing cert", gtbtls.Pair{Enabled: true, Cert: "", Key: "k"}, false},
		{"missing key", gtbtls.Pair{Enabled: true, Cert: "c", Key: ""}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, tt.pair.Valid())
		})
	}
}

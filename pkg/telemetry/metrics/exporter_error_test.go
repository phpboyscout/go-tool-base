package metrics

import (
	"context"
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

	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/pkg/telemetry/otelcore"
)

// writeSelfSignedCert writes a throwaway self-signed certificate to a temp file
// and returns its path. The OTLP HTTP exporter loads it via
// OTEL_EXPORTER_OTLP_CERTIFICATE, which sets a non-nil TLS client config; pairing
// that with an insecure (plaintext) endpoint makes the exporter constructor fail
// fast with errInsecureEndpointWithTLS — no collector or network involved.
func writeSelfSignedCert(t *testing.T) string {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "test"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
	}

	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	require.NoError(t, err)

	path := filepath.Join(t.TempDir(), "cert.pem")
	f, err := os.Create(path) //nolint:gosec // G304: operator-controlled temp path in test
	require.NoError(t, err)

	require.NoError(t, pem.Encode(f, &pem.Block{Type: "CERTIFICATE", Bytes: der}))
	require.NoError(t, f.Close())

	return path
}

// TestNewProviderEmptyEndpointExporterError covers the error return of the
// env-fallback branch (s.Endpoint == ""): a TLS cert from the environment plus an
// insecure endpoint forces otlpmetrichttp.New(ctx) to fail.
func TestNewProviderEmptyEndpointExporterError(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_CERTIFICATE", writeSelfSignedCert(t))
	t.Setenv("OTEL_EXPORTER_OTLP_INSECURE", "true")

	res := otelcore.Resource("test", "v0.0.0")

	_, err := NewProvider(context.Background(), res, otelcore.Settings{})
	require.Error(t, err)
	require.ErrorContains(t, err, "creating OTLP metric exporter")
}

// TestNewProviderConfiguredEndpointExporterError covers the error return of the
// configured-endpoint branch and the headers-append branch: an explicit insecure
// endpoint with headers, combined with a TLS cert from the environment, forces
// otlpmetrichttp.New(ctx, httpOpts...) to fail.
func TestNewProviderConfiguredEndpointExporterError(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_CERTIFICATE", writeSelfSignedCert(t))

	res := otelcore.Resource("test", "v0.0.0")

	_, err := NewProvider(context.Background(), res, otelcore.Settings{
		Endpoint: "http://localhost:4318",
		Insecure: true,
		Headers:  map[string]string{"x-test": "1"},
	})
	require.Error(t, err)
	require.ErrorContains(t, err, "creating OTLP metric exporter")
}

// TestNewProviderConfiguredEndpointWithHeaders covers the headers-append branch on
// the success path (secure endpoint, no TLS env conflict).
func TestNewProviderConfiguredEndpointWithHeaders(t *testing.T) {
	res := otelcore.Resource("test", "v0.0.0")

	mp, err := NewProvider(context.Background(), res, otelcore.Settings{
		Endpoint: "https://collector.example:4318",
		Headers:  map[string]string{"authorization": "Bearer tok"},
	}, WithInterval(time.Second))
	require.NoError(t, err)
	require.NotNil(t, mp)

	_ = mp.Shutdown(context.Background())
}

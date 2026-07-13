package grpc_test

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/health/grpc_health_v1"

	"gitlab.com/phpboyscout/go/controls"

	gtbgrpc "gitlab.com/phpboyscout/go-tool-base/pkg/grpc"
	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	gtbtls "gitlab.com/phpboyscout/go-tool-base/pkg/tls"
)

// TestTLSListener_ALPNAllowsModernClient is a regression test for the gRPC
// TLS listener advertising HTTP/2 via ALPN. Without "h2" on the listener,
// grpc-go >= 1.67 clients fail the handshake with "missing selected ALPN
// property". This brings up a real TLS gRPC server through the framework and
// dials it with DialLocal (a modern client), proving the connection succeeds.
func TestTLSListener_ALPNAllowsModernClient(t *testing.T) {
	t.Parallel()

	certPath, keyPath := writeSelfSignedCert(t)
	port := freePort(t)

	ctx := context.Background()
	controller := controls.NewController(ctx, controls.WithoutSignals())
	tlsPair := gtbtls.Pair{Enabled: true, Cert: certPath, Key: keyPath}

	_, err := gtbgrpc.Register("grpc", controller, logger.ToSlog(logger.NewNoop()), gtbgrpc.ServerSettings{Port: port}, tlsPair)
	require.NoError(t, err)

	controller.Start()
	t.Cleanup(func() {
		controller.Stop()
		controller.Wait()
	})

	conn, err := gtbgrpc.DialLocal(gtbgrpc.ServerSettings{Port: port}, tlsPair)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })

	callCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	// The Check call drives the TLS handshake; if ALPN h2 is missing it fails
	// here. WaitForReady absorbs the brief server-startup race.
	resp, err := grpc_health_v1.NewHealthClient(conn).Check(
		callCtx, &grpc_health_v1.HealthCheckRequest{},
	)
	require.NoError(t, err)
	require.Equal(t, grpc_health_v1.HealthCheckResponse_SERVING, resp.GetStatus())
}

func freePort(t *testing.T) int {
	t.Helper()

	l, err := net.Listen("tcp", "localhost:0")
	require.NoError(t, err)

	defer func() { _ = l.Close() }()

	return l.Addr().(*net.TCPAddr).Port
}

func writeSelfSignedCert(t *testing.T) (certPath, keyPath string) {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "localhost"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		DNSNames:     []string{"localhost"},
		IPAddresses:  []net.IP{net.IPv4(127, 0, 0, 1)},
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}

	der, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
	require.NoError(t, err)

	dir := t.TempDir()
	certPath = filepath.Join(dir, "cert.pem")
	keyPath = filepath.Join(dir, "key.pem")

	certPEM, err := os.Create(certPath)
	require.NoError(t, err)
	require.NoError(t, pem.Encode(certPEM, &pem.Block{Type: "CERTIFICATE", Bytes: der}))
	require.NoError(t, certPEM.Close())

	keyDER, err := x509.MarshalECPrivateKey(key)
	require.NoError(t, err)

	keyPEM, err := os.Create(keyPath)
	require.NoError(t, err)
	require.NoError(t, pem.Encode(keyPEM, &pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}))
	require.NoError(t, keyPEM.Close())

	return certPath, keyPath
}

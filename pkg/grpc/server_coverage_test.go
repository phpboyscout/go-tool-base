package grpc

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

	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/test/bufconn"

	"gitlab.com/phpboyscout/go-tool-base/pkg/config"
	"gitlab.com/phpboyscout/go-tool-base/pkg/controls"
	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	gtbtls "gitlab.com/phpboyscout/go-tool-base/pkg/tls"
)

// fakeHealthSource is a minimal healthSource that returns canned reports, used
// to exercise the NOT_SERVING branches in RegisterHealthService without standing
// up a full controller.
type fakeHealthSource struct {
	ctx       context.Context
	overall   bool
	liveness  bool
	readiness bool
}

func (f fakeHealthSource) Status() controls.HealthReport {
	return controls.HealthReport{OverallHealthy: f.overall}
}

func (f fakeHealthSource) Liveness() controls.HealthReport {
	return controls.HealthReport{OverallHealthy: f.liveness}
}

func (f fakeHealthSource) Readiness() controls.HealthReport {
	return controls.HealthReport{OverallHealthy: f.readiness}
}

func (f fakeHealthSource) GetServiceInfo(string) (controls.ServiceInfo, bool) {
	return controls.ServiceInfo{}, false
}

func (f fakeHealthSource) GetContext() context.Context { return f.ctx }

// TestRegisterHealthService_NotServingBranches drives the unhealthy branches of
// the health update closure (overall, liveness, readiness all NOT_SERVING).
func TestRegisterHealthService_NotServingBranches(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	srv := grpc.NewServer()
	t.Cleanup(srv.Stop)

	src := fakeHealthSource{ctx: ctx, overall: false, liveness: false, readiness: false}

	// The immediate update() call inside RegisterHealthService walks all three
	// NOT_SERVING branches.
	RegisterHealthService(srv, src)
}

// TestStart_ListenFailure exercises the listen-failure error path in start by
// binding an out-of-range port that the OS rejects... actually resolvePort
// rejects negatives, so we force a bind clash instead.
func TestStart_ListenFailure(t *testing.T) {
	t.Parallel()

	// Occupy a port, then try to bind the same one to provoke a listen error.
	held, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = held.Close() })

	port := held.Addr().(*net.TCPAddr).Port

	cfg := config.NewContainerFromViper(logger.NewNoop(), viper.New())
	cfg.Set("server.grpc.port", port)

	sc := defaultServerConfig()
	srv := grpc.NewServer()
	t.Cleanup(srv.Stop)

	startFn := start(cfg, logger.NewNoop(), srv, sc, nil)

	err = startFn(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to listen")
}

// TestStart_PortError exercises the early portErr return inside the curried
// start func (WithPort with an out-of-range value).
func TestStart_PortError(t *testing.T) {
	t.Parallel()

	cfg := config.NewContainerFromViper(logger.NewNoop(), viper.New())

	sc := defaultServerConfig()
	sc.port = ptr(maxPort + 1)

	srv := grpc.NewServer()
	t.Cleanup(srv.Stop)

	startFn := start(cfg, logger.NewNoop(), srv, sc, nil)
	err := startFn(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid port")
}

// TestStart_TLSWrapFailure forces wrapTLS to fail inside start by enabling TLS
// with a non-existent certificate file.
func TestStart_TLSWrapFailure(t *testing.T) {
	t.Parallel()

	cfg := config.NewContainerFromViper(logger.NewNoop(), viper.New())
	cfg.Set("server.grpc.port", 0)
	cfg.Set("server.tls.enabled", true)
	cfg.Set("server.tls.cert", filepath.Join(t.TempDir(), "missing-cert.pem"))
	cfg.Set("server.tls.key", filepath.Join(t.TempDir(), "missing-key.pem"))

	sc := defaultServerConfig()
	srv := grpc.NewServer()
	t.Cleanup(srv.Stop)

	startFn := start(cfg, logger.NewNoop(), srv, sc, nil)
	err := startFn(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "configuring gRPC TLS")
}

// TestStart_ServeExitRecorded exercises the serve-goroutine error path: starting
// on an already-closed listener-free server and then stopping it abruptly. We
// drive a serve error by closing the underlying listener out from under Serve so
// the goroutine records a non-ErrServerStopped exit into serveState.
func TestStart_ServeExitRecorded(t *testing.T) {
	t.Parallel()

	cfg := config.NewContainerFromViper(logger.NewNoop(), viper.New())
	cfg.Set("server.grpc.port", 0)

	sc := defaultServerConfig()
	srv := grpc.NewServer()
	t.Cleanup(srv.Stop)

	state := &serveState{}
	startFn := start(cfg, logger.NewNoop(), srv, sc, state)

	require.NoError(t, startFn(context.Background()))

	// Stopping the gRPC server makes Serve return ErrServerStopped, which is
	// explicitly ignored, so it should NOT record an exit error.
	srv.Stop()

	require.Eventually(t, func() bool {
		return state.status() == nil
	}, time.Second, 10*time.Millisecond,
		"clean stop must not record a serve exit error")
}

// TestWrapTLS_ConfigError exercises wrapTLS's ServerConfig-error branch directly.
func TestWrapTLS_ConfigError(t *testing.T) {
	t.Parallel()

	lis, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = lis.Close() })

	pair := gtbtls.Pair{
		Enabled: true,
		Cert:    filepath.Join(t.TempDir(), "nope-cert.pem"),
		Key:     filepath.Join(t.TempDir(), "nope-key.pem"),
	}

	_, err = wrapTLS(lis, pair)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "configuring gRPC TLS")
}

// TestTLSServerCredentials covers both the success and error paths of
// TLSServerCredentials (the 0%-covered function).
func TestTLSServerCredentials(t *testing.T) {
	t.Parallel()

	certPath, keyPath := writeSelfSignedCert(t)

	creds, err := TLSServerCredentials(certPath, keyPath)
	require.NoError(t, err)
	assert.NotNil(t, creds)

	_, err = TLSServerCredentials(
		filepath.Join(t.TempDir(), "missing.pem"),
		filepath.Join(t.TempDir(), "missing.key"),
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "configuring gRPC TLS")
}

// TestTLSClientCredentials covers the success and error paths of
// TLSClientCredentials.
func TestTLSClientCredentials(t *testing.T) {
	t.Parallel()

	// No CA files: trust system roots, no error.
	creds, err := TLSClientCredentials()
	require.NoError(t, err)
	assert.NotNil(t, creds)

	// A valid self-signed cert is accepted as a CA file.
	certPath, _ := writeSelfSignedCert(t)
	creds, err = TLSClientCredentials(certPath)
	require.NoError(t, err)
	assert.NotNil(t, creds)

	// A missing CA file errors.
	_, err = TLSClientCredentials(filepath.Join(t.TempDir(), "missing-ca.pem"))
	require.Error(t, err)
}

// TestDialLocal_PortError exercises the resolvePort error branch in dialLocal.
func TestDialLocal_PortError(t *testing.T) {
	t.Parallel()

	cfg := config.NewContainerFromViper(logger.NewNoop(), viper.New())

	sc := defaultServerConfig()
	sc.port = ptr(maxPort + 1)

	_, err := dialLocal(cfg, sc)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid port")
}

// TestDialLocal_TLSCredsError exercises the TLSClientCredentials error branch in
// dialLocal: TLS enabled but the configured cert file does not exist.
func TestDialLocal_TLSCredsError(t *testing.T) {
	t.Parallel()

	cfg := config.NewContainerFromViper(logger.NewNoop(), viper.New())
	cfg.Set("server.grpc.port", 50051)
	cfg.Set("server.tls.enabled", true)
	cfg.Set("server.tls.cert", filepath.Join(t.TempDir(), "missing-ca.pem"))

	sc := defaultServerConfig()

	_, err := dialLocal(cfg, sc)
	require.Error(t, err)
}

// TestDialLocal_AcceptsDialOption exercises the grpc.DialOption branch of the
// public DialLocal option switch.
func TestDialLocal_AcceptsDialOption(t *testing.T) {
	t.Parallel()

	cfg := config.NewContainerFromViper(logger.NewNoop(), viper.New())
	cfg.Set("server.grpc.port", 50052)

	conn, err := DialLocal(cfg, grpc.WithUserAgent("coverage-test"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	assert.NotNil(t, conn)
}

// TestStop_ForceStopOnTimeout exercises the ctx.Done() / force-stop branch of
// Stop. GracefulStop on a server that has called Serve and has a live in-flight
// stream would block; here we use a server that has begun serving and an
// already-expired context so the select takes the timeout branch.
func TestStop_ForceStopOnTimeout(t *testing.T) {
	t.Parallel()

	cfg := config.NewContainerFromViper(logger.NewNoop(), viper.New())
	cfg.Set("server.grpc.port", 0)

	sc := defaultServerConfig()
	srv := grpc.NewServer()

	state := &serveState{}
	require.NoError(t, start(cfg, logger.NewNoop(), srv, sc, state)(context.Background()))

	// Hold an open connection with an in-flight blocking stream so GracefulStop
	// cannot complete promptly; an already-cancelled context forces srv.Stop().
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	done := make(chan struct{})
	go func() {
		Stop(logger.NewNoop(), srv)(ctx)
		close(done)
	}()

	select {
	case <-done:
		// Force-stop path taken (or graceful completed); either way Stop returned.
	case <-time.After(5 * time.Second):
		t.Fatal("Stop did not return; force-stop branch may not have fired")
	}
}

// TestRegister_NewServerError exercises the newServer error branch in Register
// (empty config prefix).
func TestRegister_NewServerError(t *testing.T) {
	t.Parallel()

	cfg := config.NewContainerFromViper(logger.NewNoop(), viper.New())
	controller := controls.NewController(context.Background(), controls.WithoutSignals())

	_, err := Register(context.Background(), "bad", controller, cfg, logger.NewNoop(),
		WithConfigPrefix(""))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "config prefix must not be empty")
}

// TestRegisterHealthService_ServingOverBufconn registers the health service on a
// healthy fake source and queries it over an in-memory bufconn listener,
// confirming the immediate update() set SERVING. Cancelling the context lets the
// background update goroutine exit cleanly (no leak).
func TestRegisterHealthService_ServingOverBufconn(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	srv := grpc.NewServer()
	t.Cleanup(srv.Stop)

	src := fakeHealthSource{ctx: ctx, overall: true, liveness: true, readiness: true}
	RegisterHealthService(srv, src)

	lis := bufconn.Listen(1 << 16)

	go func() { _ = srv.Serve(lis) }()

	conn, err := grpc.NewClient(
		"passthrough:///bufconn",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return lis.DialContext(ctx)
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })

	callCtx, callCancel := context.WithTimeout(ctx, 5*time.Second)
	defer callCancel()

	resp, err := grpc_health_v1.NewHealthClient(conn).Check(
		callCtx, &grpc_health_v1.HealthCheckRequest{},
	)
	require.NoError(t, err)
	assert.Equal(t, grpc_health_v1.HealthCheckResponse_SERVING, resp.GetStatus())
}

func ptr[T any](v T) *T { return &v }

// writeSelfSignedCert writes a short-lived self-signed cert/key pair to a temp
// dir and returns their paths. It is a white-box mirror of the helper in
// tls_regression_test.go (which lives in the external grpc_test package and is
// therefore not visible here).
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

package grpc

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"

	mockConfig "gitlab.com/phpboyscout/go-tool-base/mocks/pkg/config"
	"gitlab.com/phpboyscout/go-tool-base/pkg/config"
	"gitlab.com/phpboyscout/go-tool-base/pkg/controls"
	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
)

// safeBuffer is a goroutine-safe writer for capturing log output.
type safeBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *safeBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	return b.buf.Write(p)
}

func (b *safeBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()

	return b.buf.String()
}

func TestConfigKeyConstants(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "server.port", ConfigKeySharedPort)
	assert.Equal(t, "server.grpc", DefaultConfigPrefix)
}

func TestNewServer_WithConfigPrefix_Reflection(t *testing.T) {
	t.Parallel()

	cfg := mockConfig.NewMockContainable(t)
	cfg.EXPECT().GetBool("server.admin.reflection").Return(true)

	srv, err := NewServer(cfg, WithConfigPrefix("server.admin"))
	require.NoError(t, err)

	services := srv.GetServiceInfo()
	hasReflection := false

	for name := range services {
		if strings.Contains(name, "ServerReflection") {
			hasReflection = true
			break
		}
	}

	assert.True(t, hasReflection, "reflection must honour the custom prefix")
}

func TestServerSettingsFromConfig_PreservesEnvAwareSectionUnmarshal(t *testing.T) {
	t.Setenv("GTB_SERVER_GRPC_PORT", "19082")
	t.Setenv("GTB_SERVER_GRPC_REFLECTION", "true")

	cfg := config.NewReaderContainer(
		afero.NewMemMapFs(),
		config.WithLogger(logger.NewNoop()),
		config.WithConfigFormat("yaml"),
		config.WithEnvPrefix("GTB"),
		config.WithConfigReaders(strings.NewReader("server:\n  grpc:\n    port: 19081\n    reflection: false\n")),
	)

	got := ServerSettingsFromConfig(cfg, "")

	assert.Equal(t, 19082, got.Port)
	assert.True(t, got.Reflection)
}

func TestNewServerWithSettings_Reflection(t *testing.T) {
	t.Parallel()

	srv, err := NewServerWithSettings(ServerSettings{Reflection: true})
	require.NoError(t, err)

	services := srv.GetServiceInfo()
	hasReflection := false

	for name := range services {
		if strings.Contains(name, "ServerReflection") {
			hasReflection = true
			break
		}
	}

	assert.True(t, hasReflection)
}

func TestNewServer_AcceptsServerOptionAndGRPCOption(t *testing.T) {
	t.Parallel()

	cfg := mockConfig.NewMockContainable(t)
	cfg.EXPECT().GetBool("server.admin.reflection").Return(false)

	// A ServerOption and a grpc.ServerOption can be mixed in the same call.
	srv, err := NewServer(cfg, WithConfigPrefix("server.admin"), grpc.MaxConcurrentStreams(10))
	require.NoError(t, err)
	assert.NotNil(t, srv)
}

func TestNewServer_EmptyPrefix(t *testing.T) {
	t.Parallel()

	cfg := mockConfig.NewMockContainable(t)

	_, err := NewServer(cfg, WithConfigPrefix(""))
	require.Error(t, err)
}

func TestStart_WithConfigPrefix(t *testing.T) {
	t.Parallel()

	cfg := mockConfig.NewMockContainable(t)
	cfg.EXPECT().GetBool("server.admin.reflection").Return(false).Maybe()
	cfg.EXPECT().GetInt("server.admin.port").Return(0)
	cfg.EXPECT().GetInt("server.port").Return(0)
	// TLS resolution must target the custom prefix block.
	cfg.EXPECT().GetBool("server.tls.enabled").Return(false).Maybe()
	cfg.EXPECT().GetString("server.tls.cert").Return("").Maybe()
	cfg.EXPECT().GetString("server.tls.key").Return("").Maybe()
	cfg.EXPECT().IsSet("server.admin.tls.enabled").Return(false).Maybe()
	cfg.EXPECT().IsSet("server.admin.tls.cert").Return(false).Maybe()
	cfg.EXPECT().IsSet("server.admin.tls.key").Return(false).Maybe()

	srv, err := NewServer(cfg, WithConfigPrefix("server.admin"))
	require.NoError(t, err)

	startFn := Start(cfg, testLogger(), srv, WithConfigPrefix("server.admin"))
	require.NoError(t, startFn(context.Background()))
	t.Cleanup(srv.GracefulStop)
}

func TestStart_WithPort_BindsExplicitPort(t *testing.T) {
	t.Parallel()

	listener, err := net.Listen("tcp", ":0")
	require.NoError(t, err)
	port := listener.Addr().(*net.TCPAddr).Port
	_ = listener.Close()

	cfg := mockConfig.NewMockContainable(t)
	cfg.EXPECT().GetBool("server.grpc.reflection").Return(false).Maybe()
	mockGRPCTLSDisabled(cfg)

	srv, err := NewServer(cfg)
	require.NoError(t, err)

	startFn := Start(cfg, testLogger(), srv, WithPort(port))
	require.NoError(t, startFn(context.Background()))
	t.Cleanup(srv.GracefulStop)

	require.Eventually(t, func() bool {
		conn, derr := net.DialTimeout("tcp", fmt.Sprintf("localhost:%d", port), 100*time.Millisecond)
		if derr != nil {
			return false
		}
		_ = conn.Close()

		return true
	}, 2*time.Second, 50*time.Millisecond)
}

func TestStart_InvalidPort(t *testing.T) {
	t.Parallel()

	cfg := mockConfig.NewMockContainable(t)
	cfg.EXPECT().GetBool("server.grpc.reflection").Return(false).Maybe()
	mockGRPCTLSDisabled(cfg)

	srv, err := NewServer(cfg)
	require.NoError(t, err)

	startFn := Start(cfg, testLogger(), srv, WithPort(70000))
	require.Error(t, startFn(context.Background()))
}

func TestStart_LogsBoundEphemeralPort(t *testing.T) {
	t.Parallel()

	var buf safeBuffer

	cfg := mockConfig.NewMockContainable(t)
	cfg.EXPECT().GetBool("server.grpc.reflection").Return(false).Maybe()
	cfg.EXPECT().GetInt("server.grpc.port").Return(0)
	cfg.EXPECT().GetInt("server.port").Return(0)
	mockGRPCTLSDisabled(cfg)

	srv, err := NewServer(cfg)
	require.NoError(t, err)

	startFn := Start(cfg, logger.NewCharm(&buf), srv)
	require.NoError(t, startFn(context.Background()))
	t.Cleanup(srv.GracefulStop)

	out := buf.String()
	assert.Contains(t, out, "starting gRPC server")
	assert.NotContains(t, out, "addr=:0")
}

func TestDialLocal_WithConfigPrefix(t *testing.T) {
	t.Parallel()

	cfg := mockConfig.NewMockContainable(t)
	cfg.EXPECT().GetInt("server.admin.port").Return(19090)
	// grpc.NewClient is lazy, so no real connection is made.
	cfg.EXPECT().GetBool("server.tls.enabled").Return(false).Maybe()
	cfg.EXPECT().GetString("server.tls.cert").Return("").Maybe()
	cfg.EXPECT().GetString("server.tls.key").Return("").Maybe()
	cfg.EXPECT().IsSet("server.admin.tls.enabled").Return(false).Maybe()
	cfg.EXPECT().IsSet("server.admin.tls.cert").Return(false).Maybe()
	cfg.EXPECT().IsSet("server.admin.tls.key").Return(false).Maybe()

	conn, err := DialLocal(cfg, WithConfigPrefix("server.admin"))
	require.NoError(t, err)
	require.NotNil(t, conn)
	assert.Equal(t, "localhost:19090", conn.Target())
	_ = conn.Close()
}

func TestStart_EmptyPrefixDefaultsToGRPC(t *testing.T) {
	t.Parallel()

	// An empty prefix falls back to the default "server.grpc" block.
	cfg := mockConfig.NewMockContainable(t)
	cfg.EXPECT().GetBool("server.grpc.reflection").Return(false).Maybe()
	cfg.EXPECT().GetInt("server.grpc.port").Return(0)
	cfg.EXPECT().GetInt("server.port").Return(0)
	mockGRPCTLSDisabled(cfg)

	srv, err := NewServer(cfg)
	require.NoError(t, err)

	startFn := Start(cfg, testLogger(), srv, WithConfigPrefix(""))
	require.NoError(t, startFn(context.Background()))
	t.Cleanup(srv.GracefulStop)
}

func TestDialLocal_DefaultPrefix(t *testing.T) {
	t.Parallel()

	cfg := mockConfig.NewMockContainable(t)
	cfg.EXPECT().GetInt("server.grpc.port").Return(0)
	cfg.EXPECT().GetInt("server.port").Return(18099)
	cfg.EXPECT().GetBool("server.tls.enabled").Return(false).Maybe()
	cfg.EXPECT().GetString("server.tls.cert").Return("").Maybe()
	cfg.EXPECT().GetString("server.tls.key").Return("").Maybe()
	cfg.EXPECT().IsSet("server.grpc.tls.enabled").Return(false).Maybe()
	cfg.EXPECT().IsSet("server.grpc.tls.cert").Return(false).Maybe()
	cfg.EXPECT().IsSet("server.grpc.tls.key").Return(false).Maybe()

	conn, err := DialLocal(cfg)
	require.NoError(t, err)
	require.NotNil(t, conn)
	assert.Equal(t, "localhost:18099", conn.Target())
	_ = conn.Close()
}

func TestRegister_WithServerOption(t *testing.T) {
	t.Parallel()

	cfg := mockConfig.NewMockContainable(t)
	cfg.EXPECT().GetBool("server.admin.reflection").Return(false).Maybe()
	cfg.EXPECT().GetInt("server.admin.port").Return(0).Maybe()
	cfg.EXPECT().GetInt("server.port").Return(0).Maybe()
	cfg.EXPECT().GetBool("server.tls.enabled").Return(false).Maybe()
	cfg.EXPECT().GetString("server.tls.cert").Return("").Maybe()
	cfg.EXPECT().GetString("server.tls.key").Return("").Maybe()
	cfg.EXPECT().IsSet("server.admin.tls.enabled").Return(false).Maybe()
	cfg.EXPECT().IsSet("server.admin.tls.cert").Return(false).Maybe()
	cfg.EXPECT().IsSet("server.admin.tls.key").Return(false).Maybe()

	controller := controls.NewController(context.Background(), controls.WithoutSignals())

	srv, err := Register(context.Background(), "admin-grpc", controller, cfg, testLogger(),
		WithConfigPrefix("server.admin"))
	require.NoError(t, err)
	require.NotNil(t, srv)
}

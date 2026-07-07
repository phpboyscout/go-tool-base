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

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"

	"gitlab.com/phpboyscout/go-tool-base/pkg/controls"
	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	gtbtls "gitlab.com/phpboyscout/go-tool-base/pkg/tls"
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

	srv, err := NewServer(ServerSettings{Reflection: true}, WithConfigPrefix("server.admin"))
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

func TestNewServer_WithSettingsReflection(t *testing.T) {
	t.Parallel()

	srv, err := NewServer(ServerSettings{Reflection: true})
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

	// A ServerOption and a grpc.ServerOption can be mixed in the same call.
	srv, err := NewServer(ServerSettings{}, WithConfigPrefix("server.admin"), grpc.MaxConcurrentStreams(10))
	require.NoError(t, err)
	assert.NotNil(t, srv)
}

func TestNewServer_EmptyPrefix(t *testing.T) {
	t.Parallel()

	_, err := NewServer(ServerSettings{}, WithConfigPrefix(""))
	require.Error(t, err)
}

func TestStart_WithConfigPrefix(t *testing.T) {
	t.Parallel()

	srv, err := NewServer(ServerSettings{}, WithConfigPrefix("server.admin"))
	require.NoError(t, err)

	startFn := Start(testLogger(), srv, ServerSettings{}, gtbtls.Pair{}, WithConfigPrefix("server.admin"))
	require.NoError(t, startFn(context.Background()))
	t.Cleanup(srv.GracefulStop)
}

func TestStart_WithPort_BindsExplicitPort(t *testing.T) {
	t.Parallel()

	listener, err := net.Listen("tcp", ":0")
	require.NoError(t, err)
	port := listener.Addr().(*net.TCPAddr).Port
	_ = listener.Close()

	srv, err := NewServer(ServerSettings{})
	require.NoError(t, err)

	startFn := Start(testLogger(), srv, ServerSettings{}, gtbtls.Pair{}, WithPort(port))
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

	srv, err := NewServer(ServerSettings{})
	require.NoError(t, err)

	startFn := Start(testLogger(), srv, ServerSettings{}, gtbtls.Pair{}, WithPort(70000))
	require.Error(t, startFn(context.Background()))
}

func TestStart_LogsBoundEphemeralPort(t *testing.T) {
	t.Parallel()

	var buf safeBuffer

	srv, err := NewServer(ServerSettings{})
	require.NoError(t, err)

	startFn := Start(logger.NewCharm(&buf), srv, ServerSettings{}, gtbtls.Pair{})
	require.NoError(t, startFn(context.Background()))
	t.Cleanup(srv.GracefulStop)

	out := buf.String()
	assert.Contains(t, out, "starting gRPC server")
	assert.NotContains(t, out, "addr=:0")
}

func TestDialLocal_WithConfigPrefix(t *testing.T) {
	t.Parallel()

	conn, err := DialLocal(ServerSettings{Port: 19090}, gtbtls.Pair{}, WithConfigPrefix("server.admin"))
	require.NoError(t, err)
	require.NotNil(t, conn)
	assert.Equal(t, "localhost:19090", conn.Target())
	_ = conn.Close()
}

func TestStart_EmptyPrefixDefaultsToGRPC(t *testing.T) {
	t.Parallel()

	srv, err := NewServer(ServerSettings{})
	require.NoError(t, err)

	startFn := Start(testLogger(), srv, ServerSettings{}, gtbtls.Pair{}, WithConfigPrefix(""))
	require.NoError(t, startFn(context.Background()))
	t.Cleanup(srv.GracefulStop)
}

func TestDialLocal_DefaultPrefix(t *testing.T) {
	t.Parallel()

	conn, err := DialLocal(ServerSettings{Port: 18099}, gtbtls.Pair{})
	require.NoError(t, err)
	require.NotNil(t, conn)
	assert.Equal(t, "localhost:18099", conn.Target())
	_ = conn.Close()
}

func TestRegister_WithServerOption(t *testing.T) {
	t.Parallel()

	controller := controls.NewController(context.Background(), controls.WithoutSignals())

	srv, err := Register("admin-grpc", controller, testLogger(), ServerSettings{}, gtbtls.Pair{},
		WithConfigPrefix("server.admin"))
	require.NoError(t, err)
	require.NotNil(t, srv)
}

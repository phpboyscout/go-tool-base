package gateway_test

import (
	"context"
	"net"
	"net/http"
	"testing"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"

	transithttp "gitlab.com/phpboyscout/go/transit/http"
)

// noopRegister is a RegisterFunc that wires nothing — enough to construct a
// gateway handler in tests that only assert on lifecycle/plumbing.
func noopRegister(_ context.Context, _ *runtime.ServeMux, _ *grpc.ClientConn) error {
	return nil
}

// headerMiddleware sets a fixed response header, so a test can assert the
// middleware chain was applied over the gateway's REST surface.
func headerMiddleware(key, value string) transithttp.Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set(key, value)
			next.ServeHTTP(w, r)
		})
	}
}

// freePort returns an OS-assigned free TCP port for binding a test server.
func freePort(t *testing.T) int {
	t.Helper()

	listener, err := net.Listen("tcp", ":0")
	require.NoError(t, err)

	port := listener.Addr().(*net.TCPAddr).Port
	require.NoError(t, listener.Close())

	return port
}

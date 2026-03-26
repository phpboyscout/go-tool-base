//go:build integration

package controls_test

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"net/http"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/health/grpc_health_v1"

	mockConfig "github.com/phpboyscout/go-tool-base/mocks/pkg/config"
	"github.com/phpboyscout/go-tool-base/pkg/controls"
	gtbgrpc "github.com/phpboyscout/go-tool-base/pkg/grpc"
	gtbhttp "github.com/phpboyscout/go-tool-base/pkg/http"
	"github.com/phpboyscout/go-tool-base/pkg/logger"
)

// freePortShutdown obtains a free TCP port for test use.
func freePortShutdown(t *testing.T) int {
	t.Helper()

	l, err := net.Listen("tcp", ":0")
	require.NoError(t, err)

	port := l.Addr().(*net.TCPAddr).Port
	_ = l.Close()

	return port
}

// TestGracefulShutdown_SignalInterrupt reproduces the reported bug where a
// controller with both gRPC and HTTP servers (HTTP acting as a gRPC-Gateway)
// fails to shut down cleanly when an interrupt signal is received.
//
// The expected behaviour is:
//  1. SIGINT is received
//  2. Controller initiates graceful shutdown
//  3. HTTP server drains connections within the shutdown timeout
//  4. gRPC server completes in-flight RPCs and stops
//  5. Controller reaches Stopped state without hanging or error
func TestGracefulShutdown_SignalInterrupt(t *testing.T) {
	httpPort := freePortShutdown(t)
	grpcPort := freePortShutdown(t)

	var logBuf bytes.Buffer
	l := logger.NewCharm(&logBuf, logger.WithLevel(logger.DebugLevel))

	ctx := context.Background()
	controller := controls.NewController(ctx,
		controls.WithLogger(l),
		controls.WithShutdownTimeout(3*time.Second),
	)

	// --- Register gRPC server ---
	grpcCfg := mockConfig.NewMockContainable(t)
	grpcCfg.EXPECT().GetBool("server.grpc.reflection").Return(false).Maybe()
	grpcCfg.EXPECT().GetInt("server.grpc.port").Return(grpcPort)

	grpcSrv, err := gtbgrpc.Register(ctx, "grpc", controller, grpcCfg, l)
	require.NoError(t, err)

	// --- Register HTTP server (simulating gRPC-Gateway) ---
	// In a real setup this would be grpc-gateway, but for reproduction
	// we just proxy health checks to prove both servers are wired up.
	gatewayMux := http.NewServeMux()
	gatewayMux.HandleFunc("/api/health", func(w http.ResponseWriter, r *http.Request) {
		// Simulate a gRPC-Gateway call that connects to the gRPC server
		conn, dialErr := grpc.NewClient(
			fmt.Sprintf("localhost:%d", grpcPort),
			grpc.WithTransportCredentials(insecure.NewCredentials()),
		)
		if dialErr != nil {
			http.Error(w, dialErr.Error(), http.StatusBadGateway)
			return
		}
		defer conn.Close()

		healthClient := grpc_health_v1.NewHealthClient(conn)
		resp, checkErr := healthClient.Check(r.Context(), &grpc_health_v1.HealthCheckRequest{})
		if checkErr != nil {
			http.Error(w, checkErr.Error(), http.StatusBadGateway)
			return
		}

		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprintf(w, "gRPC health: %s", resp.Status)
	})

	httpCfg := mockConfig.NewMockContainable(t)
	httpCfg.EXPECT().GetInt("server.http.port").Return(httpPort)
	httpCfg.EXPECT().GetInt("server.http.max_header_bytes").Return(0).Maybe()
	httpCfg.EXPECT().GetBool("server.tls.enabled").Return(false)
	httpCfg.EXPECT().GetString("server.tls.cert").Return("")
	httpCfg.EXPECT().GetString("server.tls.key").Return("")

	_, err = gtbhttp.Register(ctx, "http-gateway", controller, httpCfg, l, gatewayMux)
	require.NoError(t, err)

	// --- Start the controller ---
	controller.Start()

	// Wait for both servers to be ready
	require.Eventually(t, func() bool {
		resp, httpErr := http.Get(fmt.Sprintf("http://localhost:%d/healthz", httpPort))
		if httpErr != nil {
			return false
		}
		defer resp.Body.Close()
		return resp.StatusCode == http.StatusOK
	}, 5*time.Second, 50*time.Millisecond, "HTTP server should be ready")

	require.Eventually(t, func() bool {
		conn, dialErr := grpc.NewClient(
			fmt.Sprintf("localhost:%d", grpcPort),
			grpc.WithTransportCredentials(insecure.NewCredentials()),
		)
		if dialErr != nil {
			return false
		}
		defer conn.Close()

		healthClient := grpc_health_v1.NewHealthClient(conn)
		resp, checkErr := healthClient.Check(ctx, &grpc_health_v1.HealthCheckRequest{})
		if checkErr != nil {
			return false
		}
		return resp.Status == grpc_health_v1.HealthCheckResponse_SERVING
	}, 5*time.Second, 100*time.Millisecond, "gRPC server should be ready")

	// Verify the gateway route works (HTTP → gRPC)
	resp, err := http.Get(fmt.Sprintf("http://localhost:%d/api/health", httpPort))
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	// Verify we're in Running state
	assert.Equal(t, controls.Running, controller.GetState())

	_ = grpcSrv // keep reference alive

	// --- Send SIGINT to trigger graceful shutdown ---
	t.Log("Sending SIGINT to trigger shutdown...")
	controller.Signals() <- syscall.SIGINT

	// --- Wait for shutdown to complete within a reasonable time ---
	shutdownDone := make(chan struct{})
	go func() {
		controller.Wait()
		close(shutdownDone)
	}()

	select {
	case <-shutdownDone:
		t.Log("Controller shut down successfully")
	case <-time.After(10 * time.Second):
		t.Fatal("SHUTDOWN HUNG: controller did not stop within 10 seconds after SIGINT")
	}

	// --- Verify clean shutdown ---
	assert.Equal(t, controls.Stopped, controller.GetState(), "controller should be in Stopped state")

	// Check logs for shutdown errors
	logs := logBuf.String()
	t.Logf("Shutdown logs:\n%s", logs)

	assert.NotContains(t, logs, "server shutdown failed",
		"HTTP server should shut down without errors")
}

// TestGracefulShutdown_DrainsInflightRequests verifies that when SIGINT is
// received while a long-running HTTP request is in flight (proxied to gRPC),
// the controller waits for the request to complete before stopping, rather
// than killing it immediately.
//
// This is the scenario reported by the user: the HTTP server is acting as a
// gRPC-Gateway and has active connections. On shutdown, the HTTP server needs
// a valid (non-cancelled) context to drain those connections within the
// shutdown timeout window.
func TestGracefulShutdown_DrainsInflightRequests(t *testing.T) {
	httpPort := freePortShutdown(t)
	grpcPort := freePortShutdown(t)

	var logBuf bytes.Buffer
	l := logger.NewCharm(&logBuf, logger.WithLevel(logger.DebugLevel))

	ctx := context.Background()
	controller := controls.NewController(ctx,
		controls.WithLogger(l),
		controls.WithShutdownTimeout(5*time.Second),
	)

	// --- Register gRPC server ---
	grpcCfg := mockConfig.NewMockContainable(t)
	grpcCfg.EXPECT().GetBool("server.grpc.reflection").Return(false).Maybe()
	grpcCfg.EXPECT().GetInt("server.grpc.port").Return(grpcPort)

	_, err := gtbgrpc.Register(ctx, "grpc", controller, grpcCfg, l)
	require.NoError(t, err)

	// --- Register HTTP server with a slow handler ---
	requestStarted := make(chan struct{})
	requestFinished := make(chan struct{})

	gatewayMux := http.NewServeMux()
	gatewayMux.HandleFunc("/api/slow", func(w http.ResponseWriter, _ *http.Request) {
		close(requestStarted)
		// Simulate a long-running request (e.g., gRPC-Gateway streaming)
		time.Sleep(2 * time.Second)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("done"))
		close(requestFinished)
	})

	httpCfg := mockConfig.NewMockContainable(t)
	httpCfg.EXPECT().GetInt("server.http.port").Return(httpPort)
	httpCfg.EXPECT().GetInt("server.http.max_header_bytes").Return(0).Maybe()
	httpCfg.EXPECT().GetBool("server.tls.enabled").Return(false)
	httpCfg.EXPECT().GetString("server.tls.cert").Return("")
	httpCfg.EXPECT().GetString("server.tls.key").Return("")

	_, err = gtbhttp.Register(ctx, "http-gateway", controller, httpCfg, l, gatewayMux)
	require.NoError(t, err)

	// --- Start the controller ---
	controller.Start()

	// Wait for HTTP server to be ready
	require.Eventually(t, func() bool {
		resp, httpErr := http.Get(fmt.Sprintf("http://localhost:%d/healthz", httpPort))
		if httpErr != nil {
			return false
		}
		defer resp.Body.Close()
		return resp.StatusCode == http.StatusOK
	}, 5*time.Second, 50*time.Millisecond, "HTTP server should be ready")

	// --- Start a long-running request ---
	clientResult := make(chan error, 1)
	go func() {
		client := &http.Client{Timeout: 10 * time.Second}
		resp, reqErr := client.Get(fmt.Sprintf("http://localhost:%d/api/slow", httpPort))
		if reqErr != nil {
			clientResult <- reqErr
			return
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			clientResult <- fmt.Errorf("unexpected status: %d", resp.StatusCode)
			return
		}
		clientResult <- nil
	}()

	// Wait for the request to be in-flight
	select {
	case <-requestStarted:
		t.Log("Long-running request is in flight")
	case <-time.After(5 * time.Second):
		t.Fatal("request did not start in time")
	}

	// --- Send SIGINT while the request is still running ---
	t.Log("Sending SIGINT while request is in flight...")
	controller.Signals() <- syscall.SIGINT

	// --- The controller should wait for the request to drain ---
	shutdownDone := make(chan struct{})
	go func() {
		controller.Wait()
		close(shutdownDone)
	}()

	select {
	case <-shutdownDone:
		t.Log("Controller shut down")
	case <-time.After(10 * time.Second):
		t.Fatal("SHUTDOWN HUNG: controller did not stop within 10 seconds")
	}

	// --- Verify the in-flight request completed successfully ---
	select {
	case err := <-clientResult:
		assert.NoError(t, err, "in-flight HTTP request should complete successfully during graceful shutdown")
	case <-time.After(5 * time.Second):
		t.Fatal("client request did not complete")
	}

	// Verify the request handler actually finished
	select {
	case <-requestFinished:
		t.Log("Request handler completed")
	default:
		t.Error("request handler did not finish — shutdown killed it prematurely")
	}

	// Check logs
	logs := logBuf.String()
	t.Logf("Shutdown logs:\n%s", logs)

	assert.Equal(t, controls.Stopped, controller.GetState())
	assert.NotContains(t, logs, "server shutdown failed",
		"HTTP server should drain connections without error")
}

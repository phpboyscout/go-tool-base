package grpc

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"

	mockConfig "github.com/phpboyscout/go-tool-base/mocks/pkg/config"
	"github.com/phpboyscout/go-tool-base/pkg/controls"
	"github.com/phpboyscout/go-tool-base/pkg/logger"
)

func testLogger() logger.Logger {
	return logger.NewNoop()
}

func TestNewServer(t *testing.T) {
	t.Parallel()

	cfg := mockConfig.NewMockContainable(t)

	srv, err := NewServer(cfg)
	require.NoError(t, err)
	assert.NotNil(t, srv)
}

func TestStart_ListenAndServe(t *testing.T) {
	t.Parallel()

	cfg := mockConfig.NewMockContainable(t)
	cfg.EXPECT().GetInt("server.grpc.port").Return(0)
	cfg.EXPECT().GetInt("server.port").Return(0)

	srv, err := NewServer(cfg)
	require.NoError(t, err)

	startFn := Start(cfg, testLogger(), srv)

	errCh := make(chan error, 1)
	go func() {
		errCh <- startFn(context.Background())
	}()

	// Give it time to start
	time.Sleep(100 * time.Millisecond)

	// Graceful stop should cause Start to return nil
	srv.GracefulStop()

	assert.NoError(t, <-errCh)
}

func TestStop_GracefulStop(t *testing.T) {
	t.Parallel()

	cfg := mockConfig.NewMockContainable(t)

	srv, err := NewServer(cfg)
	require.NoError(t, err)

	stopFn := Stop(testLogger(), srv)

	// Should not panic even without a listener
	stopFn(context.Background())
}

func TestRegister(t *testing.T) {
	t.Parallel()

	cfg := mockConfig.NewMockContainable(t)
	cfg.EXPECT().GetInt("server.grpc.port").Return(0)
	cfg.EXPECT().GetInt("server.port").Return(0)

	controller := controls.NewController(context.Background(), controls.WithoutSignals())

	err := Register(context.Background(), "test-grpc", controller, cfg, testLogger())
	assert.NoError(t, err)
}

func TestStatus_ValidServer(t *testing.T) {
	t.Parallel()
	srv := &grpc.Server{}
	err := Status(srv)()
	assert.NoError(t, err)
}

func TestStatus_NilServer(t *testing.T) {
	t.Parallel()
	err := Status(nil)()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "grpc server is nil")
}

func TestGRPCPortConfig_Specific(t *testing.T) {
	t.Parallel()
	cfg := mockConfig.NewMockContainable(t)
	cfg.EXPECT().GetInt("server.grpc.port").Return(9090)
	
	srv, _ := NewServer(cfg)
	startFn := Start(cfg, testLogger(), srv)
	assert.NotNil(t, startFn)
}

func TestGRPCPortConfig_Fallback(t *testing.T) {
	t.Parallel()
	cfg := mockConfig.NewMockContainable(t)
	cfg.EXPECT().GetInt("server.grpc.port").Return(0)
	cfg.EXPECT().GetInt("server.port").Return(8080)
	
	srv, _ := NewServer(cfg)
	startFn := Start(cfg, testLogger(), srv)
	assert.NotNil(t, startFn)
}

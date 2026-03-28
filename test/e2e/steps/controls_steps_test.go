package steps_test

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/cucumber/godog"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/health/grpc_health_v1"

	mockConfig "github.com/phpboyscout/go-tool-base/mocks/pkg/config"
	"github.com/phpboyscout/go-tool-base/pkg/controls"
	gtbgrpc "github.com/phpboyscout/go-tool-base/pkg/grpc"
	gtbhttp "github.com/phpboyscout/go-tool-base/pkg/http"
	"github.com/phpboyscout/go-tool-base/test/e2e/support"
)

type controlsWorldKey struct{}

func getWorld(ctx context.Context) *support.ControllerWorld {
	return ctx.Value(controlsWorldKey{}).(*support.ControllerWorld)
}

func initControlsSteps(ctx *godog.ScenarioContext) {
	ctx.Before(func(ctx context.Context, _ *godog.Scenario) (context.Context, error) {
		world := support.NewControllerWorld()
		return context.WithValue(ctx, controlsWorldKey{}, world), nil
	})

	ctx.After(func(ctx context.Context, _ *godog.Scenario, _ error) (context.Context, error) {
		world := getWorld(ctx)
		world.Cleanup()
		return ctx, nil
	})

	// --- Given steps ---
	ctx.Step(`^a controller with no OS signal handling$`, aControllerWithNoSignals)
	ctx.Step(`^a controller with OS signal handling$`, aControllerWithSignals)
	ctx.Step(`^a controller with OS signal handling and (\d+) second shutdown timeout$`, aControllerWithSignalsAndTimeout)
	ctx.Step(`^a service "([^"]*)" is registered$`, aServiceIsRegistered)
	ctx.Step(`^a service "([^"]*)" is registered with a start error "([^"]*)"$`, aServiceIsRegisteredWithStartError)
	ctx.Step(`^an HTTP server registered on a free port$`, anHTTPServerRegistered)
	ctx.Step(`^a gRPC server registered on a free port$`, aGRPCServerRegistered)
	ctx.Step(`^the HTTP server has a slow handler "([^"]*)" that takes (\d+) seconds$`, theHTTPServerHasSlowHandler)
	ctx.Step(`^a service "([^"]*)" that takes (\d+)ms to start$`, aServiceThatTakesToStart)

	// --- When steps ---
	ctx.Step(`^the controller starts$`, theControllerStarts)
	ctx.Step(`^the controller starts in the background$`, theControllerStartsInBackground)
	ctx.Step(`^the controller stops$`, theControllerStops)
	ctx.Step(`^the controller receives SIGINT$`, theControllerReceivesSIGINT)
	ctx.Step(`^the parent context is cancelled$`, theParentContextIsCancelled)
	ctx.Step(`^(\d+) goroutines call stop concurrently$`, goroutinesCallStopConcurrently)
	ctx.Step(`^a status message is sent$`, aStatusMessageIsSent)
	ctx.Step(`^(\d+) status messages are sent$`, multipleStatusMessagesAreSent)
	ctx.Step(`^a stop message is sent via the message channel$`, aStopMessageIsSent)
	ctx.Step(`^the HTTP server is healthy$`, theHTTPServerIsHealthy)
	ctx.Step(`^the gRPC server is healthy$`, theGRPCServerIsHealthy)
	ctx.Step(`^a client sends a GET request to the slow handler$`, aClientSendsSlowRequest)
	ctx.Step(`^the request is in-flight$`, theRequestIsInFlight)
	ctx.Step(`^the "([^"]*)" service has begun starting$`, theServiceHasBegunStarting)

	// --- Then steps ---
	ctx.Step(`^the controller state is "([^"]*)"$`, theControllerStateIs)
	ctx.Step(`^the controller reaches "([^"]*)" state within (\d+) seconds$`, theControllerReachesStateWithin)
	ctx.Step(`^the service "([^"]*)" has been started$`, theServiceHasBeenStarted)
	ctx.Step(`^the service "([^"]*)" has been stopped exactly (\d+) times?$`, theServiceHasBeenStoppedExactly)
	ctx.Step(`^the service "([^"]*)" status has been checked at least (\d+) times?$`, theServiceStatusCheckedAtLeast)
	ctx.Step(`^the logs contain "([^"]*)" within (\d+) seconds$`, theLogsContainWithin)
	ctx.Step(`^the logs contain "([^"]*)"$`, theLogsContain)
	ctx.Step(`^the logs do not contain "([^"]*)"$`, theLogsDoNotContain)
	ctx.Step(`^the in-flight request completed successfully$`, theInflightRequestCompleted)
}

// --- Given implementations ---

func aControllerWithNoSignals(ctx context.Context) context.Context {
	w := getWorld(ctx)
	w.EnsureController(controls.WithoutSignals())
	return ctx
}

func aControllerWithSignals(ctx context.Context) context.Context {
	w := getWorld(ctx)
	w.EnsureController(controls.WithShutdownTimeout(3 * time.Second))
	return ctx
}

func aControllerWithSignalsAndTimeout(ctx context.Context, seconds int) context.Context {
	w := getWorld(ctx)
	w.EnsureController(controls.WithShutdownTimeout(time.Duration(seconds) * time.Second))
	return ctx
}

func aServiceIsRegistered(ctx context.Context, name string) context.Context {
	w := getWorld(ctx)
	w.RegisterService(name)
	return ctx
}

func aServiceIsRegisteredWithStartError(ctx context.Context, name, errMsg string) context.Context {
	w := getWorld(ctx)
	cntrs := &support.StateCounters{}
	w.Counters[name] = cntrs
	w.Controller.Register(name,
		controls.WithStart(func(_ context.Context) error {
			cntrs.Started.Add(1)
			return fmt.Errorf("%s", errMsg)
		}),
		controls.WithStop(func(_ context.Context) { cntrs.Stopped.Add(1) }),
	)
	return ctx
}

func anHTTPServerRegistered(ctx context.Context) (context.Context, error) {
	w := getWorld(ctx)

	port, err := support.FreePort()
	if err != nil {
		return ctx, err
	}

	w.HTTPPort = port

	cfg := newHTTPMockConfig(port)
	mux := http.NewServeMux()

	_, err = gtbhttp.Register(w.Ctx, "http", w.Controller, cfg, w.Logger, mux)
	if err != nil {
		return ctx, fmt.Errorf("failed to register HTTP server: %w", err)
	}

	return ctx, nil
}

func aGRPCServerRegistered(ctx context.Context) (context.Context, error) {
	w := getWorld(ctx)

	port, err := support.FreePort()
	if err != nil {
		return ctx, err
	}

	w.GRPCPort = port

	cfg := newGRPCMockConfig(port)

	_, err = gtbgrpc.Register(w.Ctx, "grpc", w.Controller, cfg, w.Logger)
	if err != nil {
		return ctx, fmt.Errorf("failed to register gRPC server: %w", err)
	}

	return ctx, nil
}

func theHTTPServerHasSlowHandler(ctx context.Context, path string, seconds int) (context.Context, error) {
	w := getWorld(ctx)
	w.RequestStarted = make(chan struct{})
	w.RequestFinished = make(chan struct{})
	w.ClientResult = make(chan error, 1)

	port, err := support.FreePort()
	if err != nil {
		return ctx, err
	}

	w.HTTPPort = port

	mux := http.NewServeMux()
	mux.HandleFunc(path, func(rw http.ResponseWriter, _ *http.Request) {
		close(w.RequestStarted)
		time.Sleep(time.Duration(seconds) * time.Second)
		rw.WriteHeader(http.StatusOK)
		_, _ = rw.Write([]byte("done"))
		close(w.RequestFinished)
	})

	cfg := newHTTPMockConfig(port)
	_, err = gtbhttp.Register(w.Ctx, "http-slow", w.Controller, cfg, w.Logger, mux)
	if err != nil {
		return ctx, fmt.Errorf("failed to register slow HTTP server: %w", err)
	}

	return ctx, nil
}

func aServiceThatTakesToStart(ctx context.Context, name string, millis int) context.Context {
	w := getWorld(ctx)
	w.StartupDelay = make(chan struct{})
	cntrs := &support.StateCounters{}
	w.Counters[name] = cntrs

	delay := w.StartupDelay
	w.Controller.Register(name,
		controls.WithStart(func(_ context.Context) error {
			close(delay)
			time.Sleep(time.Duration(millis) * time.Millisecond)
			cntrs.Started.Add(1)
			return nil
		}),
		controls.WithStop(func(_ context.Context) { cntrs.Stopped.Add(1) }),
	)

	return ctx
}

// --- When implementations ---

func theControllerStarts(ctx context.Context) context.Context {
	w := getWorld(ctx)
	w.Controller.Start()
	return ctx
}

func theControllerStartsInBackground(ctx context.Context) context.Context {
	w := getWorld(ctx)
	go w.Controller.Start()
	return ctx
}

func theControllerStops(ctx context.Context) context.Context {
	w := getWorld(ctx)
	w.Controller.Stop()
	return ctx
}

func theControllerReceivesSIGINT(ctx context.Context) context.Context {
	w := getWorld(ctx)
	w.Controller.Signals() <- syscall.SIGINT
	return ctx
}

func theParentContextIsCancelled(ctx context.Context) context.Context {
	w := getWorld(ctx)
	w.Cancel()
	return ctx
}

func goroutinesCallStopConcurrently(ctx context.Context, count int) context.Context {
	w := getWorld(ctx)

	var wg sync.WaitGroup
	for i := 0; i < count; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			w.Controller.Stop()
		}()
	}
	wg.Wait()

	return ctx
}

func aStatusMessageIsSent(ctx context.Context) context.Context {
	w := getWorld(ctx)
	w.Controller.Messages() <- controls.Status
	// Give processing time
	time.Sleep(50 * time.Millisecond)
	return ctx
}

func multipleStatusMessagesAreSent(ctx context.Context, count int) context.Context {
	w := getWorld(ctx)
	for i := 0; i < count; i++ {
		w.Controller.Messages() <- controls.Status
		time.Sleep(50 * time.Millisecond)
	}
	return ctx
}

func aStopMessageIsSent(ctx context.Context) context.Context {
	w := getWorld(ctx)
	w.Controller.Messages() <- controls.Stop
	return ctx
}

func theHTTPServerIsHealthy(ctx context.Context) (context.Context, error) {
	w := getWorld(ctx)
	deadline := time.After(5 * time.Second)
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-deadline:
			return ctx, fmt.Errorf("HTTP server did not become healthy within 5 seconds (port %d)", w.HTTPPort)
		case <-ticker.C:
			resp, err := http.Get(fmt.Sprintf("http://localhost:%d/healthz", w.HTTPPort))
			if err != nil {
				continue
			}
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return ctx, nil
			}
		}
	}
}

func theGRPCServerIsHealthy(ctx context.Context) (context.Context, error) {
	w := getWorld(ctx)
	deadline := time.After(5 * time.Second)
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-deadline:
			return ctx, fmt.Errorf("gRPC server did not become healthy within 5 seconds (port %d)", w.GRPCPort)
		case <-ticker.C:
			conn, err := grpc.NewClient(
				fmt.Sprintf("localhost:%d", w.GRPCPort),
				grpc.WithTransportCredentials(insecure.NewCredentials()),
			)
			if err != nil {
				continue
			}

			healthClient := grpc_health_v1.NewHealthClient(conn)
			resp, err := healthClient.Check(ctx, &grpc_health_v1.HealthCheckRequest{})
			conn.Close()
			if err != nil {
				continue
			}
			if resp.Status == grpc_health_v1.HealthCheckResponse_SERVING {
				return ctx, nil
			}
		}
	}
}

func aClientSendsSlowRequest(ctx context.Context) context.Context {
	w := getWorld(ctx)
	go func() {
		client := &http.Client{Timeout: 10 * time.Second}
		resp, err := client.Get(fmt.Sprintf("http://localhost:%d/api/slow", w.HTTPPort))
		if err != nil {
			w.ClientResult <- err
			return
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			w.ClientResult <- fmt.Errorf("unexpected status: %d", resp.StatusCode)
			return
		}
		w.ClientResult <- nil
	}()
	return ctx
}

func theRequestIsInFlight(ctx context.Context) (context.Context, error) {
	w := getWorld(ctx)
	select {
	case <-w.RequestStarted:
		return ctx, nil
	case <-time.After(5 * time.Second):
		return ctx, fmt.Errorf("request did not start within 5 seconds")
	}
}

func theServiceHasBegunStarting(ctx context.Context, name string) (context.Context, error) {
	w := getWorld(ctx)
	_ = name // used for readability in the feature file
	select {
	case <-w.StartupDelay:
		return ctx, nil
	case <-time.After(5 * time.Second):
		return ctx, fmt.Errorf("service %q did not begin starting within 5 seconds", name)
	}
}

// --- Then implementations ---

func theControllerStateIs(ctx context.Context, expected string) error {
	w := getWorld(ctx)
	actual := string(w.Controller.GetState())
	if actual != expected {
		return fmt.Errorf("expected controller state %q, got %q", expected, actual)
	}
	return nil
}

func theControllerReachesStateWithin(ctx context.Context, expected string, seconds int) error {
	w := getWorld(ctx)
	return w.WaitForState(controls.State(expected), time.Duration(seconds)*time.Second)
}

func theServiceHasBeenStarted(ctx context.Context, name string) error {
	w := getWorld(ctx)
	cntrs, ok := w.Counters[name]
	if !ok {
		return fmt.Errorf("service %q not registered", name)
	}

	deadline := time.After(2 * time.Second)
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-deadline:
			return fmt.Errorf("service %q was not started (started count: %d)", name, cntrs.Started.Load())
		case <-ticker.C:
			if cntrs.Started.Load() > 0 {
				return nil
			}
		}
	}
}

func theServiceHasBeenStoppedExactly(ctx context.Context, name string, count int64) error {
	w := getWorld(ctx)
	cntrs, ok := w.Counters[name]
	if !ok {
		return fmt.Errorf("service %q not registered", name)
	}

	// Allow a brief window for the stop to be recorded
	deadline := time.After(2 * time.Second)
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-deadline:
			actual := cntrs.Stopped.Load()
			if actual != count {
				return fmt.Errorf("service %q stopped %d times, expected %d", name, actual, count)
			}
			return nil
		case <-ticker.C:
			if cntrs.Stopped.Load() == count {
				return nil
			}
		}
	}
}

func theServiceStatusCheckedAtLeast(ctx context.Context, name string, count int64) error {
	w := getWorld(ctx)
	cntrs, ok := w.Counters[name]
	if !ok {
		return fmt.Errorf("service %q not registered", name)
	}

	deadline := time.After(2 * time.Second)
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-deadline:
			actual := cntrs.Statused.Load()
			if actual < count {
				return fmt.Errorf("service %q status checked %d times, expected at least %d", name, actual, count)
			}
			return nil
		case <-ticker.C:
			if cntrs.Statused.Load() >= count {
				return nil
			}
		}
	}
}

func theLogsContainWithin(ctx context.Context, substr string, seconds int) error {
	w := getWorld(ctx)

	deadline := time.After(time.Duration(seconds) * time.Second)
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-deadline:
			return fmt.Errorf("logs did not contain %q within %d seconds\nlogs:\n%s", substr, seconds, w.LogBuf.String())
		case <-ticker.C:
			if strings.Contains(w.LogBuf.String(), substr) {
				return nil
			}
		}
	}
}

func theLogsContain(ctx context.Context, substr string) error {
	w := getWorld(ctx)
	if !strings.Contains(w.LogBuf.String(), substr) {
		return fmt.Errorf("logs do not contain %q\nlogs:\n%s", substr, w.LogBuf.String())
	}
	return nil
}

func theLogsDoNotContain(ctx context.Context, substr string) error {
	w := getWorld(ctx)
	if strings.Contains(w.LogBuf.String(), substr) {
		return fmt.Errorf("logs unexpectedly contain %q", substr)
	}
	return nil
}

func theInflightRequestCompleted(ctx context.Context) error {
	w := getWorld(ctx)
	select {
	case err := <-w.ClientResult:
		if err != nil {
			return fmt.Errorf("in-flight request failed: %w", err)
		}
		return nil
	case <-time.After(5 * time.Second):
		return fmt.Errorf("in-flight request did not complete within 5 seconds")
	}
}

// --- Mock config helpers ---

// newHTTPMockConfig creates a mock Containable configured for HTTP server registration.
// We use mock.Mock directly since godog.TestingT doesn't implement Cleanup.
func newHTTPMockConfig(port int) *mockConfig.MockContainable {
	cfg := &mockConfig.MockContainable{}
	cfg.EXPECT().GetInt("server.http.port").Return(port)
	cfg.EXPECT().GetInt("server.http.max_header_bytes").Return(0).Maybe()
	cfg.EXPECT().GetBool("server.tls.enabled").Return(false)
	cfg.EXPECT().GetString("server.tls.cert").Return("")
	cfg.EXPECT().GetString("server.tls.key").Return("")
	return cfg
}

// newGRPCMockConfig creates a mock Containable configured for gRPC server registration.
func newGRPCMockConfig(port int) *mockConfig.MockContainable {
	cfg := &mockConfig.MockContainable{}
	cfg.EXPECT().GetBool("server.grpc.reflection").Return(false).Maybe()
	cfg.EXPECT().GetInt("server.grpc.port").Return(port)
	return cfg
}

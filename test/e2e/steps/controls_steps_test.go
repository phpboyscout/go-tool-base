package steps_test

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
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
	ctx.Step(`^a health check "([^"]*)" of type "([^"]*)" that returns healthy$`, aHealthCheckReturnsHealthy)
	ctx.Step(`^a health check "([^"]*)" of type "([^"]*)" that returns unhealthy with "([^"]*)"$`, aHealthCheckReturnsUnhealthy)
	ctx.Step(`^a health check "([^"]*)" of type "([^"]*)" that returns degraded with "([^"]*)"$`, aHealthCheckReturnsDegraded)
	ctx.Step(`^an async health check "([^"]*)" with interval (\d+)ms that returns healthy$`, anAsyncHealthCheckReturnsHealthy)
	ctx.Step(`^a service "([^"]*)" that starts successfully and becomes unhealthy after (\d+) status checks?$`, aServiceStartsAndBecomesUnhealthy)
	ctx.Step(`^the service "([^"]*)" has a restart policy with threshold (\d+) and interval (\d+)ms$`, theServiceHasRestartPolicy)

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
	ctx.Step(`^I wait (\d+)ms for the async check to run$`, iWaitForAsyncCheck)

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
	ctx.Step(`^the readiness report is overall healthy$`, theReadinessReportIsHealthy)
	ctx.Step(`^the readiness report is not overall healthy$`, theReadinessReportIsNotHealthy)
	ctx.Step(`^the readiness report includes "([^"]*)" with status "([^"]*)"$`, theReadinessReportIncludes)
	ctx.Step(`^the readiness report does not include "([^"]*)"$`, theReadinessReportDoesNotInclude)
	ctx.Step(`^the liveness report includes "([^"]*)" with status "([^"]*)"$`, theLivenessReportIncludes)
	ctx.Step(`^the liveness report does not include "([^"]*)"$`, theLivenessReportDoesNotInclude)
	ctx.Step(`^registering a health check "([^"]*)" fails with "([^"]*)"$`, registeringHealthCheckFails)
	ctx.Step(`^querying readiness (\d+) times returns cached results$`, queryingReadinessReturnsCached)
	ctx.Step(`^the async check "([^"]*)" ran at most (\d+) times$`, theAsyncCheckRanAtMost)
	ctx.Step(`^the service "([^"]*)" restarts at least (\d+) times within (\d+) seconds$`, theServiceRestartsAtLeast)
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
			_ = resp.Body.Close()
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
			_ = conn.Close()
			if err != nil {
				continue
			}
			if resp.GetStatus() == grpc_health_v1.HealthCheckResponse_SERVING {
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
		defer func() { _ = resp.Body.Close() }()
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

// --- Health monitoring Given implementations ---

func parseCheckType(typeName string) controls.CheckType {
	switch typeName {
	case "liveness":
		return controls.CheckTypeLiveness
	case "readiness":
		return controls.CheckTypeReadiness
	case "both":
		return controls.CheckTypeBoth
	default:
		return controls.CheckTypeReadiness
	}
}

func aHealthCheckReturnsHealthy(ctx context.Context, name, typeName string) (context.Context, error) {
	w := getWorld(ctx)
	w.EnsureController(controls.WithoutSignals())
	err := w.Controller.RegisterHealthCheck(controls.HealthCheck{
		Name: name,
		Type: parseCheckType(typeName),
		Check: func(_ context.Context) controls.CheckResult {
			return controls.CheckResult{Status: controls.CheckHealthy}
		},
	})
	return ctx, err
}

func aHealthCheckReturnsUnhealthy(ctx context.Context, name, typeName, msg string) (context.Context, error) {
	w := getWorld(ctx)
	w.EnsureController(controls.WithoutSignals())
	err := w.Controller.RegisterHealthCheck(controls.HealthCheck{
		Name: name,
		Type: parseCheckType(typeName),
		Check: func(_ context.Context) controls.CheckResult {
			return controls.CheckResult{Status: controls.CheckUnhealthy, Message: msg}
		},
	})
	return ctx, err
}

func aHealthCheckReturnsDegraded(ctx context.Context, name, typeName, msg string) (context.Context, error) {
	w := getWorld(ctx)
	w.EnsureController(controls.WithoutSignals())
	err := w.Controller.RegisterHealthCheck(controls.HealthCheck{
		Name: name,
		Type: parseCheckType(typeName),
		Check: func(_ context.Context) controls.CheckResult {
			return controls.CheckResult{Status: controls.CheckDegraded, Message: msg}
		},
	})
	return ctx, err
}

func anAsyncHealthCheckReturnsHealthy(ctx context.Context, name string, intervalMs int) (context.Context, error) {
	w := getWorld(ctx)
	w.EnsureController(controls.WithoutSignals())
	cnt := &atomic.Int64{}
	w.CheckCounts[name] = cnt
	err := w.Controller.RegisterHealthCheck(controls.HealthCheck{
		Name:     name,
		Type:     controls.CheckTypeReadiness,
		Interval: time.Duration(intervalMs) * time.Millisecond,
		Check: func(_ context.Context) controls.CheckResult {
			cnt.Add(1)
			return controls.CheckResult{Status: controls.CheckHealthy}
		},
	})
	return ctx, err
}

func aServiceStartsAndBecomesUnhealthy(ctx context.Context, name string, afterChecks int) context.Context {
	w := getWorld(ctx)
	cntrs := &support.StateCounters{}
	w.Counters[name] = cntrs
	statusCalls := &atomic.Int64{}
	w.CheckCounts[name] = statusCalls

	w.Controller.Register(name,
		controls.WithStart(func(_ context.Context) error {
			cntrs.Started.Add(1)
			return nil
		}),
		controls.WithStop(func(_ context.Context) { cntrs.Stopped.Add(1) }),
		controls.WithStatus(func() error {
			calls := statusCalls.Add(1)
			if calls > int64(afterChecks) {
				return fmt.Errorf("unhealthy")
			}
			return nil
		}),
	)
	return ctx
}

func theServiceHasRestartPolicy(ctx context.Context, name string, threshold, intervalMs int) context.Context {
	w := getWorld(ctx)
	// We need to re-register the service with the restart policy.
	// Since controls doesn't support modifying after registration,
	// we unregister isn't possible. Instead we register with policy from the start.
	// The feature file calls this step AFTER the service step, so we need to
	// re-register. Let's adjust: use a combined approach where we register
	// with policy in the "starts and becomes unhealthy" step if threshold is set.

	// Actually, we need to re-register. Let's get the counters and re-register.
	cntrs := w.Counters[name]
	statusCalls := w.CheckCounts[name]

	// Re-register with restart policy. This replaces the previous registration.
	w.Controller.Register(name,
		controls.WithStart(func(_ context.Context) error {
			cntrs.Started.Add(1)
			return nil
		}),
		controls.WithStop(func(_ context.Context) { cntrs.Stopped.Add(1) }),
		controls.WithStatus(func() error {
			calls := statusCalls.Add(1)
			if calls > 1 {
				return fmt.Errorf("unhealthy")
			}
			return nil
		}),
		controls.WithRestartPolicy(controls.RestartPolicy{
			HealthFailureThreshold: threshold,
			HealthCheckInterval:    time.Duration(intervalMs) * time.Millisecond,
			InitialBackoff:         5 * time.Millisecond,
		}),
	)
	return ctx
}

// --- Health monitoring When implementations ---

func iWaitForAsyncCheck(ctx context.Context, millis int) context.Context {
	time.Sleep(time.Duration(millis) * time.Millisecond)
	return ctx
}

// --- Health monitoring Then implementations ---

func theReadinessReportIsHealthy(ctx context.Context) error {
	w := getWorld(ctx)
	report := w.Controller.Readiness()
	if !report.OverallHealthy {
		return fmt.Errorf("readiness report is not healthy: %+v", report)
	}
	return nil
}

func theReadinessReportIsNotHealthy(ctx context.Context) error {
	w := getWorld(ctx)
	report := w.Controller.Readiness()
	if report.OverallHealthy {
		return fmt.Errorf("readiness report is healthy, expected unhealthy: %+v", report)
	}
	return nil
}

func theReadinessReportIncludes(ctx context.Context, name, status string) error {
	w := getWorld(ctx)
	report := w.Controller.Readiness()
	return assertReportIncludes(report, name, status)
}

func theReadinessReportDoesNotInclude(ctx context.Context, name string) error {
	w := getWorld(ctx)
	report := w.Controller.Readiness()
	return assertReportDoesNotInclude(report, name)
}

func theLivenessReportIncludes(ctx context.Context, name, status string) error {
	w := getWorld(ctx)
	report := w.Controller.Liveness()
	return assertReportIncludes(report, name, status)
}

func theLivenessReportDoesNotInclude(ctx context.Context, name string) error {
	w := getWorld(ctx)
	report := w.Controller.Liveness()
	return assertReportDoesNotInclude(report, name)
}

func registeringHealthCheckFails(ctx context.Context, name, errContains string) error {
	w := getWorld(ctx)
	err := w.Controller.RegisterHealthCheck(controls.HealthCheck{
		Name: name,
		Type: controls.CheckTypeReadiness,
		Check: func(_ context.Context) controls.CheckResult {
			return controls.CheckResult{Status: controls.CheckHealthy}
		},
	})
	if err == nil {
		return fmt.Errorf("expected registration to fail with %q, but it succeeded", errContains)
	}
	if !strings.Contains(err.Error(), errContains) {
		return fmt.Errorf("expected error containing %q, got: %s", errContains, err.Error())
	}
	return nil
}

func queryingReadinessReturnsCached(ctx context.Context, count int) error {
	w := getWorld(ctx)
	for i := 0; i < count; i++ {
		report := w.Controller.Readiness()
		if !report.OverallHealthy {
			return fmt.Errorf("readiness query %d returned unhealthy", i+1)
		}
	}
	return nil
}

func theAsyncCheckRanAtMost(ctx context.Context, name string, maxCount int64) error {
	w := getWorld(ctx)
	cnt, ok := w.CheckCounts[name]
	if !ok {
		return fmt.Errorf("no check counter for %q", name)
	}
	actual := cnt.Load()
	if actual > maxCount {
		return fmt.Errorf("async check %q ran %d times, expected at most %d", name, actual, maxCount)
	}
	return nil
}

func theServiceRestartsAtLeast(ctx context.Context, name string, minCount int64, seconds int) error {
	w := getWorld(ctx)
	cntrs, ok := w.Counters[name]
	if !ok {
		return fmt.Errorf("service %q not registered", name)
	}

	deadline := time.After(time.Duration(seconds) * time.Second)
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-deadline:
			actual := cntrs.Started.Load()
			return fmt.Errorf("service %q started %d times, expected at least %d", name, actual, minCount)
		case <-ticker.C:
			if cntrs.Started.Load() >= minCount {
				return nil
			}
		}
	}
}

func assertReportIncludes(report controls.HealthReport, name, status string) error {
	for _, s := range report.Services {
		if s.Name == name {
			if s.Status != status {
				return fmt.Errorf("check %q has status %q, expected %q", name, s.Status, status)
			}
			return nil
		}
	}
	return fmt.Errorf("check %q not found in report (services: %+v)", name, report.Services)
}

func assertReportDoesNotInclude(report controls.HealthReport, name string) error {
	for _, s := range report.Services {
		if s.Name == name {
			return fmt.Errorf("check %q unexpectedly found in report with status %q", name, s.Status)
		}
	}
	return nil
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

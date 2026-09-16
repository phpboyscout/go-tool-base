package steps_test

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"syscall"
	"time"

	"gitlab.com/phpboyscout/go/config"

	"github.com/cucumber/godog"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/health/grpc_health_v1"

	"gitlab.com/phpboyscout/go/controls"

	transithttp "gitlab.com/phpboyscout/go/transit/http"
	transporthttp "gitlab.com/phpboyscout/go/transport/http"

	"gitlab.com/phpboyscout/go-tool-base/cli/test/e2e/support"
	gtbgrpc "gitlab.com/phpboyscout/go-tool-base/pkg/grpc"
	gtbhttp "gitlab.com/phpboyscout/go-tool-base/pkg/http"
	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
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
	ctx.Step(`^an HTTP server registered on a free port$`, anHTTPServerRegistered)
	ctx.Step(`^an HTTP server with a (\d+) rps rate limiter$`, anHTTPServerWithRateLimiter)
	ctx.Step(`^a gRPC server registered on a free port$`, aGRPCServerRegistered)

	// --- When steps ---
	ctx.Step(`^the controller starts$`, theControllerStarts)
	ctx.Step(`^the controller receives SIGINT$`, theControllerReceivesSIGINT)
	ctx.Step(`^the HTTP server is healthy$`, theHTTPServerIsHealthy)
	ctx.Step(`^the gRPC server is healthy$`, theGRPCServerIsHealthy)
	ctx.Step(`^(\d+) rapid GET requests are sent to "([^"]*)"$`, rapidGETRequestsAreSent)

	// --- Then steps ---
	ctx.Step(`^the controller reaches "([^"]*)" state within (\d+) seconds$`, theControllerReachesStateWithin)
	ctx.Step(`^the logs do not contain "([^"]*)"$`, theLogsDoNotContain)
	ctx.Step(`^(\d+) of the requests succeed with status (\d+)$`, requestsSucceedWithStatus)
	ctx.Step(`^(\d+) of the requests are rejected with status (\d+)$`, requestsRejectedWithStatus)
}

func aControllerWithNoSignals(ctx context.Context) context.Context {
	w := getWorld(ctx)
	w.EnsureController()
	return ctx
}

func aControllerWithSignals(ctx context.Context) context.Context {
	w := getWorld(ctx)
	// WithSignals is required, not optional: signals are opt-in since
	// go/controls v0.2.0, and Signals() is nil without it — so the SIGINT
	// injection step would block forever on a nil channel.
	w.EnsureController(
		controls.WithSignals(),
		controls.WithShutdownTimeout(3*time.Second),
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

	cfg, err := newHTTPConfig(port)
	if err != nil {
		return ctx, fmt.Errorf("failed to build HTTP config: %w", err)
	}

	mux := http.NewServeMux()

	_, err = gtbhttp.RegisterFromReader(w.Ctx, "http", w.Controller, cfg, w.Logger, mux)
	if err != nil {
		return ctx, fmt.Errorf("failed to register HTTP server: %w", err)
	}

	return ctx, nil
}

func anHTTPServerWithRateLimiter(ctx context.Context, rps int) (context.Context, error) {
	w := getWorld(ctx)

	port, err := support.FreePort()
	if err != nil {
		return ctx, err
	}

	w.HTTPPort = port

	cfg, err := newHTTPConfig(port)
	if err != nil {
		return ctx, fmt.Errorf("failed to build HTTP config: %w", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(rw http.ResponseWriter, _ *http.Request) {
		rw.WriteHeader(http.StatusOK)
		_, _ = rw.Write([]byte("ok"))
	})

	// Burst == rps so an instantaneous burst of N>rps requests admits exactly
	// the bucket capacity and rejects the rest before any refill.
	chain := transithttp.NewChain(transithttp.RateLimitMiddleware(logger.ToSlog(w.Logger), transithttp.RateLimitConfig{
		RequestsPerSecond: float64(rps),
		Burst:             rps,
	}))

	_, err = gtbhttp.RegisterFromReader(w.Ctx, "http-ratelimit", w.Controller, cfg, w.Logger, mux, transporthttp.WithMiddleware(chain))
	if err != nil {
		return ctx, fmt.Errorf("failed to register rate-limited HTTP server: %w", err)
	}

	return ctx, nil
}

func rapidGETRequestsAreSent(ctx context.Context, count int, path string) (context.Context, error) {
	w := getWorld(ctx)
	w.RateLimitStatuses = make([]int, 0, count)

	for range count {
		resp, err := http.Get(fmt.Sprintf("http://localhost:%d%s", w.HTTPPort, path))
		if err != nil {
			return ctx, fmt.Errorf("rate-limit request failed: %w", err)
		}

		_ = resp.Body.Close()
		w.RateLimitStatuses = append(w.RateLimitStatuses, resp.StatusCode)
	}

	return ctx, nil
}

func countStatus(statuses []int, want int) int {
	n := 0

	for _, s := range statuses {
		if s == want {
			n++
		}
	}

	return n
}

func requestsSucceedWithStatus(ctx context.Context, want, status int) error {
	w := getWorld(ctx)
	if got := countStatus(w.RateLimitStatuses, status); got != want {
		return fmt.Errorf("expected %d requests with status %d, got %d (all: %v)", want, status, got, w.RateLimitStatuses)
	}

	return nil
}

func requestsRejectedWithStatus(ctx context.Context, want, status int) error {
	w := getWorld(ctx)
	if got := countStatus(w.RateLimitStatuses, status); got != want {
		return fmt.Errorf("expected %d requests rejected with status %d, got %d (all: %v)", want, status, got, w.RateLimitStatuses)
	}

	return nil
}

func aGRPCServerRegistered(ctx context.Context) (context.Context, error) {
	w := getWorld(ctx)

	port, err := support.FreePort()
	if err != nil {
		return ctx, err
	}

	w.GRPCPort = port

	cfg, err := newGRPCConfig(port)
	if err != nil {
		return ctx, fmt.Errorf("failed to build gRPC config: %w", err)
	}

	_, err = gtbgrpc.RegisterFromReader(w.Ctx, "grpc", w.Controller, cfg, w.Logger)
	if err != nil {
		return ctx, fmt.Errorf("failed to register gRPC server: %w", err)
	}

	return ctx, nil
}

func theControllerStarts(ctx context.Context) context.Context {
	w := getWorld(ctx)
	w.Controller.Start()
	return ctx
}

func theControllerReceivesSIGINT(ctx context.Context) (context.Context, error) {
	w := getWorld(ctx)

	// Fail fast rather than deadlocking. Signals are opt-in since go/controls
	// v0.2.0, so a controller built without WithSignals has a nil channel — and
	// a send on a nil channel blocks forever, turning a one-line mistake into a
	// CI job that only fails at the global timeout with a goroutine dump.
	sigs := w.Controller.Signals()
	if sigs == nil {
		return ctx, fmt.Errorf(
			"controller has no signal channel: the scenario says %q, "+
				"so its Given step must construct the controller with controls.WithSignals()",
			"with OS signal handling")
	}

	sigs <- syscall.SIGINT

	return ctx, nil
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

func theControllerReachesStateWithin(ctx context.Context, expected string, seconds int) error {
	w := getWorld(ctx)
	return w.WaitForState(controls.State(expected), time.Duration(seconds)*time.Second)
}

func theLogsDoNotContain(ctx context.Context, substr string) error {
	w := getWorld(ctx)
	if strings.Contains(w.LogBuf.String(), substr) {
		return fmt.Errorf("logs unexpectedly contain %q", substr)
	}
	return nil
}

func viewFromYAML(yaml string) (*config.View, error) {
	store, err := config.NewStore(context.Background(),
		config.WithReaders(config.NamedSource{Name: "e2e", Content: []byte(yaml)}))
	if err != nil {
		return nil, err
	}

	return store.View(), nil
}

func newHTTPConfig(port int) (*config.View, error) {
	return viewFromYAML(fmt.Sprintf("server:\n  http:\n    port: %d\n", port))
}

func newGRPCConfig(port int) (*config.View, error) {
	return viewFromYAML(fmt.Sprintf("server:\n  grpc:\n    port: %d\n    reflection: false\n", port))
}

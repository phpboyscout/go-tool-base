//go:build e2e

package telemetry_test

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
	"go.opentelemetry.io/contrib/bridges/otelslog"
	"go.opentelemetry.io/otel"

	gtbhttp "gitlab.com/phpboyscout/go-tool-base/pkg/http"
	"gitlab.com/phpboyscout/go-tool-base/pkg/telemetry"
	"gitlab.com/phpboyscout/go-tool-base/pkg/telemetry/otelcore"
)

// collectorImage pins the OTel collector used by the e2e. The debug exporter and
// this config shape are stable across recent versions.
const collectorImage = "otel/opentelemetry-collector-contrib:0.114.0"

// collectorConfig accepts OTLP/HTTP and logs every received signal via the debug
// exporter, so the test can assert what arrived by reading the container logs.
const collectorConfig = `
receivers:
  otlp:
    protocols:
      http:
        endpoint: 0.0.0.0:4318
exporters:
  debug:
    verbosity: detailed
service:
  telemetry:
    metrics:
      level: none
  pipelines:
    traces:
      receivers: [otlp]
      exporters: [debug]
    metrics:
      receivers: [otlp]
      exporters: [debug]
    logs:
      receivers: [otlp]
      exporters: [debug]
`

// TestObservabilityE2E stands up a real OTel collector with testcontainers and
// asserts that all three signals reach it over OTLP/HTTP: a server span from the
// HTTP OTelMiddleware, a custom metric, and a log record — all tagged with the
// service resource. The providers are built and flushed exactly as a real service
// would via telemetry.Setup.
//
// Requires docker. Run with:
//
//	go test -tags e2e -run TestObservabilityE2E ./pkg/telemetry/
func TestObservabilityE2E(t *testing.T) {
	ctx := context.Background()

	collector, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        collectorImage,
			ExposedPorts: []string{"4318/tcp"},
			Files: []testcontainers.ContainerFile{{
				Reader:            strings.NewReader(collectorConfig),
				ContainerFilePath: "/etc/otelcol-contrib/config.yaml",
				FileMode:          0o644,
			}},
			WaitingFor: wait.ForLog("Everything is ready"),
		},
		Started: true,
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = collector.Terminate(ctx) })

	host, err := collector.Host(ctx)
	require.NoError(t, err)

	port, err := collector.MappedPort(ctx, "4318/tcp")
	require.NoError(t, err)

	endpoint := fmt.Sprintf("http://%s:%s", host, port.Port())

	// Enable all three signals, pointed at the collector, sampling everything.
	otlp := otelcore.Settings{Enabled: true, Endpoint: endpoint, Insecure: true}
	shutdown, err := telemetry.Setup(ctx, telemetry.ObservabilitySettings{
		ServiceName: "macguffinsvc",
		Version:     "v1.2.3",
		Tracing: telemetry.ObservabilitySignalSettings{
			OTLP:        otlp,
			Sampling:    1,
			SamplingSet: true,
		},
		Metrics: telemetry.ObservabilitySignalSettings{
			OTLP:        otlp,
			Interval:    time.Second,
			IntervalSet: true,
		},
		Logs: telemetry.ObservabilitySignalSettings{OTLP: otlp},
	}, nil)
	require.NoError(t, err)

	// A real server span via the transport middleware (reads the global provider).
	handler := gtbhttp.NewChain(gtbhttp.OTelMiddleware("macguffinsvc")).Then(
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }),
	)
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	resp, err := http.Get(srv.URL + "/v1/macguffins") //nolint:noctx // test
	require.NoError(t, err)
	_ = resp.Body.Close()

	// A custom metric and a log record on the same pipeline.
	counter, err := otel.Meter("e2e").Int64Counter("e2e_requests_total")
	require.NoError(t, err)
	counter.Add(ctx, 1)

	otelslog.NewLogger("e2e").InfoContext(ctx, "e2e log line")

	// Flush every provider to the collector.
	require.NoError(t, shutdown(ctx))

	// The collector logs received data via the debug exporter; poll until all
	// three signals and the service resource appear, or time out.
	require.Eventually(t, func() bool {
		logs := containerLogs(ctx, collector)

		return strings.Contains(logs, "macguffinsvc") &&
			strings.Contains(logs, "ResourceSpans") &&
			strings.Contains(logs, "e2e_requests_total") &&
			strings.Contains(logs, "e2e log line")
	}, 20*time.Second, 500*time.Millisecond, "collector did not receive all three signals")
}

func containerLogs(ctx context.Context, c testcontainers.Container) string {
	rc, err := c.Logs(ctx)
	if err != nil {
		return ""
	}
	defer func() { _ = rc.Close() }()

	b, _ := io.ReadAll(rc)

	return string(b)
}

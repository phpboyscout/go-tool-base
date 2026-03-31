package telemetry

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/phpboyscout/go-tool-base/pkg/props"
	"github.com/phpboyscout/go-tool-base/pkg/setup"
)

const checkTimeout = 5 * time.Second

func init() {
	setup.RegisterChecks(props.TelemetryCmd,
		[]setup.CheckProvider{
			func(p *props.Props) []setup.CheckFunc {
				return []setup.CheckFunc{checkTelemetryStatus, checkTelemetryConnectivity}
			},
		},
	)
}

// checkTelemetryStatus reports whether telemetry is enabled and which backend
// is active. This is informational — never fails.
func checkTelemetryStatus(_ context.Context, p *props.Props) setup.CheckResult {
	if !p.Config.GetBool("telemetry.enabled") {
		return setup.CheckResult{
			Name:    "Telemetry",
			Status:  "skip",
			Message: "disabled",
		}
	}

	info := "enabled"
	if p.Collector != nil {
		info = fmt.Sprintf("enabled — %s", p.Collector.BackendInfo())
	}

	return setup.CheckResult{
		Name:    "Telemetry",
		Status:  "pass",
		Message: info,
	}
}

// checkTelemetryConnectivity verifies that the configured telemetry endpoint
// is reachable. Skipped when telemetry is disabled or using a local-only/noop backend.
func checkTelemetryConnectivity(ctx context.Context, p *props.Props) setup.CheckResult {
	if !p.Config.GetBool("telemetry.enabled") {
		return setup.CheckResult{
			Name:    "Telemetry connectivity",
			Status:  "skip",
			Message: "telemetry disabled",
		}
	}

	endpoint := resolveEndpoint(p)
	if endpoint == "" {
		return setup.CheckResult{
			Name:    "Telemetry connectivity",
			Status:  "skip",
			Message: "no remote endpoint configured",
		}
	}

	checkCtx, cancel := context.WithTimeout(ctx, checkTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(checkCtx, http.MethodHead, endpoint, nil)
	if err != nil {
		return setup.CheckResult{
			Name:    "Telemetry connectivity",
			Status:  "fail",
			Message: "invalid endpoint URL",
			Details: err.Error(),
		}
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return setup.CheckResult{
			Name:    "Telemetry connectivity",
			Status:  "warn",
			Message: "endpoint unreachable",
			Details: fmt.Sprintf("%s — %s", endpoint, err.Error()),
		}
	}

	defer func() { _ = resp.Body.Close() }()

	return setup.CheckResult{
		Name:    "Telemetry connectivity",
		Status:  "pass",
		Message: fmt.Sprintf("reachable (%d)", resp.StatusCode),
	}
}

func resolveEndpoint(p *props.Props) string {
	if p.Tool.Telemetry.OTelEndpoint != "" {
		return p.Tool.Telemetry.OTelEndpoint
	}

	return p.Tool.Telemetry.Endpoint
}

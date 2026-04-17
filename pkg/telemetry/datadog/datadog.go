// Package datadog provides a telemetry backend that sends events to Datadog's
// HTTP Logs Intake API. It maps telemetry.Event to Datadog's native log format
// with region-aware endpoint resolution.
package datadog

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/cockroachdb/errors"

	gtbhttp "github.com/phpboyscout/go-tool-base/pkg/http"
	"github.com/phpboyscout/go-tool-base/pkg/logger"
	"github.com/phpboyscout/go-tool-base/pkg/telemetry"
)

const (
	httpTimeout        = 5 * time.Second
	httpErrorThreshold = 400
	// maxResponseBytes caps bytes read from the Datadog response body.
	// Closes M-4 from docs/development/reports/security-audit-2026-04-17.md.
	maxResponseBytes int64 = 1 << 20 // 1 MiB
)

// Region identifies a Datadog data center region.
type Region string

const (
	RegionUS1 Region = "us1" // default
	RegionUS3 Region = "us3"
	RegionUS5 Region = "us5"
	RegionEU1 Region = "eu1"
	RegionAP1 Region = "ap1"
	RegionAP2 Region = "ap2"
	RegionGOV Region = "gov"
)

var regionEndpoints = map[Region]string{
	RegionUS1: "https://http-intake.logs.datadoghq.com/v1/input",
	RegionUS3: "https://http-intake.logs.us3.datadoghq.com/v1/input",
	RegionUS5: "https://http-intake.logs.us5.datadoghq.com/v1/input",
	RegionEU1: "https://http-intake.logs.datadoghq.eu/v1/input",
	RegionAP1: "https://http-intake.logs.ap1.datadoghq.com/v1/input",
	RegionAP2: "https://http-intake.logs.ap2.datadoghq.com/v1/input",
	RegionGOV: "https://http-intake.logs.ddog-gov.com/v1/input",
}

// Option configures the Datadog backend.
type Option func(*config)

type config struct {
	region   Region
	source   string
	endpoint string
}

// WithRegion sets the Datadog region. Default: RegionUS1.
func WithRegion(region Region) Option {
	return func(c *config) { c.region = region }
}

// WithSource overrides the ddsource tag. Default: "gtb".
func WithSource(source string) Option {
	return func(c *config) { c.source = source }
}

// WithEndpoint overrides the ingest URL. Used by tests pointing at a
// local httptest server, and by deployments that proxy Datadog traffic.
// Takes precedence over WithRegion when both are set.
func WithEndpoint(url string) Option {
	return func(c *config) { c.endpoint = url }
}

type backend struct {
	endpoint string
	apiKey   string
	source   string
	client   *http.Client
	log      logger.Logger
}

// NewBackend creates a Datadog telemetry backend.
// apiKey is the Datadog API key (not an application key).
func NewBackend(apiKey string, log logger.Logger, opts ...Option) telemetry.Backend {
	cfg := &config{
		region: RegionUS1,
		source: "gtb",
	}

	for _, o := range opts {
		o(cfg)
	}

	endpoint := cfg.endpoint
	if endpoint == "" {
		var ok bool

		endpoint, ok = regionEndpoints[cfg.region]
		if !ok {
			endpoint = regionEndpoints[RegionUS1]
		}
	}

	return &backend{
		endpoint: endpoint,
		apiKey:   apiKey,
		source:   cfg.source,
		client:   gtbhttp.NewClient(gtbhttp.WithTimeout(httpTimeout)),
		log:      log,
	}
}

// datadogEntry is the JSON structure for a single Datadog log entry.
type datadogEntry struct {
	Message   string            `json:"message"`
	DDSource  string            `json:"ddsource"`
	DDTags    string            `json:"ddtags"`
	Hostname  string            `json:"hostname"`
	Service   string            `json:"service"`
	Timestamp string            `json:"timestamp"`
	Level     string            `json:"level"`
	Metadata  map[string]string `json:"metadata,omitempty"`
}

func (b *backend) Send(ctx context.Context, events []telemetry.Event) error {
	entries := make([]datadogEntry, 0, len(events))

	for _, e := range events {
		tags := []string{
			fmt.Sprintf("event_type:%s", e.Type),
			fmt.Sprintf("tool_version:%s", e.Version),
			fmt.Sprintf("os:%s", e.OS),
			fmt.Sprintf("arch:%s", e.Arch),
		}

		entries = append(entries, datadogEntry{
			Message:   fmt.Sprintf("%s: %s", e.Type, e.Name),
			DDSource:  b.source,
			DDTags:    strings.Join(tags, ","),
			Hostname:  e.MachineID,
			Service:   e.ToolName,
			Timestamp: e.Timestamp.UTC().Format(time.RFC3339Nano),
			Level:     "info",
			Metadata:  e.Metadata,
		})
	}

	body, err := json.Marshal(entries)
	if err != nil {
		return errors.Wrap(err, "marshalling datadog entries")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, b.endpoint, bytes.NewReader(body))
	if err != nil {
		return errors.Wrap(err, "creating datadog request")
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("DD-API-KEY", b.apiKey)

	resp, err := b.client.Do(req)
	if err != nil {
		return nil // silently drop — telemetry must never block the user
	}

	defer func() {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxResponseBytes))
		_ = resp.Body.Close()
	}()

	if resp.StatusCode >= httpErrorThreshold {
		b.log.Debug("datadog endpoint returned non-success status",
			"status", resp.StatusCode)
	}

	return nil
}

func (b *backend) Close() error { return nil }

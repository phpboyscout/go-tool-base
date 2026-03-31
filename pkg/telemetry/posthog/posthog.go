// Package posthog provides a telemetry backend that sends events to PostHog's
// Capture API using the batch endpoint. It supports US/EU cloud instances and
// self-hosted deployments.
package posthog

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/cockroachdb/errors"

	gtbhttp "github.com/phpboyscout/go-tool-base/pkg/http"
	"github.com/phpboyscout/go-tool-base/pkg/logger"
	"github.com/phpboyscout/go-tool-base/pkg/telemetry"
)

const (
	httpTimeout        = 5 * time.Second
	httpErrorThreshold = 400
)

// Instance identifies a PostHog deployment.
type Instance string

const (
	InstanceUS Instance = "us" // default
	InstanceEU Instance = "eu"
)

var instanceEndpoints = map[Instance]string{
	InstanceUS: "https://us.i.posthog.com/capture/",
	InstanceEU: "https://eu.i.posthog.com/capture/",
}

// Option configures the PostHog backend.
type Option func(*config)

type config struct {
	instance Instance
	endpoint string // custom endpoint for self-hosted; overrides instance
}

// WithInstance sets the PostHog cloud instance. Default: InstanceUS.
func WithInstance(instance Instance) Option {
	return func(c *config) { c.instance = instance }
}

// WithEndpoint sets a custom endpoint for self-hosted PostHog.
// Takes precedence over WithInstance.
func WithEndpoint(endpoint string) Option {
	return func(c *config) { c.endpoint = endpoint }
}

type backend struct {
	endpoint   string
	projectKey string
	client     *http.Client
	log        logger.Logger
}

// NewBackend creates a PostHog telemetry backend.
// projectKey is the PostHog project API key (starts with "phc_").
func NewBackend(projectKey string, log logger.Logger, opts ...Option) telemetry.Backend {
	cfg := &config{
		instance: InstanceUS,
	}

	for _, o := range opts {
		o(cfg)
	}

	endpoint := cfg.endpoint
	if endpoint == "" {
		var ok bool

		endpoint, ok = instanceEndpoints[cfg.instance]
		if !ok {
			endpoint = instanceEndpoints[InstanceUS]
		}
	}

	return &backend{
		endpoint:   endpoint,
		projectKey: projectKey,
		client:     gtbhttp.NewClient(gtbhttp.WithTimeout(httpTimeout)),
		log:        log,
	}
}

// posthogEvent is a single PostHog event within a batch.
type posthogEvent struct {
	Event      string            `json:"event"`
	DistinctID string            `json:"distinct_id"`
	Timestamp  string            `json:"timestamp"`
	Properties map[string]string `json:"properties"`
}

func (b *backend) Send(ctx context.Context, events []telemetry.Event) error {
	batch := make([]posthogEvent, 0, len(events))

	for _, e := range events {
		props := map[string]string{
			"event_name":   e.Name,
			"tool_name":    e.ToolName,
			"tool_version": e.Version,
			"$os":          e.OS,
			"arch":         e.Arch,
		}

		for k, v := range e.Metadata {
			props[k] = v
		}

		batch = append(batch, posthogEvent{
			Event:      string(e.Type),
			DistinctID: e.MachineID,
			Timestamp:  e.Timestamp.UTC().Format(time.RFC3339Nano),
			Properties: props,
		})
	}

	payload := map[string]any{
		"api_key": b.projectKey,
		"batch":   batch,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return errors.Wrap(err, "marshalling posthog batch")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, b.endpoint, bytes.NewReader(body))
	if err != nil {
		return errors.Wrap(err, "creating posthog request")
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := b.client.Do(req)
	if err != nil {
		return nil // silently drop — telemetry must never block the user
	}

	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= httpErrorThreshold {
		b.log.Debug("posthog endpoint returned non-success status",
			"status", resp.StatusCode)
	}

	return nil
}

func (b *backend) Close() error { return nil }

---
title: Create a Custom Telemetry Backend
description: How to implement the telemetry.Backend interface and wire it into your tool for custom analytics platforms.
date: 2026-03-31
tags: [how-to, telemetry, backend, custom, analytics]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# Create a Custom Telemetry Backend

GTB's telemetry framework ships with noop, stdout, file, HTTP, and OTLP backends. These cover most use cases, but you may need a custom backend for internal analytics platforms, message queues, or vendor APIs with proprietary formats.

This guide walks through:

1. Implementing `telemetry.Backend`
2. Wiring the factory into `TelemetryConfig`
3. Handling errors and timeouts
4. Writing unit tests

---

## Step 1: Implement `telemetry.Backend`

The backend interface has two methods:

```go
type Backend interface {
    Send(ctx context.Context, events []Event) error
    Close() error
}
```

**Key requirements:**

- `Send` receives a batch of events. It must be safe for concurrent use.
- `Send` should be non-blocking or short-timeout — telemetry must never slow down the CLI.
- Network errors should be silently dropped (return `nil`). Only return errors for conditions that warrant logging (e.g. marshalling failures).
- `Close` is called during process shutdown. Use it to flush internal buffers or close connections.

Create a new package, e.g. `pkg/telemetry/rabbitmq`:

```go
package rabbitmq

import (
    "context"
    "encoding/json"
    "time"

    "github.com/cockroachdb/errors"

    "github.com/phpboyscout/go-tool-base/pkg/logger"
    "github.com/phpboyscout/go-tool-base/pkg/telemetry"
)

const publishTimeout = 5 * time.Second

type backend struct {
    conn *amqp.Connection
    ch   *amqp.Channel
    queue string
    log  logger.Logger
}

// NewBackend creates a telemetry backend that publishes events to a RabbitMQ queue.
func NewBackend(amqpURL, queue string, log logger.Logger) (telemetry.Backend, error) {
    conn, err := amqp.Dial(amqpURL)
    if err != nil {
        return nil, errors.Wrap(err, "connecting to RabbitMQ")
    }

    ch, err := conn.Channel()
    if err != nil {
        conn.Close()
        return nil, errors.Wrap(err, "opening channel")
    }

    return &backend{conn: conn, ch: ch, queue: queue, log: log}, nil
}
```

---

## Step 2: Implement Send

Map `telemetry.Event` to your platform's format and publish. Handle errors gracefully — telemetry should never block the user.

```go
func (b *backend) Send(ctx context.Context, events []telemetry.Event) error {
    for _, e := range events {
        body, err := json.Marshal(e)
        if err != nil {
            return errors.Wrap(err, "marshalling event")
        }

        pubCtx, cancel := context.WithTimeout(ctx, publishTimeout)

        err = b.ch.PublishWithContext(pubCtx, "", b.queue, false, false,
            amqp.Publishing{
                ContentType: "application/json",
                Body:        body,
            })

        cancel()

        if err != nil {
            // Silently drop — telemetry must not block the user
            b.log.Debug("failed to publish telemetry event",
                "error", err, "event", e.Name)

            return nil
        }
    }

    return nil
}

func (b *backend) Close() error {
    if err := b.ch.Close(); err != nil {
        return errors.Wrap(err, "closing channel")
    }

    return b.conn.Close()
}
```

---

## Step 3: Wire It Into Your Tool

Use `TelemetryConfig.Backend` to supply a factory function. The factory receives `*props.Props` so you can read configuration values, and returns `any` (to avoid import cycles). The returned value must implement `telemetry.Backend` — a failed type assertion falls back to noop with a warning.

```go
import "myorg/pkg/telemetry/rabbitmq"

p := &props.Props{
    Tool: props.Tool{
        Name: "mytool",
        Features: props.SetFeatures(
            props.Enable(props.TelemetryCmd),
        ),
        Telemetry: props.TelemetryConfig{
            Backend: func(p *props.Props) any {
                b, err := rabbitmq.NewBackend(
                    p.Config.GetString("rabbitmq.url"),
                    "telemetry-events",
                    p.Logger,
                )
                if err != nil {
                    p.Logger.Warn("failed to create RabbitMQ backend", "error", err)
                    return nil // falls back to noop
                }
                return b
            },
            Metadata: map[string]string{
                "environment": "production",
            },
        },
    },
}
```

!!! tip "Factory error handling"
    If your factory returns `nil` or a value that doesn't implement `telemetry.Backend`, the framework falls back to the noop backend. Log a warning so misconfiguration is visible in debug output.

---

## Step 4: Write Tests

Use `httptest.Server` or an in-memory mock for your transport. The key scenarios to test:

```go
func TestBackend_Send(t *testing.T) {
    t.Parallel()

    // Set up your mock transport
    b, _ := NewBackend("amqp://localhost:5672", "test-queue", logger.NewNoop())

    events := []telemetry.Event{
        {
            Type:     telemetry.EventCommandInvocation,
            Name:     "generate",
            ToolName: "mytool",
            Version:  "1.0.0",
        },
    }

    err := b.Send(context.Background(), events)
    if err != nil {
        t.Fatalf("send: %v", err)
    }

    // Assert events were published to your mock
}

func TestBackend_NetworkError(t *testing.T) {
    t.Parallel()

    // Backend with unreachable server
    b, _ := NewBackend("amqp://localhost:1", "test-queue", logger.NewNoop())

    // Should return nil — telemetry never blocks
    err := b.Send(context.Background(), []telemetry.Event{{Name: "test"}})
    if err != nil {
        t.Errorf("expected nil for network error, got %v", err)
    }
}

func TestBackend_Close(t *testing.T) {
    t.Parallel()

    b, _ := NewBackend("amqp://localhost:5672", "test-queue", logger.NewNoop())

    if err := b.Close(); err != nil {
        t.Fatalf("close: %v", err)
    }
}
```

---

## Backend Selection Precedence

When a custom `Backend` factory is set, it takes the highest precedence in backend selection:

1. **Custom backend** (`TelemetryConfig.Backend`) — your factory
2. Local-only (file backend)
3. OTLP (`TelemetryConfig.OTelEndpoint`)
4. HTTP (`TelemetryConfig.Endpoint`)
5. Noop (fallback)

---

## Related Documentation

- [Telemetry Component](../components/telemetry.md) — architecture, events, privacy controls
- [Telemetry Command](../components/commands/telemetry.md) — CLI management commands
- [Vendor Backends Specification](../development/specs/2026-03-30-telemetry-vendor-backends.md) — Datadog and PostHog reference implementations

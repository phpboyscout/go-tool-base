---
title: Create a Custom Deletion Requestor
description: How to implement the telemetry.DeletionRequestor interface for GDPR-compliant data deletion from custom backends.
date: 2026-03-31
tags: [how-to, telemetry, gdpr, deletion, privacy, custom]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# Create a Custom Deletion Requestor

When a user runs `telemetry reset`, the framework sends a data deletion request to the remote backend. GTB ships three built-in requestors (HTTP, email, event-based), but your backend may require a different mechanism — a GraphQL mutation, a queue message, or a vendor-specific API call.

This guide walks through:

1. Implementing `telemetry.DeletionRequestor`
2. Wiring the factory into `TelemetryConfig`
3. Testing the requestor
4. User-facing behaviour

---

## Step 1: Implement `telemetry.DeletionRequestor`

The interface has a single method:

```go
type DeletionRequestor interface {
    RequestDeletion(ctx context.Context, machineID string) error
}
```

**Key requirements:**

- `machineID` is the SHA-256-derived anonymised identifier (16 hex chars). This is the only identifier available for deletion — it's what appears in every telemetry event.
- Return an error if the deletion request fails — the framework will inform the user and suggest contacting the help channel.
- Deletion is best-effort. Not all backends can guarantee deletion (e.g. append-only logs). The requestor should make a reasonable attempt.

Create a requestor, e.g. for a GraphQL API:

```go
package myanalytics

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

const requestTimeout = 10 * time.Second

type graphQLRequestor struct {
    endpoint string
    apiKey   string
    client   *http.Client
    log      logger.Logger
}

// NewDeletionRequestor creates a requestor that sends a GraphQL mutation
// to delete telemetry data for the given machine ID.
func NewDeletionRequestor(endpoint, apiKey string, log logger.Logger) telemetry.DeletionRequestor {
    return &graphQLRequestor{
        endpoint: endpoint,
        apiKey:   apiKey,
        client:   gtbhttp.NewClient(gtbhttp.WithTimeout(requestTimeout)),
        log:      log,
    }
}
```

---

## Step 2: Implement RequestDeletion

```go
type graphQLRequest struct {
    Query     string         `json:"query"`
    Variables map[string]any `json:"variables"`
}

func (r *graphQLRequestor) RequestDeletion(ctx context.Context, machineID string) error {
    payload := graphQLRequest{
        Query: `mutation DeleteTelemetry($machineId: String!) {
            deleteTelemetryData(machineId: $machineId) {
                success
                deletedCount
            }
        }`,
        Variables: map[string]any{
            "machineId": machineID,
        },
    }

    body, err := json.Marshal(payload)
    if err != nil {
        return errors.Wrap(err, "marshalling deletion request")
    }

    req, err := http.NewRequestWithContext(ctx, http.MethodPost, r.endpoint, bytes.NewReader(body))
    if err != nil {
        return errors.Wrap(err, "creating deletion request")
    }

    req.Header.Set("Content-Type", "application/json")
    req.Header.Set("Authorization", "Bearer "+r.apiKey)

    resp, err := r.client.Do(req)
    if err != nil {
        return errors.Wrap(err, "sending deletion request")
    }

    defer func() { _ = resp.Body.Close() }()

    if resp.StatusCode >= 400 {
        r.log.Debug("deletion endpoint returned non-success status",
            "status", resp.StatusCode, "endpoint", r.endpoint)

        return errors.Newf("deletion request returned status %d", resp.StatusCode)
    }

    return nil
}
```

---

## Step 3: Wire It Into Your Tool

Use `TelemetryConfig.DeletionRequestor` to supply a factory function. Like the backend factory, it returns `any` to avoid import cycles. The returned value must implement `telemetry.DeletionRequestor`.

```go
import "myorg/pkg/myanalytics"

p := &props.Props{
    Tool: props.Tool{
        Name: "mytool",
        Features: props.SetFeatures(
            props.Enable(props.TelemetryCmd),
        ),
        Telemetry: props.TelemetryConfig{
            Endpoint: "https://analytics.example.com/events",
            DeletionRequestor: func(p *props.Props) any {
                return myanalytics.NewDeletionRequestor(
                    "https://analytics.example.com/graphql",
                    p.Config.GetString("analytics.api_key"),
                    p.Logger,
                )
            },
        },
    },
}
```

!!! tip "Fallback behaviour"
    If no `DeletionRequestor` is configured, or if the factory returns a value that doesn't implement the interface, the framework falls back to sending a `data.deletion_request` event through the existing telemetry backend. This works with any backend type but relies on server-side processing of the event.

---

## Step 4: Write Tests

```go
func TestGraphQLRequestor_Success(t *testing.T) {
    t.Parallel()

    var receivedBody graphQLRequest

    srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        body, _ := io.ReadAll(r.Body)
        _ = json.Unmarshal(body, &receivedBody)

        if r.Header.Get("Authorization") == "" {
            t.Error("missing Authorization header")
        }

        w.WriteHeader(http.StatusOK)
        _, _ = w.Write([]byte(`{"data":{"deleteTelemetryData":{"success":true}}}`))
    }))
    defer srv.Close()

    r := NewDeletionRequestor(srv.URL, "test-key", logger.NewNoop())

    err := r.RequestDeletion(context.Background(), "abc123def456")
    if err != nil {
        t.Fatalf("deletion error: %v", err)
    }

    if receivedBody.Variables["machineId"] != "abc123def456" {
        t.Errorf("machineId = %v, want abc123def456", receivedBody.Variables["machineId"])
    }
}

func TestGraphQLRequestor_ServerError(t *testing.T) {
    t.Parallel()

    srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
        w.WriteHeader(http.StatusInternalServerError)
    }))
    defer srv.Close()

    r := NewDeletionRequestor(srv.URL, "test-key", logger.NewNoop())

    err := r.RequestDeletion(context.Background(), "abc123")
    if err == nil {
        t.Error("expected error for 500 response")
    }
}
```

---

## Built-in Requestors

For reference, the framework provides three built-in requestors that cover common patterns:

| Requestor | Use case | Constructor |
|-----------|----------|-------------|
| HTTP | REST API with `POST {"machine_id": "..."}` | `telemetry.NewHTTPDeletionRequestor(endpoint, logger)` |
| Email | Opens a pre-filled `mailto:` link for the user | `telemetry.NewEmailDeletionRequestor(address, toolName)` |
| Event | Sends a `data.deletion_request` event through the backend | `telemetry.NewEventDeletionRequestor(backend)` |

You can use these directly instead of writing a custom requestor:

```go
Telemetry: props.TelemetryConfig{
    DeletionRequestor: func(p *props.Props) any {
        return telemetry.NewHTTPDeletionRequestor(
            "https://analytics.example.com/delete",
            p.Logger,
        )
    },
},
```

---

## User-Facing Behaviour

When the user runs `telemetry reset`:

1. All local data (buffer, spill files, local log) is cleared immediately
2. Your `RequestDeletion` is called with the machine ID
3. If it succeeds: `"Deletion request sent for machine ID: 4a3f8c1d9e2b6f70"`
4. If it fails: the error is shown and the user is directed to the help channel (if configured)
5. Telemetry is disabled regardless of deletion outcome

```bash
$ mytool telemetry reset
Deletion request sent for machine ID: 4a3f8c1d9e2b6f70
Local telemetry data cleared. Telemetry disabled.
```

---

## Related Documentation

- [Telemetry Component](../components/telemetry.md) — architecture, GDPR deletion, privacy controls
- [Create a Custom Telemetry Backend](custom-telemetry-backend.md) — the `Backend` interface
- [Telemetry Command](../components/commands/telemetry.md) — the `reset` command

---
title: Trace a request end to end
description: Turn on OpenTelemetry for the serve command, instrument the HTTP and gRPC transports and the store, and follow one REST request through the gateway, gRPC and the store as a single trace in Jaeger.
date: 2026-11-01
tags: [tutorial, observability, opentelemetry, tracing, jaeger]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# Trace a request end to end

By the end of this, one REST request to `mytool serve` shows up in Jaeger as a
single trace: the HTTP request, the gateway's gRPC call, the gRPC handler, and
the store, each with its timing, and the store's span tagged with the macguffin
it created. Every request log line carries the same trace id, so you can get
from a log line to its trace.

This is the last part of the web-service series. It builds on
[Put a gateway in front of gRPC](put-a-gateway-in-front.md).

Allow about thirty minutes.

## Before you start

- **The gateway** from [Put a gateway in front of gRPC](put-a-gateway-in-front.md).
  [Serve live API docs](serve-api-docs.md) is optional here.
- **The same gtb release.** Everything below is verified against **v0.50.0**.
- **Docker,** to run Jaeger locally. Any OpenTelemetry collector that accepts
  OTLP over HTTP works instead.

## Start Jaeger

[Jaeger](https://www.jaegertracing.io/) receives traces over OTLP and gives you
a UI to look at them. Run it, bound to your own machine only:

```bash
docker run -d --rm --name jaeger \
  -p 127.0.0.1:4318:4318 -p 127.0.0.1:16686:16686 \
  jaegertracing/jaeger:2.11.0
```

Port 4318 receives OTLP over HTTP, and 16686 is the UI. `--rm` removes the
container when you stop it, and the traces go with it.

## Turn telemetry on

`telemetry.SetupFromProps` builds whichever signals your config enables and
installs them as the process's OpenTelemetry providers. It also registers their
shutdown on the controller, so buffered spans are flushed before the process
exits. In `pkg/cmd/serve/main.go`, import it:

```go
	"gitlab.com/phpboyscout/go-tool-base/pkg/telemetry"
```

and call it straight after the controller is created:

```go
	if _, err := telemetry.SetupFromProps(ctx, props, controller); err != nil {
		return err
	}
```

With no telemetry config, every signal is off and this does nothing.

## Instrument the transports

Two additions feed the pipeline. Both come from the
[`go/transit`](https://transit.go.phpboyscout.uk) module, and both wrap the
standard OpenTelemetry libraries (`otelgrpc` and `otelhttp`), so the spans
follow the conventions every tracing backend reads. Add the imports:

```go
	transitgrpc "gitlab.com/phpboyscout/go/transit/grpc"
	transithttp "gitlab.com/phpboyscout/go/transit/http"
	transporthttp "gitlab.com/phpboyscout/go/transport/http"
```

The gRPC server takes a stats handler as an extra option:

```go
	grpcSrv, err := gtbgrpc.RegisterFromReader(ctx, "grpc", controller, cfg, props.Logger,
		transitgrpc.OTelStatsHandler())
```

The HTTP server takes a middleware chain. A chain is an ordered stack of
wrappers around your handler. The OTel middleware goes first, so it opens the
span, and the request logger goes inside it, so each log line can pick the span
up:

```go
	chain := transithttp.NewChain(
		transithttp.OTelMiddleware("mytool"),
		transithttp.LoggingMiddleware(logger.ToSlog(props.Logger)),
	)

	if _, err := gtbhttp.RegisterFromReader(ctx, "http", controller, cfg, props.Logger, mux,
		transporthttp.WithMiddleware(chain)); err != nil {
		return err
	}
```

Leave the gateway as it is. `gateway.NewFromConfig` already instruments its
connection to the gRPC server, so the trace context crosses from HTTP into gRPC
with nothing from you. Don't add `gateway.WithDialOptions(transitgrpc.OTelClientHandler())`
as well. A second handler records a span that never reaches Jaeger, and the
gateway's call then hangs off that missing span instead of the HTTP request.

## Trace inside your own code

The transport spans stop where your code starts. To see time spent in the store,
open a span there. That's plain OpenTelemetry, no gtb API. The span needs the
request's context to find its parent, so the store's methods take one.

In `internal/macguffin/store.go`, add the imports and a tracer:

```go
import (
	"context"
	"fmt"
	"sort"
	"sync"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
)

var tracer = otel.Tracer("mytool/macguffin")
```

Give `Create` a context, open a span around the work, and tag it:

```go
func (s *Store) Create(ctx context.Context, name string, quantity int32) Macguffin {
	_, span := tracer.Start(ctx, "Store.Create")
	defer span.End()

	span.SetAttributes(
		attribute.String("macguffin.name", name),
		attribute.Int("macguffin.quantity", int(quantity)),
	)

	s.mu.Lock()
	defer s.mu.Unlock()

	s.seq++
	m := Macguffin{ID: fmt.Sprintf("m-%d", s.seq), Name: name, Quantity: quantity}
	s.items[m.ID] = m

	span.SetAttributes(attribute.String("macguffin.id", m.ID))

	return m
}
```

`NewStore` seeds the first macguffin through `Create`, so it needs a context
too. Take the caller's rather than making a fresh one, which the scaffold's
`contextcheck` linter would reject:

```go
func NewStore(ctx context.Context) *Store {
	s := &Store{items: map[string]Macguffin{}}
	s.Create(ctx, "maltese-falcon", 1)

	return s
}
```

Then pass the context through from its two callers. In
`internal/grpcsvc/server.go`, name the handler's context and hand it on:

```go
func (s *Server) CreateMacguffin(ctx context.Context, req *macguffinv1.CreateMacguffinRequest) (*macguffinv1.Macguffin, error) {
	if req.GetName() == "" {
		return nil, status.Error(codes.InvalidArgument, "name is required")
	}

	return toProto(s.store.Create(ctx, req.GetName(), req.GetQuantity())), nil
}
```

And in `pkg/cmd/serve/main.go`:

```go
	store := macguffin.NewStore(ctx)
```

## Point it at Jaeger

Add a `telemetry` block to your own config, `~/.mytool/config.yaml`:

```yaml
telemetry:
  endpoint: "http://localhost:4318"
  insecure: true
  tracing:
    enabled: true
    sampling: 1.0
```

- **`endpoint`** is the collector's OTLP address.
- **`insecure`** sends plaintext. That's fine for a collector on your own
  machine and nowhere else.
- **`sampling: 1.0`** records every trace. Unset, it records one in ten, which
  is right under real load but means you'll wonder where your traces went while
  you're developing.

Leave `metrics` and `logs` off with Jaeger. It accepts traces only. With metrics
switched on, the exporter's flush at shutdown fails with
`failed to send metrics to http://localhost:4318/v1/metrics: 404 Not Found`. Turn
them on with a collector that takes them, such as the
[OpenTelemetry Collector](https://opentelemetry.io/docs/collector/).

## Send a request

Tidy, build and run:

```bash
go mod tidy
just build
./bin/mytool serve
```

In a second terminal, list the macguffins and create one through the gateway:

```bash
CA="$(mkcert -CAROOT)/rootCA.pem"
curl --cacert "$CA" https://localhost:8443/v1/macguffins
curl --cacert "$CA" -d '{"name":"the-grail","quantity":1}' https://localhost:8443/v1/macguffins
```

The server's log now has a line per request, carrying the trace and span ids:

```
INFO  request completed method=POST path=/v1/macguffins status=200 bytes=44 latency=352.811µs client_ip=::1 user_agent=curl/8.20.0 trace_id=f795e61eec9850901a79fcd53a8fb2b8 span_id=9ab43eae3d74e5e0
```

The create answers 200 rather than 201. The gateway maps a successful gRPC call
to 200.

## Follow it in Jaeger

Open `http://localhost:16686`, choose the `mytool` service, and find the POST.
Or paste its `trace_id` from the log line into the search box. It's one trace,
four spans deep:

```
POST /v1/                                      HTTP server
└─ macguffin.v1.MacguffinService/CreateMacguffin   gRPC client (the gateway)
   └─ macguffin.v1.MacguffinService/CreateMacguffin   gRPC server
      └─ Store.Create        macguffin.id=m-2 macguffin.name=the-grail macguffin.quantity=1
```

The REST request became a gRPC call through the gateway, ran the handler, and
went into the store, and the trace followed it the whole way. Each span has its
own duration, so when a request is slow, the trace shows which layer the time
went to. The store's attributes say which macguffin it was.

Stop the server with Ctrl-C. It shuts down without errors, and spans recorded
just before the signal are flushed rather than lost.

When you're done, stop Jaeger:

```bash
docker stop jaeger
```

## Two kinds of telemetry

gtb also has product analytics, in the same `telemetry` package. That's a
different thing with a different rule. Analytics is about your users and is off
until they agree to it. Observability is about your service, sent by you to a
collector you run, and it's on when you configure it. The two never share a
consent gate. [Observability](../explanation/components/observability.md)
explains the split.

## Where this leaves you

The macguffin service starts cleanly, reports its health, serves gRPC, REST, a
web page and live API docs over TLS, shuts down without dropping work, and now
shows you what each request did and where its time went. It's all one tool,
from one proto, on one controller.

## What to read next

- [Observability](../explanation/components/observability.md): the three
  signals, their config keys, and per-signal endpoints.
- [observability.go.phpboyscout.uk](https://observability.go.phpboyscout.uk):
  the module underneath, including how to correlate logs with traces.

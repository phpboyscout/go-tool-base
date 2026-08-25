---
title: Expose a gRPC Service as REST
description: Bridge an existing gRPC service to JSON/REST with grpc-gateway, mounted on your HTTP server or run as its own.
date: 2026-05-31
tags: [how-to, gateway, grpc, rest, http]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# Expose a gRPC Service as REST

You already have a gRPC service. Now you want a JSON/REST surface over it (for browsers, `curl`, or callers that don't speak gRPC) without hand-writing a translation layer.

GTB's `pkg/gateway` package makes a [grpc-gateway](https://github.com/grpc-ecosystem/grpc-gateway) a first-class transport. It dials the local gRPC server itself (matching the server's own transport security) and serves the generated REST handlers, either mounted on an existing HTTP server or as its own controller-managed server. The only gateway-specific code you write is a single registration function.

---

## Prerequisites

You need an existing gRPC service registered with the controller. If you're starting from scratch, see **[Add a gRPC Management Service](add-grpc-service.md)** first: this guide picks up from a working gRPC server.

You'll also need the grpc-gateway code generator on your path. Install it as a `buf` plugin (next step) or as a binary (`protoc-gen-grpc-gateway`).

---

## Step 1: Annotate the `.proto`

grpc-gateway maps HTTP routes onto your RPCs using `google.api.http` annotations. Import the annotations and add an `option (google.api.http)` to each RPC you want exposed:

```protobuf
syntax = "proto3";

package widget.v1;

import "google/api/annotations.proto";

option go_package = "gitlab.com/phpboyscout/widgetsvc/internal/gen/widget/v1;widgetv1";

service WidgetService {
  rpc GetWidget(GetWidgetRequest) returns (Widget) {
    option (google.api.http) = {get: "/v1/widgets/{id}"};
  }
  rpc CreateWidget(CreateWidgetRequest) returns (Widget) {
    option (google.api.http) = {
      post: "/v1/widgets"
      body: "*"
    };
  }
}
```

The `{id}` in the GET path binds to the `id` field of `GetWidgetRequest`. The `body: "*"` on the POST tells the gateway to decode the whole JSON request body into `CreateWidgetRequest`.

---

## Step 2: Regenerate with the grpc-gateway plugin

Add the `protoc-gen-grpc-gateway` plugin to your `buf.gen.yaml` alongside the Go and gRPC plugins:

```yaml
version: v2
plugins:
  - local: protoc-gen-go
    out: internal/gen
    opt: paths=source_relative
  - local: protoc-gen-go-grpc
    out: internal/gen
    opt: paths=source_relative
  - local: protoc-gen-grpc-gateway
    out: internal/gen
    opt: paths=source_relative
```

Regenerate:

```bash
buf generate
```

This emits a `*.pb.gw.go` file next to your generated Go code, containing `RegisterWidgetServiceHandler`: the function the gateway uses to wire the REST handlers onto its mux.

---

## Step 3: Write the RegisterFunc

The caller supplies a single `RegisterFunc` that wires the generated handlers onto the gateway mux using a connection to the gRPC server. It's typically a one-liner calling the generated `RegisterXServiceHandler`:

```go
register := func(ctx context.Context, mux *runtime.ServeMux, conn *grpc.ClientConn) error {
    return widgetv1.RegisterWidgetServiceHandler(ctx, mux, conn)
}
```

That's the only gateway-specific code you write.

---

## Step 4: Build the gateway and mount it

Use `gateway.New` to build a handler, then mount it on your existing HTTP server's mux:

```go
gwHandler, err := gateway.New(ctx, p.Config,
    func(ctx context.Context, mux *runtime.ServeMux, conn *grpc.ClientConn) error {
        return widgetv1.RegisterWidgetServiceHandler(ctx, mux, conn)
    })
if err != nil {
    return err
}

mux := stdhttp.NewServeMux()
mux.Handle("/v1/", gwHandler)

if _, err := gtbhttp.RegisterFromReader(ctx, "http", controller, p.Config.View(), p.Logger, mux); err != nil {
    return err
}
```

**Mount it at the prefix the annotations use, and do not strip that prefix.** The gateway matches the *full* annotated path (`/v1/widgets`, `/v1/widgets/{id}`), so it must receive the request with `/v1/` still attached. Mount with:

```go
mux.Handle("/v1/", gwHandler)
```

Do **not** wrap it in `http.StripPrefix("/v1", gwHandler)`. Stripping the prefix hands the gateway `/widgets`, which matches none of its annotated routes, and every request comes back `404`. This is the single most common way to break gateway routing.

---

## Step 5: Transport security comes for free

You don't dial the gRPC server yourself. `gateway.New` opens the connection internally via `grpc.DialLocal`, which reads the same config the gRPC server started from. The gateway's transport security therefore *matches the server's* automatically:

- gRPC server running plaintext? The gateway dials plaintext.
- gRPC server running TLS (even a single shared self-signed cert across all transports)? The gateway dials TLS, trusting that same certificate: no extra client setup, no cert pool to assemble by hand.

This is why a gateway "just works" over the shared `server.tls` certificate that the rest of your stack already uses.

---

## Test it

Start the service and hit the REST surface with `curl`:

```bash
curl http://localhost:8080/v1/widgets
# {"widgets":[{"id":"1","name":"sprocket","quantity":3}]}

curl http://localhost:8080/v1/widgets/1
# {"id":"1","name":"sprocket","quantity":3}

curl -X POST http://localhost:8080/v1/widgets \
  -H 'Content-Type: application/json' \
  -d '{"name":"flange","quantity":7}'
# {"id":"2","name":"flange","quantity":7}
```

If you get a `404` on a path you know is annotated, check Step 4: a stray `http.StripPrefix` is almost always the cause.

---

## Alternative: run the gateway as its own server

Mounting on an existing mux (above) is the right call when the REST surface should share an origin with other routes. The OpenAPI docs, say. But the gateway can also stand up as its own controller-managed HTTP server, a peer of the gRPC and HTTP servers. Use `gateway.Register`:

```go
srv, err := gateway.RegisterFromConfig(ctx, "gateway", controller, p.Config.View(), p.Logger,
    func(ctx context.Context, mux *runtime.ServeMux, conn *grpc.ClientConn) error {
        return widgetv1.RegisterWidgetServiceHandler(ctx, mux, conn)
    })
if err != nil {
    return err
}
```

This reads its own `server.gateway` config block for port and TLS, with TLS falling back to the shared `server.tls` defaults:

```yaml
server:
  gateway:
    port: 8081
  tls:
    enabled: true
    cert: /etc/certs/server.crt
    key: /etc/certs/server.key
```

Internally `Register` builds the handler with `New`, then hosts it through `pkg/http`, so the prefix-matching behaviour from Step 4 is identical; you just don't write the `mux.Handle` line yourself.

---

## Related Documentation

- **[Gateway component](../explanation/components/gateway.md)**: `New`, `Register`, `RegisterFunc`, the options, and the config block
- **[gRPC component](../explanation/components/grpc.md)**: `NewServer`, `Register`, and `DialLocal` (the in-process dial the gateway uses)
- **[Add a gRPC Management Service](add-grpc-service.md)**: register the gRPC server this gateway sits in front of
- **[Serve API Docs](serve-api-docs.md)**: serve an OpenAPI spec and docs site on the same mux as the gateway

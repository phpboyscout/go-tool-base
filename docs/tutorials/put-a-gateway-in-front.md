---
title: Put a gateway in front of gRPC
description: Annotate the proto with HTTP routes, generate a grpc-gateway, and serve the REST API from the gRPC service itself, so the hand-written REST layer and its second data mapping go away.
date: 2026-11-01
tags: [tutorial, gateway, grpc-gateway, grpc, rest, protobuf]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# Put a gateway in front of gRPC

By the end of this, the REST API is generated from the proto and answered by
the gRPC service itself, through a grpc-gateway. The REST adapter you wrote in
[Add a REST API](add-a-rest-api.md) is deleted, and with it the second mapping
of the same data. A macguffin becomes JSON in one place.

This is the fourth part of the web-service series.

Allow about thirty minutes.

## Before you start

- **The `serve` command** from [Add a REST API](add-a-rest-api.md) and
  [Serve a web page](serve-a-web-page.md): gRPC, the REST API and the page, on one
  controller.
- **The buf setup** from [Serve gRPC over TLS](serve-grpc-over-tls.md).
- **The same gtb release.** Everything below is verified against **v0.50.0**.

## What the gateway is

A grpc-gateway is generated code that turns an HTTP request into a gRPC call and
the reply back into JSON. You add a route to each RPC in the proto, a buf plugin
generates the translation, and the gateway calls your existing gRPC service. You
write no handlers, and the REST API can't drift from the gRPC one, because both
come from the same file.

## Add HTTP routes to the proto

In `proto/macguffin/v1/macguffin.proto`, import Google's HTTP annotations under
the `package` line, and give each RPC a route:

```protobuf
syntax = "proto3";

package macguffin.v1;

import "google/api/annotations.proto";

option go_package = "github.com/myorg/mytool/internal/gen/macguffin/v1;macguffinv1";

service MacguffinService {
  rpc GetMacguffin(GetMacguffinRequest) returns (Macguffin) {
    option (google.api.http) = {get: "/v1/macguffins/{id}"};
  }
  rpc ListMacguffins(ListMacguffinsRequest) returns (ListMacguffinsResponse) {
    option (google.api.http) = {get: "/v1/macguffins"};
  }
  rpc CreateMacguffin(CreateMacguffinRequest) returns (Macguffin) {
    option (google.api.http) = {
      post: "/v1/macguffins"
      body: "*"
    };
  }
}
```

The messages stay as they were. `{id}` binds to the request's `id` field.
`body: "*"` maps the whole JSON body onto `CreateMacguffinRequest`. The routes
live under `/v1/`, so they don't collide with the page at `/`.

These rules can do much more: query parameters, several routes for one RPC,
choosing which field is the response body. The
[grpc-gateway mapping docs](https://grpc-ecosystem.github.io/grpc-gateway/docs/mapping/)
cover them.

## Fetch the annotations

`google/api/annotations.proto` isn't in your project. It comes from Google's
common protos, which buf fetches from the Buf Schema Registry. Add the dependency
to `buf.yaml`:

```yaml
version: v2
modules:
  - path: proto
deps:
  - buf.build/googleapis/googleapis
```

Then fetch it:

```bash
buf dep update
```

That writes a `buf.lock` pinning the exact version. Commit it, so every build
uses the same protos.

## Generate the gateway

The gateway generator is another buf plugin. Install it:

```bash
go install github.com/grpc-ecosystem/grpc-gateway/v2/protoc-gen-grpc-gateway@latest
```

Add it to the end of `buf.gen.yaml`:

```yaml
  - local: protoc-gen-grpc-gateway
    out: internal/gen
    opt: paths=source_relative
```

Regenerate:

```bash
go generate ./...
ls internal/gen/macguffin/v1/
```

```
macguffin_grpc.pb.go
macguffin.pb.go
macguffin.pb.gw.go
```

`macguffin.pb.gw.go` is the gateway. Its `RegisterMacguffinServiceHandler`
mounts the REST routes on a gateway mux, given a connection to the gRPC server.

## Delete the REST layer

The gateway serves what your REST adapter served, so the adapter goes:

```bash
rm -rf internal/resthand internal/restapi api
```

If you used the spec version, take the oapi-codegen line out of `gen.go` too, so
it's back to buf alone:

```go
package mytool

//go:generate buf generate
```

## Wire the gateway in

Replace `pkg/cmd/serve/main.go`:

```go
package serve

import (
	"context"
	"net/http"

	"gitlab.com/phpboyscout/go-tool-base/pkg/gateway"
	gtbgrpc "gitlab.com/phpboyscout/go-tool-base/pkg/grpc"
	gtbhttp "gitlab.com/phpboyscout/go-tool-base/pkg/http"
	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go/controls"
	"gitlab.com/phpboyscout/go/errors"

	macguffinv1 "github.com/myorg/mytool/internal/gen/macguffin/v1"
	"github.com/myorg/mytool/internal/grpcsvc"
	"github.com/myorg/mytool/internal/macguffin"
	"github.com/myorg/mytool/internal/site"
)

func RunServe(ctx context.Context, props *props.Props, _ *ServeOptions, _ []string) error {
	controller := controls.NewController(ctx,
		controls.WithLogger(logger.ToSlog(props.Logger)),
		controls.WithStopOnGiveUp(),
	)

	store := macguffin.NewStore()
	cfg := props.Config.View()

	grpcSrv, err := gtbgrpc.RegisterFromReader(ctx, "grpc", controller, cfg, props.Logger)
	if err != nil {
		return err
	}

	macguffinv1.RegisterMacguffinServiceServer(grpcSrv, grpcsvc.New(store))

	gw, err := gateway.NewFromConfig(ctx, cfg, macguffinv1.RegisterMacguffinServiceHandler)
	if err != nil {
		return err
	}

	mux := http.NewServeMux()
	mux.Handle("/v1/", gw)
	mux.Handle("/", site.New(store).Routes())

	if _, err := gtbhttp.RegisterFromReader(ctx, "http", controller, cfg, props.Logger, mux); err != nil {
		return err
	}

	controller.Start()
	<-controller.Done()

	outcome, _ := controller.Outcome()
	if outcome.Cause == controls.CauseGaveUp {
		return errors.Wrapf(outcome.Err, "service %s gave up", outcome.Service)
	}

	return nil
}
```

`gateway.NewFromConfig` opens the connection to your gRPC server for you, from
the same config the server started with. So it matches the server's transport
security. Your gRPC server is on TLS with the mkcert certificate, so the gateway
dials TLS and trusts that certificate, with nothing to set up.

The generated `RegisterMacguffinServiceHandler` already has the shape the
gateway wants, so it goes in as it is.

Mount the gateway at `/v1/` and leave the prefix on. The gateway matches the full
annotated path, `/v1/macguffins`. If you wrap it in `http.StripPrefix("/v1", gw)`,
it sees `/macguffins`, matches nothing, and answers every request with
`{"code":5,"message":"Not Found","details":[]}`.

Tidy and build:

```bash
go mod tidy
just build
./bin/mytool serve
```

## Call the generated API

In a second terminal:

```bash
CA="$(mkcert -CAROOT)/rootCA.pem"
curl --cacert "$CA" https://localhost:8443/v1/macguffins
curl --cacert "$CA" -d '{"name":"the-grail","quantity":1}' https://localhost:8443/v1/macguffins
curl --cacert "$CA" https://localhost:8443/v1/macguffins/m-1
```

```
{"macguffins":[{"id":"m-1", "name":"maltese-falcon", "quantity":1}]}
{"id":"m-2", "name":"the-grail", "quantity":1}
{"id":"m-1", "name":"maltese-falcon", "quantity":1}
```

The list comes back wrapped in `macguffins`, because that's the shape of
`ListMacguffinsResponse` in the proto. The spacing is protojson's: it varies
its whitespace on purpose, so compare the JSON as data, never as text.

The REST API is now the gRPC service, so the grail is there over gRPC, and on the
page at `/`:

```bash
grpcurl -cacert "$CA" localhost:50051 macguffin.v1.MacguffinService/ListMacguffins
```

Both `maltese-falcon` and `the-grail` are listed. The old `/macguffins` path from
the last part is gone and answers 404.

## Errors come across

The gateway turns a gRPC status into the matching HTTP status. The
`codes.NotFound` and `codes.InvalidArgument` your service already returns become
404 and 400, with a JSON body:

```bash
curl --cacert "$CA" -w '\n%{http_code}\n' https://localhost:8443/v1/macguffins/nope
curl --cacert "$CA" -w '\n%{http_code}\n' -d '{}' https://localhost:8443/v1/macguffins
```

```
{"code":5, "message":"macguffin \"nope\" not found", "details":[]}
404
{"code":3, "message":"name is required", "details":[]}
400
```

`code` is the gRPC code: 5 is `NotFound`, 3 is `InvalidArgument`. So the rule is
to return the right `codes.*` from the gRPC service, and the REST side gets the
right status.

If your clients expect a different error shape, give the gateway your own error
handler. `NewFromConfig` passes grpc-gateway mux options through:

```go
gw, err := gateway.NewFromConfig(ctx, cfg, macguffinv1.RegisterMacguffinServiceHandler,
	gateway.WithMuxOptions(runtime.WithErrorHandler(myErrorHandler)),
)
```

`runtime` is `github.com/grpc-ecosystem/grpc-gateway/v2/runtime`. The same option
takes the gateway's other settings, such as header matchers and marshalers.

## Where this leaves you

There's one place a macguffin becomes JSON, and it's generated. gRPC, REST and
the page run on one controller, behind one certificate, from one store, and the
REST routes live in the proto next to the RPCs they serve. The next part
generates API docs from the same proto.

## What to read next

- [Expose a gRPC service as REST](../how-to/expose-grpc-as-rest.md): the gateway
  as its own server, and the options.
- [Gateway component](../explanation/components/gateway.md): how the gateway
  dials the gRPC server, and its config block.

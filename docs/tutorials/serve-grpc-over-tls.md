---
title: Serve gRPC over TLS
description: Define a gRPC service in protobuf, generate its Go code with buf, run it on the serve command's controller with health and reflection, then switch it to TLS with a config change.
date: 2026-11-01
tags: [tutorial, grpc, protobuf, buf, tls, service]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# Serve gRPC over TLS

By the end of this, `mytool serve` answers a gRPC API over TLS, with a health
service an orchestrator understands and reflection for poking at it. The only
code you write is the service itself. The server, its health checks, its
shutdown and its TLS come from the framework.

This is the second part of the web-service series. It builds on the `serve`
command from [Run a long-lived service](run-a-long-lived-service.md).

Allow about forty minutes. The `go install` steps download and build each tool
the first time, so they take a minute or two.

## Before you start

- **The `serve` command** from [Run a long-lived service](run-a-long-lived-service.md),
  in the `mytool` project.
- **The same gtb release.** Everything below is verified against **v0.50.0**.
- **[mkcert](https://github.com/FiloSottile/mkcert)**, for the TLS step.

## Install the protobuf tools

gRPC starts from a schema, a `.proto` file, and the Go code comes from that.
You don't write it. [buf](https://buf.build/docs) runs the generators, and it
needs two plugins: `protoc-gen-go` for the message types, and
`protoc-gen-go-grpc` for the client and server stubs.

```bash
go install github.com/bufbuild/buf/cmd/buf@latest
go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest
```

They land in `$(go env GOPATH)/bin`, so make sure that's on your `$PATH`, and
check they run:

```bash
buf --version
protoc-gen-go --version
protoc-gen-go-grpc --version
```

```
1.75.0
protoc-gen-go v1.36.12
protoc-gen-go-grpc 1.6.2
```

Newer versions are fine.

## Define the contract

The service looks after *macguffins*: named things with a quantity. Create
`proto/macguffin/v1/macguffin.proto`:

```protobuf
syntax = "proto3";

package macguffin.v1;

option go_package = "github.com/myorg/mytool/internal/gen/macguffin/v1;macguffinv1";

service MacguffinService {
  rpc GetMacguffin(GetMacguffinRequest) returns (Macguffin);
  rpc ListMacguffins(ListMacguffinsRequest) returns (ListMacguffinsResponse);
  rpc CreateMacguffin(CreateMacguffinRequest) returns (Macguffin);
}

message Macguffin {
  string id = 1;
  string name = 2;
  int32 quantity = 3;
}

message GetMacguffinRequest { string id = 1; }
message ListMacguffinsRequest { int32 page_size = 1; }
message ListMacguffinsResponse { repeated Macguffin macguffins = 1; }
message CreateMacguffinRequest { string name = 1; int32 quantity = 2; }
```

The `go_package` path starts with your module path, from the first line of
`go.mod`. If yours isn't `github.com/myorg/mytool`, change it here and in the
imports below.

## Generate the Go code

buf needs two small files at the project root. `buf.yaml` says where the
protos live:

```yaml
version: v2
modules:
  - path: proto
```

`buf.gen.yaml` says what to generate, and where:

```yaml
version: v2
plugins:
  - local: protoc-gen-go
    out: internal/gen
    opt: paths=source_relative
  - local: protoc-gen-go-grpc
    out: internal/gen
    opt: paths=source_relative
```

Don't leave `buf.yaml` out. Without it, buf treats the whole project as the
module, and the code lands in `internal/gen/proto/macguffin/v1` instead of
matching the `go_package` line.

Then hook buf into `go generate`, so the code can't drift from the proto. Create
`gen.go` at the project root:

```go
package mytool

//go:generate buf generate
```

`just build` already runs `go generate ./...` before it compiles, so from now on
every build regenerates the code from the proto. That means `buf` has to be on
your `$PATH` whenever you build. Run it once by hand to check:

```bash
go generate ./...
find internal/gen -type f
```

```
internal/gen/macguffin/v1/macguffin_grpc.pb.go
internal/gen/macguffin/v1/macguffin.pb.go
```

`macguffin.pb.go` holds the message types. `macguffin_grpc.pb.go` holds the
`MacguffinServiceServer` interface you implement next, with one method per RPC.

## Keep the data out of the transport

The gRPC server is how requests arrive, not where the data lives. Later parts
put an HTTP API on the same data, so keep it in a type of its own that knows
nothing about gRPC. Create `internal/macguffin/store.go`, an in-memory store
standing in for a real database:

```go
package macguffin

import (
	"fmt"
	"sort"
	"sync"
)

type Macguffin struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Quantity int32  `json:"quantity"`
}

type Store struct {
	mu    sync.Mutex
	items map[string]Macguffin
	seq   int
}

func NewStore() *Store {
	s := &Store{items: map[string]Macguffin{}}
	s.Create("maltese-falcon", 1)

	return s
}

func (s *Store) Get(id string) (Macguffin, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	m, ok := s.items[id]

	return m, ok
}

func (s *Store) List() []Macguffin {
	s.mu.Lock()
	defer s.mu.Unlock()

	out := make([]Macguffin, 0, len(s.items))
	for _, m := range s.items {
		out = append(out, m)
	}

	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })

	return out
}

func (s *Store) Create(name string, quantity int32) Macguffin {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.seq++
	m := Macguffin{ID: fmt.Sprintf("m-%d", s.seq), Name: name, Quantity: quantity}
	s.items[m.ID] = m

	return m
}
```

It starts with one macguffin, `m-1`.

## Implement the service

Now the gRPC side, kept thin: each method calls the store and converts the
result to the generated type. Create `internal/grpcsvc/server.go`:

```go
package grpcsvc

import (
	"context"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	macguffinv1 "github.com/myorg/mytool/internal/gen/macguffin/v1"
	"github.com/myorg/mytool/internal/macguffin"
)

type Server struct {
	macguffinv1.UnimplementedMacguffinServiceServer

	store *macguffin.Store
}

func New(store *macguffin.Store) *Server {
	return &Server{store: store}
}

func (s *Server) GetMacguffin(_ context.Context, req *macguffinv1.GetMacguffinRequest) (*macguffinv1.Macguffin, error) {
	m, ok := s.store.Get(req.GetId())
	if !ok {
		return nil, status.Errorf(codes.NotFound, "macguffin %q not found", req.GetId())
	}

	return toProto(m), nil
}

func (s *Server) ListMacguffins(_ context.Context, _ *macguffinv1.ListMacguffinsRequest) (*macguffinv1.ListMacguffinsResponse, error) {
	items := s.store.List()

	resp := &macguffinv1.ListMacguffinsResponse{Macguffins: make([]*macguffinv1.Macguffin, 0, len(items))}
	for _, m := range items {
		resp.Macguffins = append(resp.Macguffins, toProto(m))
	}

	return resp, nil
}

func (s *Server) CreateMacguffin(_ context.Context, req *macguffinv1.CreateMacguffinRequest) (*macguffinv1.Macguffin, error) {
	if req.GetName() == "" {
		return nil, status.Error(codes.InvalidArgument, "name is required")
	}

	return toProto(s.store.Create(req.GetName(), req.GetQuantity())), nil
}

func toProto(m macguffin.Macguffin) *macguffinv1.Macguffin {
	return &macguffinv1.Macguffin{Id: m.ID, Name: m.Name, Quantity: m.Quantity}
}
```

Embedding `UnimplementedMacguffinServiceServer` is required by the generated
interface. It also means that if you add an RPC to the proto later, the server
still compiles, and the new call answers "unimplemented" until you write it.

Return gRPC status codes such as `codes.NotFound` rather than plain errors, so a
caller gets something it can branch on.

## Run it on the controller

Replace `pkg/cmd/serve/main.go`. The heartbeat from part 1 goes, and the gRPC
server takes its place on the same controller:

```go
package serve

import (
	"context"

	gtbgrpc "gitlab.com/phpboyscout/go-tool-base/pkg/grpc"
	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go/controls"
	"gitlab.com/phpboyscout/go/errors"

	macguffinv1 "github.com/myorg/mytool/internal/gen/macguffin/v1"
	"github.com/myorg/mytool/internal/grpcsvc"
	"github.com/myorg/mytool/internal/macguffin"
)

func RunServe(ctx context.Context, props *props.Props, _ *ServeOptions, _ []string) error {
	controller := controls.NewController(ctx,
		controls.WithLogger(logger.ToSlog(props.Logger)),
		controls.WithStopOnGiveUp(),
	)

	store := macguffin.NewStore()

	grpcSrv, err := gtbgrpc.RegisterFromReader(ctx, "grpc", controller, props.Config.View(), props.Logger)
	if err != nil {
		return err
	}

	macguffinv1.RegisterMacguffinServiceServer(grpcSrv, grpcsvc.New(store))

	controller.Start()
	<-controller.Done()

	outcome, _ := controller.Outcome()
	if outcome.Cause == controls.CauseGaveUp {
		return errors.Wrapf(outcome.Err, "service %s gave up", outcome.Service)
	}

	return nil
}
```

`RegisterFromReader` builds the gRPC server from the `server.grpc` config block.
It registers the standard gRPC health service, backed by the controller's health
reports, and registers the server's start, stop and status on the controller as
a service called `grpc`. Then it hands the server back, for you to register your
own service on.

The project's imports go in a group of their own, after the framework's. The
scaffold's linter enforces that grouping.

## Give it a port

With no config, the server listens on a random free port. Give `serve` shipped
defaults, the way `hello` got them in [Configure your CLI](configure-your-cli.md):

```bash
gtb generate command --name serve --short "Run the service until stopped" --assets
```

Then put this in `pkg/cmd/serve/assets/config.yaml`:

```yaml
server:
  grpc:
    port: 50051
    reflection: true
```

Reflection lets a client ask the server what it offers, so you don't need the
`.proto` to hand. Turn it off in production, because it publishes your whole API
surface.

Build and run it:

```bash
go mod tidy
just build
./bin/mytool serve
```

```
INFO  starting gRPC server tls=false addr=[::]:50051
```

## Call it with grpcurl

Leave the server running and use a second terminal.
[grpcurl](https://github.com/fullstorydev/grpcurl) is curl for gRPC:

```bash
go install github.com/fullstorydev/grpcurl/cmd/grpcurl@latest
grpcurl -plaintext localhost:50051 list
```

```
grpc.health.v1.Health
grpc.reflection.v1.ServerReflection
grpc.reflection.v1alpha.ServerReflection
macguffin.v1.MacguffinService
```

That's your service, plus health and reflection, which you didn't register.
Call it:

```bash
grpcurl -plaintext localhost:50051 macguffin.v1.MacguffinService/ListMacguffins
grpcurl -plaintext -d '{"name":"sankara-stone","quantity":5}' localhost:50051 macguffin.v1.MacguffinService/CreateMacguffin
grpcurl -plaintext -d '{"id":"m-9"}' localhost:50051 macguffin.v1.MacguffinService/GetMacguffin
```

```
{
  "macguffins": [
    {
      "id": "m-1",
      "name": "maltese-falcon",
      "quantity": 1
    }
  ]
}
{
  "id": "m-2",
  "name": "sankara-stone",
  "quantity": 5
}
ERROR:
  Code: NotFound
  Message: macguffin "m-9" not found
```

And the health service:

```bash
grpcurl -plaintext localhost:50051 grpc.health.v1.Health/Check
```

```
{
  "status": "SERVING"
}
```

That answer comes from the controller. A SIGTERM still drains and stops the
server, the same way it stopped the heartbeat:

```
INFO  Stopping Services component=controller cause=parent
INFO  Stopping gRPC server
INFO  Stopped component=controller
```

## Switch to TLS

Plaintext is fine on your laptop and nowhere else. Turning on TLS is a config
change.

You need a certificate a client trusts. mkcert creates a local certificate
authority and signs certificates from it. Run this once to create the authority
and add it to your system's trust store (it may ask for your password):

```bash
mkcert -install
```

Then, in the project directory, make a certificate for the names the server
answers on:

```bash
mkcert localhost 127.0.0.1 ::1
```

That writes `localhost+2.pem`, the certificate, and `localhost+2-key.pem`, its
key. Don't commit either of them.

Point the server at them in your own config file, `~/.mytool/config.yaml`.
`init` already wrote a `server:` key there, so add `tls:` under it rather than
starting a second `server:` block, which isn't valid YAML:

```yaml
server:
  grpc:
    reflection: true
  tls:
    enabled: true
    cert: ./localhost+2.pem
    key: ./localhost+2-key.pem
```

The paths are relative to where you run the tool, so run it from the project
directory. Restart it:

```bash
./bin/mytool serve
```

```
INFO  starting gRPC server tls=true addr=[::]:50051
```

Call it over TLS. `-cacert` names mkcert's authority explicitly, which works
whether or not your system trust store picked it up:

```bash
grpcurl -cacert "$(mkcert -CAROOT)/rootCA.pem" -d '{"id":"m-1"}' localhost:50051 macguffin.v1.MacguffinService/GetMacguffin
```

```
{
  "id": "m-1",
  "name": "maltese-falcon",
  "quantity": 1
}
```

A plaintext client is now turned away:

```bash
grpcurl -plaintext localhost:50051 list
```

```
Failed to dial target host "localhost:50051": read tcp 127.0.0.1:35754->127.0.0.1:50051: read: connection reset by peer
```

So is a client that doesn't trust the certificate's authority, with
`x509: certificate signed by unknown authority`.

The certificate sits under `server.tls`, not `server.grpc.tls`, on purpose. Every
transport falls back to the shared block, so the HTTP server in the next part
uses the same certificate. A transport gets its own only when you set one under
its own block.

The server also advertises HTTP/2 over ALPN (the `h2` protocol), and current
gRPC clients refuse a TLS connection that doesn't offer it. You can see it with
`openssl s_client -connect localhost:50051 -alpn h2`, which reports
`ALPN protocol: h2`.

## Where this leaves you

`mytool serve` is a real gRPC service: a typed contract, a thin implementation
over a store, health that an orchestrator understands, reflection for
development, a clean shutdown, and TLS. The part that was yours to write was the
service logic.

## What to read next

- [Add a gRPC service](../how-to/add-grpc-service.md): custom server options,
  interceptors, and wiring the lifecycle by hand.
- [gRPC component](../explanation/components/grpc.md): what the config adapter
  reads, and how TLS resolves.
- [TLS component](../explanation/components/tls.md): the hardened defaults and
  the per-transport overrides.

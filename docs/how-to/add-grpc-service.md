---
title: Add a gRPC Management Service
description: How to register a gRPC server with the controller, wire health checks, and configure the port.
date: 2026-03-25
tags: [how-to, grpc, services, controls, health, lifecycle]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# Add a gRPC Management Service

GTB's `pkg/grpc` package is the config adapter over the `go/transport/grpc` server: it reads the `server.grpc` block and hands you `RegisterFromReader`, `StartFromReader` and `DialLocalFromReader`, each curried for the `controls.Controller`. You register your gRPC server as a managed service, and the controller handles startup ordering, health reporting, and graceful shutdown.

---

## Prerequisites

You need an existing controller. If you're starting from scratch, see **[Managing Background Services](manage-background-services.md)** first.

---

## Step 1: Configure the Port

Add gRPC port configuration to your embedded defaults (`assets/config/defaults.yaml`):

```yaml
server:
  grpc:
    port: 50051
    reflection: false   # set true to enable gRPC reflection (useful in development)
```

The `pkg/grpc` package reads `server.grpc.port`, falling back to `server.port` if the grpc-specific key is absent.

---

## Step 2: Define Your Service

Implement your gRPC service as normal using the generated protobuf code:

```go
// myservice/server.go
package myservice

import (
    "context"
    pb "github.com/my-org/mytool/gen/proto/myservice/v1"
)

type Server struct {
    pb.UnimplementedMyServiceServer
    props *props.Props
}

func (s *Server) DoThing(ctx context.Context, req *pb.DoThingRequest) (*pb.DoThingResponse, error) {
    s.props.Logger.Info("DoThing called", "id", req.GetId())
    return &pb.DoThingResponse{Result: "ok"}, nil
}
```

---

## Step 3: Register with the Controller

Use `RegisterFromReader`: a single call that creates the server, wires health checks, and adds it to the controller:

```go
import (
    gtbgrpc "gitlab.com/phpboyscout/go-tool-base/pkg/grpc"
    "gitlab.com/phpboyscout/go/controls"
    pb "github.com/my-org/mytool/gen/proto/myservice/v1"
    "google.golang.org/grpc"
)

func registerGRPCService(ctx context.Context, controller controls.Controllable, p *props.Props) error {
    srv, err := gtbgrpc.RegisterFromReader(ctx, "grpc", controller, p.Config.View(), p.Logger)
    if err != nil {
        return err
    }

    // Register your service implementation on the gRPC server
    pb.RegisterMyServiceServer(srv, &myservice.Server{props: p})

    return nil
}
```

`RegisterFromReader` does four things:
1. Creates a `*grpc.Server` with optional server options
2. Calls `RegisterHealthService` to wire the standard gRPC health protocol
3. Registers `Start`, `Stop`, and `Status` functions with the controller under the given ID
4. Returns the `*grpc.Server` for you to register your own services on

---

## Step 4: Wire into Your Command

```go
func NewCmdServe(p *props.Props) *setup.Command {
    return setup.Wrap("serve", &cobra.Command{
        Use:  "serve",
        RunE: func(cmd *cobra.Command, args []string) error {
            ctx := cmd.Context()

            controller := controls.NewController(ctx,
                controls.WithLogger(logger.ToSlog(p.Logger)),
            )

            if err := registerGRPCService(ctx, controller, p); err != nil {
                return err
            }

            controller.Start()

            // Block until the controller shuts down. The controller installs
            // SIGINT/SIGTERM handlers itself and drives a graceful shutdown.
            controller.Wait()

            return nil
        },
    })
}
```

---

## Step 5: Enable Reflection for Development

gRPC reflection allows tools like `grpcurl` and `evans` to query your service schema without a `.proto` file. Enable it in your development config:

```yaml
server:
  grpc:
    reflection: true
```

Test with:

```bash
grpcurl -plaintext localhost:50051 list
# my.org.MyService
```

Disable reflection in production, it exposes your full API surface.

---

## Serving over TLS

Enable TLS by setting the shared `server.tls` keys (one certificate serves every transport), or override per transport under `server.grpc.tls`:

```yaml
server:
  tls:
    enabled: true
    cert: /etc/certs/server.crt
    key: /etc/certs/server.key
```

`RegisterFromReader` and `StartFromReader` pick this up automatically: including advertising HTTP/2 via ALPN, which modern gRPC clients require. Resolution, the typed `Pair`, and the client-side cert-pool helpers live in the **[TLS component](../explanation/components/tls.md)**. For an in-process client (such as the gateway) that needs to dial the server with matching transport security, use `gtbgrpc.DialLocalFromReader(p.Config.View())`.

---

## Manual Control (Without `RegisterFromReader`)

If you need more control (e.g. custom server options, interceptors), build the server through the config adapter and wire the `go/transport/grpc` lifecycle functions yourself:

```go
import (
    "context"

    "google.golang.org/grpc"
    "gitlab.com/phpboyscout/go/controls"
    transportgrpc "gitlab.com/phpboyscout/go/transport/grpc"
    gtbgrpc "gitlab.com/phpboyscout/go-tool-base/pkg/grpc"
    "gitlab.com/phpboyscout/go-tool-base/pkg/logger"
)

view := p.Config.View()

// Create the server from the server.grpc block, forwarding grpc.ServerOption values
srv, err := gtbgrpc.NewServerFromReader(view,
    grpc.ChainUnaryInterceptor(
        myAuthInterceptor,
        myLoggingInterceptor,
    ),
)
if err != nil {
    return err
}

// Wire health checks from the controller; the returned func stops the poller
stopHealth := transportgrpc.RegisterHealthService(srv, controller)

// Register your services
pb.RegisterMyServiceServer(srv, &myservice.Server{props: p})

// Register with the controller manually
slogger := logger.ToSlog(p.Logger)
controller.Register("grpc",
    controls.WithStart(gtbgrpc.StartFromReader(view, p.Logger, srv)),
    controls.WithStop(func(ctx context.Context) {
        stopHealth()
        transportgrpc.Stop(slogger, srv)(ctx)
    }),
    controls.WithStatus(transportgrpc.Status(srv)),
)
```

`StartFromReader` reads the port and TLS from the same block `NewServerFromReader` did; `Stop` and `Status` are the module's own and need no configuration.

---

## Health Protocol

`RegisterHealthService` wires the [gRPC Health Checking Protocol](https://github.com/grpc/grpc/blob/master/doc/health-checking.md) to the controller's `Status()`, `Liveness()`, and `Readiness()` reports:

| gRPC service name | Controller method | Meaning |
|-------------------|-------------------|---------|
| `""` (default) | `Status()` | Overall health of all services |
| `"liveness"` | `Liveness()` | Process is alive |
| `"readiness"` | `Readiness()` | Ready to accept traffic |

The health status is updated every 10 seconds in a background goroutine tied to the controller's context.

Check health externally:

```bash
grpcurl -plaintext localhost:50051 grpc.health.v1.Health/Check
```

---

## Adding Liveness and Readiness Probes to Services

The health service reflects the probes registered on individual services. Wire them when you `Register` a service:

```go
controller.Register("myservice",
    controls.WithStart(startFunc),
    controls.WithStop(stopFunc),
    controls.WithStatus(statusFunc),
    controls.WithLiveness(func() error {
        // return nil if alive, error if the process should be restarted
        return nil
    }),
    controls.WithReadiness(func() error {
        // return nil if ready to accept traffic
        if !db.IsConnected() {
            return errors.New("database not connected")
        }
        return nil
    }),
)
```

---

## Related Documentation

- **[Managing Background Services](manage-background-services.md)**: controller setup, service registration basics
- **[Controls component](../explanation/components/controls/index.md)**: `Controllable`, `Runner`, `HealthReporter` interface reference
- **[gRPC component](../explanation/components/grpc.md)**: the `pkg/grpc` config adapters (`RegisterFromReader`, `NewServerFromReader`, `StartFromReader`, `DialLocalFromReader`) over the [go/transport gRPC server](https://transport.go.phpboyscout.uk)
- **[TLS component](../explanation/components/tls.md)**: shared TLS config, the typed `Pair`, and per-transport resolution
- **[Gateway component](../explanation/components/gateway.md)**: expose the gRPC service as REST via grpc-gateway
- **[go/transport-openapi](https://transport-openapi.go.phpboyscout.uk)**: serve an OpenAPI spec and a Stoplight docs site

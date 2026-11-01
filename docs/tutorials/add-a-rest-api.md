---
title: Add a REST API
description: Put a JSON-over-HTTP API beside the gRPC service on the same controller, first with hand-written net/http handlers and then from an OpenAPI spec with oapi-codegen, with health endpoints and the shared TLS certificate.
date: 2026-11-01
tags: [tutorial, http, rest, openapi, oapi-codegen, service]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# Add a REST API

By the end of this, `mytool serve` answers the same macguffins over plain JSON
and HTTPS as well as gRPC, from one store, on one controller. You'll build the
HTTP side twice, once by hand on the standard library and once from an OpenAPI
spec, and keep whichever suits you. Both get health endpoints and TLS without
extra code.

This is the third part of the web-service series. It builds on
[Serve gRPC over TLS](serve-grpc-over-tls.md).

Allow about thirty-five minutes.

## Before you start

- **The `serve` command and macguffin store** from
  [Serve gRPC over TLS](serve-grpc-over-tls.md), with its mkcert certificate.
- **The same gtb release.** Everything below is verified against **v0.50.0**.

## Write the handlers by hand

`net/http`'s `ServeMux` matches on method and path, so a small REST API needs no
router. The handlers are another thin adapter over the store, like the gRPC
server. Create `internal/resthand/server.go`:

```go
package resthand

import (
	"encoding/json"
	"net/http"

	"github.com/myorg/mytool/internal/macguffin"
)

type API struct {
	store *macguffin.Store
}

func New(store *macguffin.Store) *API {
	return &API{store: store}
}

func (a *API) Routes() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /macguffins", a.list)
	mux.HandleFunc("GET /macguffins/{id}", a.get)
	mux.HandleFunc("POST /macguffins", a.create)

	return mux
}

func (a *API) list(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, a.store.List())
}

func (a *API) get(w http.ResponseWriter, r *http.Request) {
	m, ok := a.store.Get(r.PathValue("id"))
	if !ok {
		http.Error(w, "macguffin not found", http.StatusNotFound)

		return
	}

	writeJSON(w, http.StatusOK, m)
}

type createRequest struct {
	Name     string `json:"name"`
	Quantity int32  `json:"quantity"`
}

func (a *API) create(w http.ResponseWriter, r *http.Request) {
	var req createRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Name == "" {
		http.Error(w, "name is required", http.StatusBadRequest)

		return
	}

	writeJSON(w, http.StatusCreated, a.store.Create(req.Name, req.Quantity))
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	body, err := json.Marshal(v)
	if err != nil {
		http.Error(w, "encoding response", http.StatusInternalServerError)

		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(append(body, '\n'))
}
```

`{id}` in a pattern is a wildcard the handler reads with `r.PathValue("id")`.
The store's `Macguffin` already has JSON tags, so these handlers encode it as it
is, with nothing to convert.

`writeJSON` marshals before it writes so a failure can still become a 500. If
you write `json.NewEncoder(w).Encode(v)` and discard its error instead, the
scaffold's linter stops the build.

## Run it beside the gRPC server

Register the handler on the same controller as the gRPC server, sharing the
store. In `pkg/cmd/serve/main.go`, add two imports:

```go
	gtbhttp "gitlab.com/phpboyscout/go-tool-base/pkg/http"
```

```go
	"github.com/myorg/mytool/internal/resthand"
```

Then register the HTTP server after the gRPC one:

```go
	macguffinv1.RegisterMacguffinServiceServer(grpcSrv, grpcsvc.New(store))

	handler := resthand.New(store).Routes()
	if _, err := gtbhttp.RegisterFromReader(ctx, "http", controller, props.Config.View(), props.Logger, handler); err != nil {
		return err
	}
```

`RegisterFromReader` is the HTTP counterpart of the gRPC call. It builds an
`http.Server` from the `server.http` config block, with read, write and idle
timeouts and size limits a bare `http.Server{}` doesn't have. It mounts
`/healthz`, `/livez` and `/readyz` beside your routes, and registers the server
on the controller as `http`.

Give it a port in `pkg/cmd/serve/assets/config.yaml`:

```yaml
server:
  grpc:
    port: 50051
    reflection: true
  http:
    port: 8443
```

The `server.tls` block in your own config from the last part covers both
transports, so the HTTP server serves HTTPS with the same certificate.

Build and run it:

```bash
just build
./bin/mytool serve
```

```
INFO  starting http server tls=true addr=[::]:8443
INFO  starting gRPC server tls=true addr=[::]:50051
```

If something else on your machine has port 8443, the HTTP service can't start
and gives up, and the tool exits with
`failed to listen: listen tcp :8443: bind: address already in use`. Pick another
port for this run with `MYTOOL_SERVER_HTTP_PORT=18443 ./bin/mytool serve`, and use
that port below.

## Call it with curl

In a second terminal:

```bash
CA="$(mkcert -CAROOT)/rootCA.pem"
curl --cacert "$CA" https://localhost:8443/macguffins
curl --cacert "$CA" https://localhost:8443/macguffins/m-1
curl --cacert "$CA" -d '{"name":"sankara-stone","quantity":5}' https://localhost:8443/macguffins
```

```
[{"id":"m-1","name":"maltese-falcon","quantity":1}]
{"id":"m-1","name":"maltese-falcon","quantity":1}
{"id":"m-2","name":"sankara-stone","quantity":5}
```

The create answers `201 Created`, and an unknown id answers
`404` with `macguffin not found`.

Both transports read the one store, so the macguffin you just created over
HTTP is there over gRPC too:

```bash
grpcurl -cacert "$CA" localhost:50051 macguffin.v1.MacguffinService/ListMacguffins
```

The response lists both `maltese-falcon` and `sankara-stone`.

Now the health endpoint:

```bash
curl --cacert "$CA" https://localhost:8443/healthz
```

```
{"overall_healthy":true,"state":"running","services":[{"name":"grpc","status":"OK"},{"name":"http","status":"OK"}]}
```

It reports both services, because both are on the one controller. `/livez` and
`/readyz` answer the same way today. They differ once a service reports liveness
and readiness separately. A single SIGTERM stops both servers:

```
INFO  Stopping Services component=controller cause=parent
INFO  stopping http server addr=:8443
INFO  Stopping gRPC server
INFO  Stopped component=controller
```

## Generate the handlers from a spec instead

The other way is to describe the API in an OpenAPI document and generate the Go
from it, the same idea as the proto in the last part.
[oapi-codegen](https://github.com/oapi-codegen/oapi-codegen) does the
generating. Install it:

```bash
go install github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@latest
```

Write the contract to `api/macguffin.openapi.yaml`:

```yaml
openapi: 3.0.3
info:
  title: Macguffin API
  version: 1.0.0
paths:
  /macguffins:
    get:
      operationId: listMacguffins
      responses:
        "200":
          description: OK
          content:
            application/json:
              schema:
                type: array
                items:
                  $ref: "#/components/schemas/Macguffin"
    post:
      operationId: createMacguffin
      requestBody:
        required: true
        content:
          application/json:
            schema:
              $ref: "#/components/schemas/NewMacguffin"
      responses:
        "201":
          description: Created
          content:
            application/json:
              schema:
                $ref: "#/components/schemas/Macguffin"
        "400":
          description: Invalid request
  /macguffins/{id}:
    get:
      operationId: getMacguffin
      parameters:
        - name: id
          in: path
          required: true
          schema:
            type: string
      responses:
        "200":
          description: OK
          content:
            application/json:
              schema:
                $ref: "#/components/schemas/Macguffin"
        "404":
          description: Not found
components:
  schemas:
    Macguffin:
      type: object
      required: [id, name, quantity]
      properties:
        id:
          type: string
        name:
          type: string
        quantity:
          type: integer
          format: int32
    NewMacguffin:
      type: object
      required: [name]
      properties:
        name:
          type: string
        quantity:
          type: integer
          format: int32
```

Tell oapi-codegen what to emit, in `api/oapi-codegen.yaml`. `std-http-server`
targets the standard library, the same `ServeMux` as the hand-written version:

```yaml
package: restapi
output: internal/restapi/macguffin.gen.go
generate:
  std-http-server: true
  models: true
```

Add it to `gen.go`, under the buf line, so `just build` regenerates both:

```go
package mytool

//go:generate buf generate
//go:generate oapi-codegen -config api/oapi-codegen.yaml api/macguffin.openapi.yaml
```

```bash
mkdir -p internal/restapi
go generate ./...
```

`internal/restapi/macguffin.gen.go` now holds a `Macguffin` type, a
`NewMacguffin` request type and a `ServerInterface`, one method per operation:

```go
type ServerInterface interface {
	ListMacguffins(w http.ResponseWriter, r *http.Request)
	CreateMacguffin(w http.ResponseWriter, r *http.Request)
	GetMacguffin(w http.ResponseWriter, r *http.Request, id string)
}
```

The path parameter arrives as a typed `id`. Implement the interface over the
store in `internal/restapi/server.go`:

```go
package restapi

import (
	"encoding/json"
	"net/http"

	"github.com/myorg/mytool/internal/macguffin"
)

type API struct {
	store *macguffin.Store
}

var _ ServerInterface = (*API)(nil)

func NewAPI(store *macguffin.Store) *API {
	return &API{store: store}
}

func (a *API) ListMacguffins(w http.ResponseWriter, _ *http.Request) {
	items := a.store.List()

	out := make([]Macguffin, 0, len(items))
	for _, m := range items {
		out = append(out, toDTO(m))
	}

	writeJSON(w, http.StatusOK, out)
}

func (a *API) GetMacguffin(w http.ResponseWriter, _ *http.Request, id string) {
	m, ok := a.store.Get(id)
	if !ok {
		http.Error(w, "macguffin not found", http.StatusNotFound)

		return
	}

	writeJSON(w, http.StatusOK, toDTO(m))
}

func (a *API) CreateMacguffin(w http.ResponseWriter, r *http.Request) {
	var req CreateMacguffinJSONRequestBody
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Name == "" {
		http.Error(w, "name is required", http.StatusBadRequest)

		return
	}

	var quantity int32
	if req.Quantity != nil {
		quantity = *req.Quantity
	}

	writeJSON(w, http.StatusCreated, toDTO(a.store.Create(req.Name, quantity)))
}

func toDTO(m macguffin.Macguffin) Macguffin {
	return Macguffin{Id: m.ID, Name: m.Name, Quantity: m.Quantity}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	body, err := json.Marshal(v)
	if err != nil {
		http.Error(w, "encoding response", http.StatusInternalServerError)

		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(append(body, '\n'))
}
```

Two things differ from the hand-written version:

- **The generated `Macguffin` is its own type**, so `toDTO` converts the
  store's into it, as `toProto` does for gRPC. That's the same data mapped into
  two generated shapes by hand. The next part removes that.
- **`quantity` is optional in the spec**, so the generated request type makes it
  a pointer, and the handler checks it before use.

`var _ ServerInterface = (*API)(nil)` makes the compiler check that `API`
implements every operation, so a new operation in the spec fails the build
until you write it.

Swap the handler in `pkg/cmd/serve/main.go`. Replace the `resthand` import with
`restapi`, add `"net/http"`, and change the handler line:

```go
	handler := restapi.HandlerFromMux(restapi.NewAPI(store), http.NewServeMux())
```

Build and run it, and the curl calls above give exactly the same answers.

## Which to keep

Hand-written handlers suit a few endpoints that only you call. There's no
generator to own, and nothing to convert. A spec suits an API other teams
consume, or one that changes shape often, because the document is the contract
and the compiler holds you to it. It also gives you an OpenAPI document, which
a later part serves as live API docs.

Either way, the server, its timeouts, TLS, health endpoints and shutdown came
from `RegisterFromReader`.

## Where this leaves you

`mytool serve` answers gRPC and REST from one store, under one controller, over
one certificate, with health endpoints that report both. What you wrote was the
adapters: routes, encoding and, on the spec road, a second mapping of the same
data. The next part puts the REST API in front of the gRPC service instead, so
that layer disappears.

## What to read next

- [HTTP component](../explanation/components/http.md): what the config adapter
  reads, the timeouts and limits, and middleware.
- [Register health checks](../how-to/register-health-checks.md): checks of your
  own, such as a database, reported through the same endpoints.

---
title: Serve Interactive API Docs
description: Generate an OpenAPI v3 spec from your protobuf and serve it with an embedded Stoplight Elements docs site and a working "try it" console.
date: 2026-05-31
tags: [how-to, openapi, documentation, stoplight]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# Serve Interactive API Docs

GTB's `pkg/openapi` package serves an OpenAPI specification alongside an interactive [Stoplight Elements](https://stoplight.io/open-source/elements) docs site — complete with a "try it" console — from a single `Register` call. The Stoplight Elements UI is embedded in the framework, so your project ships only its generated spec. There is no per-project vendoring of the UI.

This guide takes you from a `.proto` file to a browsable, runnable docs site mounted alongside your REST API.

---

## Prerequisites

You need an existing REST/gateway server (an `*http.ServeMux` exposing your grpc-gateway routes). If you're starting from gRPC, see **[Expose a gRPC Service as REST](expose-grpc-as-rest.md)** first — the docs mount onto the same mux.

---

## Step 1: Install the v3 Generator Plugin

grpc-gateway ships its own OpenAPI generator (`protoc-gen-openapiv2`), but it emits OpenAPI v2 (Swagger). Stoplight Elements wants OpenAPI v3, so we use a v3 generator instead — [`kollalabs/protoc-gen-openapi`](https://github.com/kollalabs/protoc-gen-openapi), which understands the same `google.api.http` annotations as the gateway and emits OpenAPI 3.x.

Install it:

```bash
go install github.com/kollalabs/protoc-gen-openapi@latest
```

This puts the `protoc-gen-openapi` binary on your `PATH`, where buf can find it as a local plugin.

---

## Step 2: Generate the v3 Spec with buf

Add a plugin entry to your `buf.gen.yaml` alongside the `protoc-gen-go`, `protoc-gen-go-grpc` and `protoc-gen-grpc-gateway` plugins you already run:

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
  # OpenAPI v3 (kollalabs/protoc-gen-openapi) — understands google.api.http
  # annotations and emits OpenAPI 3.x. Written into the docs asset bundle so it
  # is embedded and served alongside the Stoplight UI.
  - local: protoc-gen-openapi
    out: internal/docs/assets
    opt:
      - title=Widget API
      - default_response=false
```

A few things worth calling out:

- `out: internal/docs/assets` writes the spec straight into the package that embeds it (next step), so you never hand-copy the file.
- `opt: title=Widget API` sets the `info.title` on the generated document. Make it match the title you give Stoplight in Step 4.
- `default_response=false` keeps the generator from inventing a catch-all error response on every operation.

Run the generation:

```bash
buf generate
```

You should now have `internal/docs/assets/openapi.yaml`.

---

## Step 3: Embed the Spec

The `openapi.Register` call takes the spec as a `[]byte`. Embed the generated file into the binary with `//go:embed` so there's nothing to ship or locate at runtime:

```go
// internal/docs/docs.go
package docs

import (
	_ "embed"
	"net/http"

	"gitlab.com/phpboyscout/go-tool-base/pkg/openapi"
)

//go:embed assets/openapi.yaml
var spec []byte
```

Embedding only your own spec keeps the project lean — the Stoplight Elements assets (the JavaScript and CSS) ship inside go-tool-base, not your repository.

---

## Step 4: Register the Docs Endpoints

Call `openapi.Register` with your mux and the embedded spec. It mounts two endpoints:

| Method & Path | Serves |
|---------------|--------|
| `GET /openapi.yaml` | The spec bytes (Content-Type `application/yaml`) |
| `GET /docs/` | The Stoplight Elements UI (the try-it console) |

```go
// Register mounts /openapi.yaml and the Stoplight docs site (/docs/) onto mux.
func Register(mux *http.ServeMux) error {
	return openapi.Register(mux, spec, openapi.WithTitle("Widget API"))
}
```

`WithTitle` sets the docs page title; the defaults (`/openapi.yaml` and `/docs/`) can be overridden with `WithSpecPath` and `WithDocsPath` if you need them elsewhere.

---

## Step 5: Mount on the Same Server as Your API

This is the step that makes the "try it" console actually work, so don't skip it: register the docs onto **the same `*http.ServeMux` that serves your REST/gateway routes**.

```go
mux := http.NewServeMux()

// Your existing REST/gateway handler, e.g.
// mux.Handle("/", gatewayMux)

if err := docs.Register(mux); err != nil {
	return err
}
```

When the spec and the docs site are served from the same origin as the API, Stoplight's "try it" console can call your endpoints directly — no CORS configuration, no proxy, nothing. (Internally the Elements UI is configured with `tryItCredentialsPolicy="same-origin"`, which is what that buys you.) Split the docs onto a separate server or port and the browser's same-origin policy will block the console's requests, and you'd be back to writing CORS rules to undo it.

---

## Step 6: Test It

Start your server, then curl the spec endpoint:

```bash
curl -s http://localhost:8080/openapi.yaml | head
# openapi: 3.0.3
# info:
#   title: Widget API
#   ...
```

Then open the docs site in a browser:

```
http://localhost:8080/docs/
```

You should see the Stoplight Elements UI with your operations in the sidebar. Pick an endpoint, fill in the request, and hit **Send** — the call goes to your live API on the same origin and the response comes back in the console.

---

## Related Documentation

- **[OpenAPI component](../explanation/components/openapi.md)** — `Register`, the `WithSpecPath`/`WithDocsPath`/`WithTitle` options, and the embedded Stoplight assets
- **[Gateway component](../explanation/components/gateway.md)** — the grpc-gateway REST mux the docs are mounted alongside
- **[Expose a gRPC Service as REST](expose-grpc-as-rest.md)** — wire the gateway routes the spec documents
- **[Add a gRPC Management Service](add-grpc-service.md)** — register the gRPC server the gateway fronts

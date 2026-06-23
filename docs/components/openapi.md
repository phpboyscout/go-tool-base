---
title: OpenAPI
description: Serve an OpenAPI specification and an interactive Stoplight Elements docs site from a single Register call.
date: 2026-05-31
tags: [components, openapi, documentation, http]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# OpenAPI

The `pkg/openapi` package serves an OpenAPI specification alongside an interactive [Stoplight Elements](https://stoplight.io/open-source/elements) documentation site (complete with a "try it" console) from a single `Register` call.

The Stoplight Elements UI is embedded in the framework, so a project ships only its generated spec — there is no per-project vendoring of the UI. The Stoplight Elements distribution under `assets/` is vendored from Stoplight (Apache-2.0), pinned at v9.0.0.

## Register

`Register` mounts both the OpenAPI document and the Stoplight Elements docs site onto a provided `*http.ServeMux`:

```go
func Register(mux *http.ServeMux, spec []byte, opts ...Option) error
```

It mounts two endpoints:

| Method & Path | Serves |
|---------------|--------|
| `GET /openapi.yaml` | The spec bytes (Content-Type `application/yaml`) |
| `GET /docs/` | The Stoplight Elements UI (the try-it console) |

The docs subtree also serves the embedded JavaScript and CSS assets beneath the docs path prefix.

Serving both the spec and the docs site on the same server keeps the spec same-origin with the API it documents, so the "try it" console works without any extra CORS configuration.

**Security headers are applied automatically.** Both built-in handlers are wrapped with [`SecurityHeadersMiddleware`](http.md#built-in-security-headers-middleware) — the try-it console benefits from `nosniff`/frame/referrer protections. This is one of the few places GTB applies transport middleware for you; customise it with `WithSecurityHeaderOptions(...)` or opt out with `WithoutSecurityHeaders()`. See the [Transport Middleware & Resilience](../concepts/transport-middleware.md) concept.

## Options

`Register` accepts functional options to override the defaults.

| Option | Default | Description |
|--------|---------|-------------|
| `WithSpecPath(p string)` | `/openapi.yaml` | Path the OpenAPI document is served at. |
| `WithDocsPath(p string)` | `/docs/` | Path prefix the Stoplight UI is served at. Must end in a slash. |
| `WithTitle(t string)` | `API documentation` | The docs page title. |
| `WithSecurityHeaderOptions(opts ...http.SecurityHeadersOption)` | GTB defaults | Customise the auto-applied security headers. |
| `WithoutSecurityHeaders()` | *(on)* | Disable the auto-applied security headers. |

## Generating a Spec

The package serves whatever spec bytes you hand it; producing the document is the project's responsibility. A common approach is to generate the OpenAPI v3 document from Protobuf definitions with [buf](https://buf.build) and a v3 generator plugin such as [`kollalabs/protoc-gen-openapi`](https://github.com/kollalabs/protoc-gen-openapi). The generated `openapi.yaml` is then embedded into the binary with `//go:embed` and passed straight to `Register`.

## Usage Example

```go
package main

import (
	_ "embed"
	"net/http"

	"gitlab.com/phpboyscout/go-tool-base/pkg/openapi"
)

//go:embed openapi.yaml
var spec []byte

func main() {
	mux := http.NewServeMux()

	// Mount GET /openapi.yaml and GET /docs/
	if err := openapi.Register(mux, spec,
		openapi.WithTitle("Widget Service API"),
	); err != nil {
		panic(err)
	}

	srv := &http.Server{
		Addr:    ":8080",
		Handler: mux,
	}

	if err := srv.ListenAndServe(); err != nil {
		panic(err)
	}
}
```

With the server running, the raw specification is available at `GET /openapi.yaml` and the interactive Stoplight Elements docs site at `GET /docs/`.

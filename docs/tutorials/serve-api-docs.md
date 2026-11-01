---
title: Serve live API docs
description: Generate an OpenAPI 3 document from the annotated proto and serve it with an interactive Stoplight Elements docs site whose "try it" console calls the running API.
date: 2026-11-01
tags: [tutorial, openapi, api-docs, stoplight, protobuf]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# Serve live API docs

By the end of this, `https://localhost:8443/docs/` is an interactive API
reference generated from the proto: every endpoint listed, every field typed,
and a console that sends real requests to the running service. You won't
write any of it. The HTTP routes you added to the proto in the last part
describe the API, and the docs come from them.

This is the fifth part of the web-service series. It builds on
[Put a gateway in front of gRPC](put-a-gateway-in-front.md).

Allow about twenty minutes.

## Before you start

- **The gateway** from [Put a gateway in front of gRPC](put-a-gateway-in-front.md),
  with its HTTP routes in the proto.
- **The same gtb release.** Everything below is verified against **v0.50.0**.
- **A browser on the same machine,** for the last step.

## Generate the OpenAPI document

The OpenAPI document comes from one more buf plugin. grpc-gateway has its own
generator, but it writes OpenAPI v2 (the older Swagger format), and the docs
viewer below wants v3. [`kollalabs/protoc-gen-openapi`](https://github.com/kollalabs/protoc-gen-openapi)
reads the same `google.api.http` routes and writes v3. Install it:

```bash
go install github.com/kollalabs/protoc-gen-openapi@latest
```

Add it to the end of `buf.gen.yaml`:

```yaml
  - local: protoc-gen-openapi
    out: internal/docs/assets
    opt:
      - title=Macguffin API
      - default_response=false
```

`out` writes the document straight into the package that serves it, next.
`title` becomes the document's title. `default_response=false` stops the
generator adding a catch-all error response to every operation.

Regenerate:

```bash
go generate ./...
head -20 internal/docs/assets/openapi.yaml
```

```yaml
# Generated with protoc-gen-openapi
# https://github.com/kollalabs/protoc-gen-openapi

openapi: 3.0.3
info:
    title: Macguffin API
    version: 0.0.1
paths:
    /v1/macguffins:
        get:
            tags:
                - MacguffinService
            summary: ListMacguffins
            operationId: MacguffinService_ListMacguffins
            parameters:
                - name: pageSize
                  in: query
                  schema:
                    type: integer
                    format: int32
```

The list's `page_size` request field became a `pageSize` query parameter. The
gateway accepts it, because it maps any request field that isn't in the path or
the body from the query string.

## Serve the document and the docs site

The [`go/transport-openapi`](https://transport-openapi.go.phpboyscout.uk) module
serves an OpenAPI document and a
[Stoplight Elements](https://stoplight.io/open-source/elements) docs site from
one call. The site's JavaScript and CSS ship inside the module, so your project
only carries its own document. Add it:

```bash
go get gitlab.com/phpboyscout/go/transport-openapi
```

At v0.50.0 that also moves `gitlab.com/phpboyscout/go/transport` up a patch
release, from v0.6.3 to v0.6.4. That's expected, and the build is fine with it.

Create `internal/docs/docs.go`. It embeds the generated document and registers
both endpoints:

```go
package docs

import (
	_ "embed"
	"net/http"

	openapi "gitlab.com/phpboyscout/go/transport-openapi"
)

//go:embed assets/openapi.yaml
var spec []byte

func Register(mux *http.ServeMux) error {
	return openapi.Register(mux, spec, openapi.WithTitle("Macguffin API"))
}
```

`openapi.Register` mounts two routes:

| Path | Serves |
|---|---|
| `GET /openapi.yaml` | the document, as `application/yaml` |
| `GET /docs/` | the docs site |

## Mount it beside the API

In `pkg/cmd/serve/main.go`, import the package with the project's own imports:

```go
	"github.com/myorg/mytool/internal/docs"
```

Register it on the same mux as the gateway and the page:

```go
	mux := http.NewServeMux()
	mux.Handle("/v1/", gw)
	mux.Handle("/", site.New(store).Routes())

	if err := docs.Register(mux); err != nil {
		return err
	}
```

The same mux is what makes the "try it" console work. The docs page and the API
share an origin, so the browser lets the console call the API, with no CORS rules
to write. Put the docs on another port or host and the browser blocks those
calls.

`/openapi.yaml` and `/docs/` are more specific than the site's `/`, so they take
priority and the page stays where it was.

Tidy, build and run:

```bash
go mod tidy
just build
./bin/mytool serve
```

## Check the endpoints

```bash
CA="$(mkcert -CAROOT)/rootCA.pem"
curl --cacert "$CA" -sS -D - -o /dev/null https://localhost:8443/openapi.yaml | grep -i content-type
curl --cacert "$CA" -sS -o /dev/null -w '%{http_code} %{content_type}\n' https://localhost:8443/docs/
```

```
content-type: application/yaml
200 text/html; charset=utf-8
```

`/docs` without the slash redirects to `/docs/`.

## Try it in the browser

Open `https://localhost:8443/docs/`. If you ran `mkcert -install`, there's no
certificate warning. The sidebar lists the three operations under
`MacguffinService`, and the three schemas.

Pick **ListMacguffins** and press **Send API Request**. The console calls the
live API and shows the answer:

```
200 OK

{
  "macguffins": [
    {
      "id": "m-1",
      "name": "maltese-falcon",
      "quantity": 1
    }
  ]
}
```

That came from your store, through the gateway and the gRPC service. The docs
also show a curl command for each request, ready to copy.

## Keep it in step

The document is generated from the proto on every `just build`, and embedded in
the binary. Add a field to a message or an RPC with a route, and the next build
updates the gRPC API, the REST API and the docs together. There's nothing to
keep in sync by hand.

## Where this leaves you

From one `.proto` file you get a gRPC API, a REST API through the gateway, and
live docs with a working console, all served by one tool, on one certificate,
under one controller. The last part of the series adds observability: logs,
metrics and traces for every request.

## What to read next

- [Serve API docs](../how-to/serve-api-docs.md): changing the paths and title,
  and the generator's options.
- [go/transport-openapi](https://transport-openapi.go.phpboyscout.uk): the full
  `Register` API.

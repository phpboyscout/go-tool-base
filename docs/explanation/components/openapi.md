---
title: OpenAPI
description: Serving an OpenAPI spec + Stoplight Elements docs site has been extracted to the standalone go/transport-openapi companion module.
date: 2026-07-22
tags: [components, openapi, documentation, http, module-extraction]
authors: [Matt Cockayne <matt@phpboyscout.uk>]
---

# OpenAPI

Serving an OpenAPI specification and an interactive
[Stoplight Elements](https://stoplight.io/open-source/elements) docs site (with a
"try it" console) from a single `Register(mux, spec, opts...)` call now lives in the
standalone module
**[`gitlab.com/phpboyscout/go/transport-openapi`](https://gitlab.com/phpboyscout/go/transport-openapi)**.

It is a **companion to [`go/transport`](https://transport.go.phpboyscout.uk)**, not
part of its core: the Stoplight Elements distribution it embeds is ~2.4 MB, so
keeping it a separate import means only tools that actually serve API docs pay for
the embed. The handler mounts on any `*http.ServeMux` and wraps its routes with
`go/transport`'s security-header middleware by default.

- **Docs:** [transport-openapi.go.phpboyscout.uk](https://transport-openapi.go.phpboyscout.uk)
- **API reference:** [pkg.go.dev](https://pkg.go.dev/gitlab.com/phpboyscout/go/transport-openapi)
- **Migration:** [`go/transport-openapi` → `go/transport-openapi`](../../reference/migration/v0.x-openapi-extracted.md)

```go
import openapi "gitlab.com/phpboyscout/go/transport-openapi"

// GET /openapi.yaml -> the spec ; GET /docs/ -> the Stoplight UI
if err := openapi.Register(mux, spec, openapi.WithTitle("Widget API")); err != nil {
    return err
}
```

GTB itself does not consume this module. It is a downstream-facing feature for
tools built on GTB. Import it directly alongside `go/transport`.

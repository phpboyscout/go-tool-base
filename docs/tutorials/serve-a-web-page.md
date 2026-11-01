---
title: Serve a web page
description: Serve escaped HTML and static files from the same HTTP server as the REST API, with html/template and embed, so the page ships inside the binary.
date: 2026-11-01
tags: [tutorial, http, html-template, embed, service]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# Serve a web page

By the end of this, `https://localhost:8443/` shows a page listing the
macguffins, with a stylesheet, served by the same HTTP server as the REST API.
The template and the stylesheet are baked into the binary, so `mytool` still
ships as one file.

This is a short side step in the web-service series, after
[Add a REST API](add-a-rest-api.md). A later part uses the same technique to
serve API docs.

Allow about fifteen minutes.

## Before you start

- **The REST API** from [Add a REST API](add-a-rest-api.md), registered on the
  controller as `http`. Either version of the handler works.
- **The same gtb release.** Everything below is verified against **v0.50.0**.

## Write the template

Go's `html/template` renders HTML from your data, and escapes that data as it
goes. Create `internal/site/templates/index.html`:

```html
<!doctype html>
<html lang="en">
  <head>
    <meta charset="utf-8" />
    <title>Macguffins</title>
    <link rel="stylesheet" href="/static/style.css" />
  </head>
  <body>
    <h1>The macguffin catalogue</h1>
    <ul>
      {{range .}}
      <li>{{.Name}} <span class="qty">&times;{{.Quantity}}</span></li>
      {{end}}
    </ul>
  </body>
</html>
```

`{{range .}}` walks the list of macguffins the page is given, and `{{.Name}}`
and `{{.Quantity}}` read each one's fields.

And a stylesheet, `internal/site/static/style.css`:

```css
body { font-family: system-ui, sans-serif; max-width: 40rem; margin: 2rem auto; }
.qty { color: #666; }
```

## Embed the files and serve them

Create `internal/site/site.go`:

```go
package site

import (
	"embed"
	"html/template"
	"io/fs"
	"net/http"

	"github.com/myorg/mytool/internal/macguffin"
)

//go:embed templates static
var content embed.FS

var tmpl = template.Must(template.ParseFS(content, "templates/index.html"))

type Site struct {
	store *macguffin.Store
}

func New(store *macguffin.Store) *Site {
	return &Site{store: store}
}

func (s *Site) Routes() *http.ServeMux {
	static, err := fs.Sub(content, "static")
	if err != nil {
		panic(err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", s.index)
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.FS(static))))

	return mux
}

func (s *Site) index(w http.ResponseWriter, _ *http.Request) {
	if err := tmpl.Execute(w, s.store.List()); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}
```

Three lines do the work:

- **`//go:embed templates static`** copies both folders into the binary at
  build time, as the `content` filesystem.
- **`template.ParseFS`** parses the page from that filesystem once, at startup.
- **`http.FileServer(http.FS(static))`** serves everything under `static/` from
  the embedded files, with the right content type.

`fs.Sub` can only fail if the `static` folder isn't embedded, which the
`//go:embed` line rules out, so the `panic` there never runs.

`GET /{$}` matches the root path and nothing else, so `/` renders the page and
`/anything-else` doesn't.

## Put it on the same server

The site and the REST API both want the HTTP server, but there's only one.
Registering the site as a second server would make it read the same
`server.http` block and try to bind the same port. One of the two then fails to
start, gives up, and takes the tool down with exit 1.

So mount both on one handler, and register that once. In
`pkg/cmd/serve/main.go`, import the site package:

```go
	"github.com/myorg/mytool/internal/site"
```

Then replace the HTTP registration with a mux that sends the API's paths to the
API and everything else to the site. With the spec-generated handler from the
last part, that's:

```go
	api := restapi.HandlerFromMux(restapi.NewAPI(store), http.NewServeMux())

	mux := http.NewServeMux()
	mux.Handle("/macguffins", api)
	mux.Handle("/macguffins/", api)
	mux.Handle("/", site.New(store).Routes())

	if _, err := gtbhttp.RegisterFromReader(ctx, "http", controller, props.Config.View(), props.Logger, mux); err != nil {
		return err
	}
```

If you kept the hand-written handler, set `api` to `resthand.New(store).Routes()`
instead. `/macguffins` is the list and `/macguffins/` covers each item. `/` is the
catch-all, so the site gets the root page and the stylesheet, and its own mux
answers 404 for anything else. The health endpoints stay where they were,
because `RegisterFromReader` mounts them outside your handler.

Build and run:

```bash
just build
./bin/mytool serve
```

## Look at it

Open `https://localhost:8443/` in a browser. If you ran `mkcert -install`, the
page loads with no certificate warning. Or fetch it with curl:

```bash
curl --cacert "$(mkcert -CAROOT)/rootCA.pem" https://localhost:8443/
```

```html
<!doctype html>
<html lang="en">
  <head>
    <meta charset="utf-8" />
    <title>Macguffins</title>
    <link rel="stylesheet" href="/static/style.css" />
  </head>
  <body>
    <h1>The macguffin catalogue</h1>
    <ul>
      
      <li>maltese-falcon <span class="qty">&times;1</span></li>
      
    </ul>
  </body>
</html>
```

The stylesheet comes back with its content type set:

```bash
curl --cacert "$(mkcert -CAROOT)/rootCA.pem" -o /dev/null -w '%{http_code} %{content_type}\n' https://localhost:8443/static/style.css
```

```
200 text/css; charset=utf-8
```

`/macguffins` still answers JSON, and `/healthz` still reports both `grpc` and
`http`.

## Check the escaping

Create a macguffin whose name is a script tag:

```bash
curl --cacert "$(mkcert -CAROOT)/rootCA.pem" -d '{"name":"<script>alert(1)</script>","quantity":1}' https://localhost:8443/macguffins
```

Reload the page. The name shows up as text, because the template escaped it:

```html
      <li>&lt;script&gt;alert(1)&lt;/script&gt; <span class="qty">&times;1</span></li>
```

That's `html/template` doing its job. `text/template` would have written the
tag out as it is, and the browser would have run it.

## Edit without rebuilding

The embedded files only change when you rebuild. While you're working on the
page, you can read from disk instead: `template.ParseGlob` in place of
`ParseFS`, and `http.FileServer(http.Dir("internal/site/static"))` in place of
the embedded filesystem. The handlers don't change. Switch back to `embed`
before you release, or the binary will look for files that aren't there.

## Where this leaves you

The HTTP server serves whatever handler you give it: JSON, HTML or files, over
the same TLS, with the same health endpoints and shutdown. The page ships inside
the binary. Next, the series goes back to the API and stops writing the REST
layer by hand.

## What to read next

- [HTTP component](../explanation/components/http.md): middleware, timeouts and
  what `RegisterFromReader` mounts.
- [`html/template`](https://pkg.go.dev/html/template) and
  [`embed`](https://pkg.go.dev/embed): the standard-library packages this page
  uses.

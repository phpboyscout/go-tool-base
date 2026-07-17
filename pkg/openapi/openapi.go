// Package openapi serves an OpenAPI specification and a Stoplight Elements docs
// site (interactive, with a "try it" console) from a single Register call. The
// Stoplight Elements assets are embedded in the framework, so a project ships
// only its generated spec — no per-project vendoring of the UI.
//
// The Stoplight Elements distribution under assets/ is vendored from
// https://stoplight.io/open-source/elements (Apache-2.0), pinned at v9.0.0.
package openapi

import (
	"bytes"
	"embed"
	"html/template"
	"io/fs"
	"net/http"

	"github.com/cockroachdb/errors"

	transithttp "gitlab.com/phpboyscout/go/transit/http"
	transporthttp "gitlab.com/phpboyscout/go/transport/http"
)

//go:embed assets/web-components.min.js assets/styles.min.css
var elements embed.FS

const (
	defaultSpecPath = "/openapi.yaml"
	defaultDocsPath = "/docs/"
	defaultTitle    = "API documentation"
)

var indexTemplate = template.Must(template.New("index").Parse(
	`<!doctype html>
<html lang="en">
  <head>
    <meta charset="utf-8" />
    <meta name="viewport" content="width=device-width, initial-scale=1" />
    <title>{{.Title}}</title>
    <link rel="stylesheet" href="{{.DocsPath}}styles.min.css" />
    <script src="{{.DocsPath}}web-components.min.js"></script>
  </head>
  <body style="height: 100vh; margin: 0">
    <elements-api
      apiDescriptionUrl="{{.SpecPath}}"
      router="hash"
      layout="sidebar"
      tryItCredentialsPolicy="same-origin"
    ></elements-api>
  </body>
</html>
`))

type config struct {
	specPath            string
	docsPath            string
	title               string
	securityHeaders     []transporthttp.SecurityHeadersOption
	withoutSecurityHdrs bool
}

// Option configures the docs endpoints.
type Option func(*config)

// WithSpecPath sets the path the OpenAPI document is served at (default
// "/openapi.yaml").
func WithSpecPath(p string) Option {
	return func(c *config) { c.specPath = p }
}

// WithDocsPath sets the path prefix the Stoplight UI is served at (default
// "/docs/"). It must end in a slash.
func WithDocsPath(p string) Option {
	return func(c *config) { c.docsPath = p }
}

// WithTitle sets the docs page title.
func WithTitle(t string) Option {
	return func(c *config) { c.title = t }
}

// WithSecurityHeaderOptions customises the security-header middleware applied to
// the docs/spec handlers. By default the conservative
// http.SecurityHeadersMiddleware defaults are used (nosniff, X-Frame-Options:
// DENY, frame-ancestors 'none', Referrer-Policy: no-referrer, HSTS off).
func WithSecurityHeaderOptions(opts ...transporthttp.SecurityHeadersOption) Option {
	return func(c *config) { c.securityHeaders = append(c.securityHeaders, opts...) }
}

// WithoutSecurityHeaders disables the default security-header middleware on the
// docs/spec handlers. Use this only when an outer middleware chain already sets
// equivalent headers; the built-in interactive docs UI is otherwise served
// without nosniff/frame/referrer protections.
func WithoutSecurityHeaders() Option {
	return func(c *config) { c.withoutSecurityHdrs = true }
}

// Register mounts the OpenAPI spec and the Stoplight Elements docs site onto the
// provided mux:
//
//	GET /openapi.yaml  -> the spec bytes
//	GET /docs/         -> the Stoplight UI (try-it console)
//
// Serving both on the same server keeps the spec same-origin with the API it
// documents, so the "try it" console works without extra CORS configuration.
func Register(mux *http.ServeMux, spec []byte, opts ...Option) error {
	cfg := &config{
		specPath: defaultSpecPath,
		docsPath: defaultDocsPath,
		title:    defaultTitle,
	}
	for _, opt := range opts {
		opt(cfg)
	}

	assets, err := fs.Sub(elements, "assets")
	if err != nil {
		return errors.Wrap(err, "loading embedded docs assets")
	}

	index, err := renderIndex(cfg)
	if err != nil {
		return err
	}

	// Wrap every built-in docs/spec handler with conservative security
	// headers by default — the docs path serves an interactive try-it console
	// that benefits from nosniff/frame/referrer protections. Callers may
	// customise via WithSecurityHeaderOptions or opt out via
	// WithoutSecurityHeaders.
	secure := securityWrapper(cfg)

	specHandler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/yaml")
		_, _ = w.Write(spec)
	})
	mux.Handle("GET "+cfg.specPath, secure(specHandler))

	indexHandler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(index)
	})

	// Exact docs path -> the generated index; subtree -> the embedded JS/CSS.
	mux.Handle("GET "+cfg.docsPath+"{$}", secure(indexHandler))
	mux.Handle("GET "+cfg.docsPath, secure(http.StripPrefix(cfg.docsPath, http.FileServer(http.FS(assets)))))

	return nil
}

// securityWrapper returns the middleware applied to the docs/spec handlers:
// the security-headers middleware by default, or a no-op pass-through when
// WithoutSecurityHeaders was supplied.
func securityWrapper(cfg *config) transithttp.Middleware {
	if cfg.withoutSecurityHdrs {
		return func(next http.Handler) http.Handler { return next }
	}

	return transporthttp.SecurityHeadersMiddleware(cfg.securityHeaders...)
}

func renderIndex(cfg *config) ([]byte, error) {
	data := map[string]string{
		"Title":    cfg.title,
		"SpecPath": cfg.specPath,
		"DocsPath": cfg.docsPath,
	}

	var buf bytes.Buffer
	if err := indexTemplate.Execute(&buf, data); err != nil {
		return nil, errors.Wrap(err, "rendering docs index")
	}

	return buf.Bytes(), nil
}

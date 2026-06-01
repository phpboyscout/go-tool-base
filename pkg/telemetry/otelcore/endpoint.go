package otelcore

import (
	"net/url"

	"github.com/cockroachdb/errors"
)

// Endpoint is a parsed OTLP/HTTP base URL, split into the components every signal
// exporter needs. Each signal appends its own suffix to BasePath: "/v1/traces",
// "/v1/metrics" or "/v1/logs".
type Endpoint struct {
	Host     string            // host:port, e.g. "collector:4318"
	BasePath string            // path prefix the per-signal suffix is appended to
	Insecure bool              // plaintext OTLP (http scheme, or an explicit insecure flag)
	Headers  map[string]string // exporter headers, e.g. an auth token
}

// ParseEndpoint splits an OTLP/HTTP base URL into exporter components. An http
// scheme, or insecure being true, marks the endpoint as plaintext — use that
// only for a local collector.
func ParseEndpoint(rawURL string, insecure bool, headers map[string]string) (Endpoint, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return Endpoint{}, errors.Wrap(err, "parsing OTLP endpoint URL")
	}

	return Endpoint{
		Host:     u.Host,
		BasePath: u.Path,
		Insecure: u.Scheme == "http" || insecure,
		Headers:  headers,
	}, nil
}

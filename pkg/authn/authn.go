// Package authn provides transport-agnostic credential verification primitives
// for the HTTP and gRPC server transports. It carries no HTTP or gRPC imports,
// so it is trivially unit-testable and reusable from both transports (and by
// tools directly).
//
// A Verifier authenticates a credential and returns the verified Identity, or an
// error. Verifiers MUST NOT leak the reason for failure in a user-facing form —
// the returned error is for the server log only; the transport adapters map it
// to a generic wire status (401 / Unauthenticated) and log the detail redacted.
// Authorization (deciding what a verified principal may do) is a single
// tool-supplied predicate, AuthorizeFunc — GTB ships no policy engine.
package authn

import (
	"context"

	"github.com/cockroachdb/errors"
)

// Identity is the verified outcome of a successful authentication. It carries
// the minimum a downstream handler needs to make authorization decisions.
type Identity struct {
	// Subject is the principal identifier: the "sub" claim for JWT, the
	// configured label for an API key, or the certificate subject for mTLS.
	Subject string
	// Method records which verifier authenticated the request: "apikey", "jwt",
	// or "mtls".
	Method string
	// Claims holds verified JWT claims (nil for API-key and mTLS auth).
	Claims map[string]any
	// Scopes is the parsed "scope"/"scp" claim when present, for convenience.
	Scopes []string
}

// Verifier authenticates a single credential string (the raw bearer token or
// API key, already extracted from the transport) and returns the verified
// Identity, or an error. Implementations MUST NOT encode the reason for failure
// in a user-facing way — callers redact the error and map it to a generic wire
// status.
//
// mTLS client-certificate authentication does not implement this interface: a
// certificate is a transport property, not a credential string. See CertVerifier.
type Verifier interface {
	Verify(ctx context.Context, credential string) (*Identity, error)
}

// ErrUnauthenticated is the sentinel a Verifier wraps when a credential fails
// verification. Callers test for it with errors.Is, but treat any non-nil verify
// error the same way: a generic 401 / Unauthenticated on the wire, the wrapped
// detail logged server-side (redacted). It never crosses the wire.
var ErrUnauthenticated = errors.New("authn: unauthenticated")

package authn

import "context"

type contextKey int

const (
	identityKey contextKey = iota
	requestMetaKey
)

// RequestMetadata carries the minimal transport facts an AuthorizeFunc may want
// for a policy decision — the route being accessed. The auth adapters populate
// it before running authorization, so a policy can be route-aware without GTB
// shipping a routing model.
type RequestMetadata struct {
	// Method is the HTTP method (GET, POST, …) or "grpc" for an RPC.
	Method string
	// Path is the HTTP request path or the gRPC full method name.
	Path string
}

// ContextWithIdentity returns a child context carrying the verified Identity.
// Auth adapters call this on success so downstream handlers can read it.
func ContextWithIdentity(ctx context.Context, id *Identity) context.Context {
	return context.WithValue(ctx, identityKey, id)
}

// IdentityFromContext returns the verified Identity stored by an auth adapter,
// and whether one was present. The HTTP and gRPC adapters share this key, so a
// handler reads identity the same way regardless of transport.
func IdentityFromContext(ctx context.Context) (*Identity, bool) {
	id, ok := ctx.Value(identityKey).(*Identity)

	return id, ok
}

// ContextWithRequestMetadata returns a child context carrying request metadata
// for AuthorizeFunc. Auth adapters call this before running authorization.
func ContextWithRequestMetadata(ctx context.Context, m RequestMetadata) context.Context {
	return context.WithValue(ctx, requestMetaKey, m)
}

// RequestMetadataFromContext returns the request metadata an adapter injected,
// and whether any was present. Use it from an AuthorizeFunc to make a
// route-aware authorization decision.
func RequestMetadataFromContext(ctx context.Context) (RequestMetadata, bool) {
	m, ok := ctx.Value(requestMetaKey).(RequestMetadata)

	return m, ok
}

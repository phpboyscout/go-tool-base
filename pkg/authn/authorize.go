package authn

import (
	"context"
	"reflect"
)

// AuthorizeFunc decides whether an authenticated Identity may proceed. It runs
// AFTER successful verification; returning false yields 403 / PermissionDenied.
//
// This is the ENTIRE authorization surface GTB ships — there is no built-in RBAC
// engine and no policy DSL. It is deliberately shaped as a policy-model hole: it
// receives the resolved Identity and a context that carries the request metadata
// the adapter injected (see RequestMetadataFromContext), so a tool can implement
// arbitrary external authorization — evaluate a role map, call out to OPA,
// whatever — without GTB shipping the engine. Keep the predicate fast and pure;
// it runs on every authenticated request.
type AuthorizeFunc func(ctx context.Context, id *Identity) bool

// RequireScopes returns an AuthorizeFunc that admits an Identity only if it
// carries all of the named scopes. With no scopes it admits any authenticated
// identity.
func RequireScopes(scopes ...string) AuthorizeFunc {
	return func(_ context.Context, id *Identity) bool {
		if id == nil {
			return false
		}

		have := make(map[string]struct{}, len(id.Scopes))
		for _, s := range id.Scopes {
			have[s] = struct{}{}
		}

		for _, want := range scopes {
			if _, ok := have[want]; !ok {
				return false
			}
		}

		return true
	}
}

// RequireClaim returns an AuthorizeFunc that admits an Identity only if its
// verified claim named name deep-equals value. (DeepEqual avoids a panic on a
// non-comparable claim value.)
func RequireClaim(name string, value any) AuthorizeFunc {
	return func(_ context.Context, id *Identity) bool {
		if id == nil || id.Claims == nil {
			return false
		}

		got, ok := id.Claims[name]

		return ok && reflect.DeepEqual(got, value)
	}
}

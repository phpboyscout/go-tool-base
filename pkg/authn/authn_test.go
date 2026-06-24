package authn_test

import (
	"context"
	"testing"

	"github.com/cockroachdb/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/pkg/authn"
)

func TestNewAPIKeyVerifier_ConstructionErrors(t *testing.T) {
	t.Parallel()

	_, err := authn.NewAPIKeyVerifier()
	require.Error(t, err, "empty key set must fail closed")

	_, err = authn.NewAPIKeyVerifier(authn.KeyEntry{Key: "ok", Subject: "a"}, authn.KeyEntry{Key: "", Subject: "b"})
	require.Error(t, err, "an empty key entry must be rejected")
}

func TestAPIKeyVerifier_AcceptsConfiguredKeys(t *testing.T) {
	t.Parallel()

	v, err := authn.NewAPIKeyVerifier(
		authn.KeyEntry{Key: "key-ci", Subject: "ci-runner"},
		authn.KeyEntry{Key: "key-admin", Subject: "admin"},
	)
	require.NoError(t, err)

	id, err := v.Verify(context.Background(), "key-admin")
	require.NoError(t, err)
	assert.Equal(t, "admin", id.Subject)
	assert.Equal(t, "apikey", id.Method)
	assert.Nil(t, id.Claims)

	id, err = v.Verify(context.Background(), "key-ci")
	require.NoError(t, err)
	assert.Equal(t, "ci-runner", id.Subject)
}

func TestAPIKeyVerifier_RejectsUnknownKey(t *testing.T) {
	t.Parallel()

	v, err := authn.NewAPIKeyVerifier(authn.KeyEntry{Key: "secret", Subject: "a"})
	require.NoError(t, err)

	for _, cred := range []string{"", "wrong", "secre", "secret-extra"} {
		id, err := v.Verify(context.Background(), cred)
		require.Nil(t, id)
		require.ErrorIs(t, err, authn.ErrUnauthenticated, "credential %q must be rejected", cred)
	}
}

func TestRequireScopes(t *testing.T) {
	t.Parallel()

	id := &authn.Identity{Scopes: []string{"api:read", "api:write"}}

	assert.True(t, authn.RequireScopes()(context.Background(), id), "no required scopes admits any identity")
	assert.True(t, authn.RequireScopes("api:read")(context.Background(), id))
	assert.True(t, authn.RequireScopes("api:read", "api:write")(context.Background(), id))
	assert.False(t, authn.RequireScopes("api:admin")(context.Background(), id), "a missing scope is denied")
	assert.False(t, authn.RequireScopes("api:read", "api:admin")(context.Background(), id), "all scopes must be present")
	assert.False(t, authn.RequireScopes("api:read")(context.Background(), nil), "nil identity is denied")
}

func TestRequireClaim(t *testing.T) {
	t.Parallel()

	id := &authn.Identity{Claims: map[string]any{"role": "admin", "tier": 3}}

	assert.True(t, authn.RequireClaim("role", "admin")(context.Background(), id))
	assert.True(t, authn.RequireClaim("tier", 3)(context.Background(), id))
	assert.False(t, authn.RequireClaim("role", "user")(context.Background(), id), "wrong value denied")
	assert.False(t, authn.RequireClaim("missing", "x")(context.Background(), id), "absent claim denied")
	assert.False(t, authn.RequireClaim("role", "admin")(context.Background(), nil), "nil identity denied")
	assert.False(t, authn.RequireClaim("role", "admin")(context.Background(), &authn.Identity{}), "nil claims denied")
}

func TestIdentityContextRoundTrip(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	_, ok := authn.IdentityFromContext(ctx)
	assert.False(t, ok, "absent identity reports not-present")

	want := &authn.Identity{Subject: "admin", Method: "apikey"}
	ctx = authn.ContextWithIdentity(ctx, want)

	got, ok := authn.IdentityFromContext(ctx)
	require.True(t, ok)
	assert.Same(t, want, got)
}

func TestRequestMetadataContextRoundTrip(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	_, ok := authn.RequestMetadataFromContext(ctx)
	assert.False(t, ok, "absent metadata reports not-present")

	want := authn.RequestMetadata{Method: "POST", Path: "/api/widgets"}
	ctx = authn.ContextWithRequestMetadata(ctx, want)

	got, ok := authn.RequestMetadataFromContext(ctx)
	require.True(t, ok)
	assert.Equal(t, want, got)
}

// TestAuthorizeFunc_SeesRequestMetadata proves the policy-shaped seam: an
// AuthorizeFunc can make a route-aware decision from the injected metadata.
func TestAuthorizeFunc_SeesRequestMetadata(t *testing.T) {
	t.Parallel()

	// A policy: writes to /admin require the "admin" scope; everything else is
	// allowed for any authenticated identity.
	policy := authn.AuthorizeFunc(func(ctx context.Context, id *authn.Identity) bool {
		meta, _ := authn.RequestMetadataFromContext(ctx)
		if meta.Path == "/admin" {
			return authn.RequireScopes("admin")(ctx, id)
		}

		return id != nil
	})

	user := &authn.Identity{Subject: "u", Scopes: []string{"api:read"}}
	admin := &authn.Identity{Subject: "a", Scopes: []string{"admin"}}

	adminCtx := authn.ContextWithRequestMetadata(context.Background(), authn.RequestMetadata{Path: "/admin"})
	publicCtx := authn.ContextWithRequestMetadata(context.Background(), authn.RequestMetadata{Path: "/public"})

	assert.False(t, policy(adminCtx, user), "user denied on /admin")
	assert.True(t, policy(adminCtx, admin), "admin allowed on /admin")
	assert.True(t, policy(publicCtx, user), "user allowed on /public")
}

func TestErrUnauthenticated_IsWrapped(t *testing.T) {
	t.Parallel()

	wrapped := errors.Wrap(authn.ErrUnauthenticated, "bad signature")
	assert.ErrorIs(t, wrapped, authn.ErrUnauthenticated)
}

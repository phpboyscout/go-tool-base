package forge

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go/config"
	forgeapi "gitlab.com/phpboyscout/go/forge"

	"gitlab.com/phpboyscout/go-tool-base/internal/testutil"
)

// captureEndpoint registers a provider under sourceType that records the
// endpoint it is constructed with, unregisters it when the test ends, and
// returns where the record lands.
//
// Each caller registers its own type so nothing is shared between subtests; the
// registry is process-wide, and a fake reused across parallel tests is the
// race this package's testing guidance exists to avoid.
func captureEndpoint(t *testing.T, sourceType string) *forgeapi.Endpoint {
	t.Helper()

	seen := &forgeapi.Endpoint{}

	require.NoError(t, forgeapi.Register(sourceType,
		func(_ context.Context, ep forgeapi.Endpoint, _ forgeapi.Config, _ ...forgeapi.Option) (forgeapi.Provider, error) {
			*seen = ep

			return capturingProvider{}, nil
		}))

	t.Cleanup(func() { forgeapi.Unregister(sourceType) })

	return seen
}

// TestCapabilityLookupsNameTheProviderType pins spec 0192 D2 as a property.
//
// Both account-level capability lookups resolve a factory through
// forgeapi.Lookup(profile.Provider) and then construct it. Before 0192 they
// built the provider with only a Host set, which was harmless only because
// nothing validated the source type. It is not harmless under the endpoint
// model: forgeapi.Endpoint.Section scopes a provider's configuration subtree BY
// Type, so an endpoint with an empty Type reads the wrong subtree — silently,
// because reading an absent section is not an error.
//
// The assertion is on the endpoint the factory actually receives rather than on
// the call site, so it survives either function being rewritten, and it covers
// both sites because the next capability lookup will be written by copying one
// of them.
func TestCapabilityLookupsNameTheProviderType(t *testing.T) {
	t.Parallel()

	const host = "forge.example.test"

	for _, tc := range []struct {
		name  string
		build func(context.Context, Profile, *config.View) error
	}{
		{
			name: "defaultForgeProvider",
			build: func(ctx context.Context, profile Profile, cfg *config.View) error {
				_, err := defaultForgeProvider(profile)(ctx, cfg)

				return err
			},
		},
		{
			name: "defaultKeyManager",
			build: func(ctx context.Context, profile Profile, cfg *config.View) error {
				_, err := defaultKeyManager(profile)(ctx, cfg)

				return err
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			sourceType := "gtb-endpoint-capture-" + tc.name
			seen := captureEndpoint(t, sourceType)

			require.NoError(t, tc.build(t.Context(), Profile{Provider: sourceType, Host: host}, testutil.ViewFromYAML(t, "")))

			assert.Equal(t, sourceType, seen.Type,
				"%s must name the provider type; an untyped endpoint scopes config to the wrong subtree", tc.name)
			assert.Equal(t, host, seen.Host, "%s must carry the profile's host", tc.name)
			require.NoError(t, seen.Validate(), "the endpoint reaching a factory must be valid")
		})
	}
}

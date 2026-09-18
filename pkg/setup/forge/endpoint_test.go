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

// construction is what a fake factory saw: the endpoint it was built for and
// the resolved options it was handed.
type construction struct {
	Endpoint forgeapi.Endpoint
	Options  *forgeapi.Options
}

// captureConstruction registers a provider under sourceType that records the
// endpoint and options it is constructed with, unregisters it when the test
// ends, and returns where the record lands.
//
// Each caller registers its own type so nothing is shared between subtests; the
// registry is process-wide, and a fake reused across parallel tests is the
// race this package's testing guidance exists to avoid.
func captureConstruction(t *testing.T, sourceType string) *construction {
	t.Helper()

	seen := &construction{}

	require.NoError(t, forgeapi.Register(sourceType,
		func(_ context.Context, ep forgeapi.Endpoint, _ forgeapi.Config, opts ...forgeapi.Option) (forgeapi.Provider, error) {
			seen.Endpoint = ep
			seen.Options = forgeapi.NewOptions(opts...)

			return capturingProvider{}, nil
		}))

	t.Cleanup(func() { forgeapi.Unregister(sourceType) })

	return seen
}

// capabilityLookups are the two account-level construction sites, named so a
// property can be asserted over both; the next one will be written by copying
// one of these.
var capabilityLookups = []struct {
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

	for _, tc := range capabilityLookups {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			sourceType := "gtb-endpoint-capture-" + tc.name
			seen := captureConstruction(t, sourceType)

			require.NoError(t, tc.build(t.Context(), Profile{Provider: sourceType, Host: host}, testutil.ViewFromYAML(t, "")))

			assert.Equal(t, sourceType, seen.Endpoint.Type,
				"%s must name the provider type; an untyped endpoint scopes config to the wrong subtree", tc.name)
			assert.Equal(t, host, seen.Endpoint.Host, "%s must carry the profile's host", tc.name)
			require.NoError(t, seen.Endpoint.Validate(), "the endpoint reaching a factory must be valid")
		})
	}
}

// TestCapabilityLookupsHandTheFactoryGTBsChain: both sites pass GTB's
// credential chain through forge.WithCredential (go/forge spec 0025), so the
// factory consults it and nothing else. The assertion is on what the factory
// receives: a source that dereferences the profile's auth.env pointer.
//
// Not parallel: it sets the variable the pointer names.
func TestCapabilityLookupsHandTheFactoryGTBsChain(t *testing.T) {
	t.Setenv("GTB_CAPABILITY_TEST_TOKEN", "tok-from-env")

	for _, tc := range capabilityLookups {
		t.Run(tc.name, func(t *testing.T) {
			sourceType := "gtb-credential-capture-" + tc.name
			seen := captureConstruction(t, sourceType)

			cfg := testutil.ViewFromYAML(t, sourceType+":\n  auth:\n    env: GTB_CAPABILITY_TEST_TOKEN\n")
			require.NoError(t, tc.build(t.Context(), Profile{Provider: sourceType, ConfigPrefix: sourceType}, cfg))

			require.NotNil(t, seen.Options.Credential, "%s must hand the factory GTB's chain", tc.name)

			token, err := seen.Options.Credential(t.Context())
			require.NoError(t, err)
			assert.Equal(t, "tok-from-env", token, "the chain dereferences auth.env, which the factory alone would report as stale")
		})
	}
}

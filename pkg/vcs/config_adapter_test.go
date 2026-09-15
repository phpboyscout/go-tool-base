package vcs

import (
	"context"
	"testing"

	"gitlab.com/phpboyscout/go-tool-base/internal/testutil"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go/config"
	"gitlab.com/phpboyscout/go/forge"
)

func TestConfigFromReader_Nil(t *testing.T) {
	t.Parallel()

	assert.Nil(t, ConfigFromReader(nil))
}

func TestConfigFromReader_PreservesEnvAwareSubtrees(t *testing.T) {
	t.Setenv("GTB_GITHUB_AUTH_VALUE", "env-token")
	t.Setenv("GTB_GITHUB_URL_API", "https://env.example.com/api/v3/")

	store, err := config.NewStore(t.Context(),
		config.WithReaders(config.NamedSource{
			Name:    "test",
			Content: []byte("github:\n  auth:\n    value: file-token\n  url:\n    api: https://file.example.com/api/v3/\n"),
		}),
		config.WithEnv("GTB"),
	)
	require.NoError(t, err)

	adapted := ConfigFromReader(store.View())
	require.NotNil(t, adapted)

	githubCfg := adapted.Sub("github")
	require.NotNil(t, githubCfg)

	assert.Equal(t, "env-token", githubCfg.GetString("auth.value"))
	assert.Equal(t, "https://env.example.com/api/v3/", githubCfg.GetString("url.api"))
}

// TestConfigFromReader_AbsentSub pins the nil contract forge's guards rely on:
// Sub of a section defined nowhere returns nil, not an empty reader.
func TestConfigFromReader_AbsentSub(t *testing.T) {
	t.Parallel()

	store, err := config.NewStore(t.Context(),
		config.WithReaders(config.NamedSource{Name: "test", Content: []byte("github:\n  auth:\n    value: tok\n")}))
	require.NoError(t, err)

	adapted := ConfigFromReader(store.View())
	require.NotNil(t, adapted)

	assert.Nil(t, adapted.Sub("gitlab"))
	assert.NotNil(t, adapted.Sub("github"))
	assert.Nil(t, adapted.Sub("github").Sub("url"))
}

// TestEndpointSectionResolvesTheTypeSubtree covers the join between GTB's config
// adapter and the forge module's scoping, which spec 0192 D2 depends on.
//
// GTB hands a provider factory the WHOLE configuration and lets the endpoint
// scope it: forge.Endpoint.Section walks Sub(Type) and then Sub(Name). Nothing
// on either side asserts that those two agree about what a subtree is, so this
// is the one place they could diverge silently — the module could change how it
// composes a section, or configAdapter.Sub could change what it does with a
// missing one, and every provider would read an empty subtree while every test
// that stubs a provider stayed green.
func TestEndpointSectionResolvesTheTypeSubtree(t *testing.T) {
	t.Parallel()

	store, err := config.NewStore(t.Context(),
		config.WithReaders(config.NamedSource{
			Name:    "test",
			Content: []byte("github:\n  url:\n    api: https://ghe.example.com/api/v3/\n  named:\n    url:\n      api: https://other.example.com/api/v3/\n"),
		}),
	)
	require.NoError(t, err)

	adapted := ConfigFromReader(store.View())
	require.NotNil(t, adapted)

	t.Run("default source reads the bare type subtree", func(t *testing.T) {
		t.Parallel()

		section := forge.Endpoint{Type: "github"}.Section(adapted)
		require.NotNil(t, section)
		assert.Equal(t, "https://ghe.example.com/api/v3/", section.GetString("url.api"))
	})

	t.Run("a named source reads one level deeper", func(t *testing.T) {
		t.Parallel()

		section := forge.Endpoint{Type: "github", Name: "named"}.Section(adapted)
		require.NotNil(t, section)
		assert.Equal(t, "https://other.example.com/api/v3/", section.GetString("url.api"))
	})

	t.Run("an untyped endpoint yields a section that reads nothing", func(t *testing.T) {
		t.Parallel()

		// The failure mode D2 guards against, and it is worse than a nil.
		//
		// configAdapter.Sub("") does not report the empty section as absent: it
		// returns an adapter prefixed ".", so every subsequent lookup asks for
		// ".url.api" and gets "". A provider that nil-guards its config — which
		// is the documented way to support configuration-free construction —
		// sees a non-nil section and treats it as real. The result is a provider
		// configured entirely from empty strings, with no error anywhere.
		//
		// forge.Endpoint.Validate is what stops this reaching a provider, which
		// is why D2 makes every construction site name its type rather than
		// relying on a provider to backfill one.
		section := forge.Endpoint{}.Section(adapted)
		require.NotNil(t, section, "an empty type is not reported as an absent section")
		assert.Empty(t, section.GetString("url.api"),
			"...but nothing can be read through it either")
		require.Error(t, forge.Endpoint{}.Validate(),
			"Validate is the guard that keeps this endpoint away from a provider")
	})
}

// TestConfigFromReader_HidesTheKeysGTBDereferences pins #76. GTB reads
// auth.env and auth.keychain itself (they are pointers the ForgeCredential
// chain dereferences); go/forge no longer reads them and reports a config
// carrying them as stale when auth.value yields nothing. The adapter GTB hands
// to a provider factory therefore presents the resolved credential as
// auth.value and shows the pointer keys as unset.
func TestConfigFromReader_HidesTheKeysGTBDereferences(t *testing.T) {
	t.Setenv("MY_DEPLOYMENT_TOKEN", "tok-from-env")

	cfg := ConfigFromReader(testutil.ViewFromYAML(t, "gitlab:\n  auth:\n    env: MY_DEPLOYMENT_TOKEN\n"))
	sub := cfg.Sub("gitlab")
	require.NotNil(t, sub)

	assert.Equal(t, "tok-from-env", sub.GetString("auth.value"), "auth.value is the credential GTB's chain resolves")
	assert.Empty(t, sub.GetString("auth.env"), "the pointer GTB already followed is not forge's to read")
	assert.Empty(t, sub.GetString("auth.keychain"))
	assert.Equal(t, "MY_DEPLOYMENT_TOKEN", testutil.ViewFromYAML(t, "gitlab:\n  auth:\n    env: MY_DEPLOYMENT_TOKEN\n").GetString("gitlab.auth.env"),
		"the underlying config is untouched; only the forge-facing view hides it")
}

// TestConfigFromReader_FactoryChainDoesNotReportTheShippedDefaultAsStale is the
// CI failure from #76 at the seam the provider factories use: every registered
// factory composes forge.ConfigCredential(section, auth.value) first, and with
// GTB's shipped `<forge>.auth.env: <FORGE>_TOKEN` default and no token in the
// environment it returned ErrStaleAuthKeys, which fails provider construction
// and with it the update check.
func TestConfigFromReader_FactoryChainDoesNotReportTheShippedDefaultAsStale(t *testing.T) {
	t.Setenv("GITLAB_TOKEN", "")

	cfg := ConfigFromReader(testutil.ViewFromYAML(t, "gitlab:\n  auth:\n    env: GITLAB_TOKEN\n"))

	token, err := forge.ConfigCredential(cfg.Sub("gitlab"), forge.DefaultAuthKey)(context.Background())
	require.NoError(t, err, "an absent credential is absent, not stale")
	assert.Empty(t, token)
}
